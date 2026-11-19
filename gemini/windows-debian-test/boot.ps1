param([Parameter(Mandatory)][string]$Emulator, [Parameter(Mandatory)][string]$Scratch)
$ErrorActionPreference = 'Stop'
$qemu = Get-Item -LiteralPath $Emulator
$source = Get-Content -LiteralPath "$PSScriptRoot/../../third_party/lima/templates/_images/debian-13.yaml" -Raw
$location = [regex]::Match($source, '(?m)^- location: "(https://cloud\.debian\.org/[^"]+)"\s+arch: "x86_64"\s+digest: "sha512:([a-f0-9]{128})"')
if (-not $location.Success) { throw '缺少已固定校验值的 Debian 13 x64 镜像' }
$disk = Join-Path $Scratch 'debian.qcow2'
& curl.exe -fsSL --retry 3 $location.Groups[1].Value -o $disk
if ($LASTEXITCODE -or (Get-FileHash $disk -Algorithm SHA512).Hash -ne $location.Groups[2].Value) { throw 'Debian 镜像校验失败' }
$seed = Join-Path $Scratch 'seed.iso'
& go run "$PSScriptRoot/seed.go" $seed
if ($LASTEXITCODE) { throw '生成客机测试介质失败' }
$firmware = Join-Path $qemu.DirectoryName 'share/edk2-x86_64-code.fd'
foreach ($accel in @('whpx','tcg')) {
    $machine = if ($accel -eq 'whpx') { 'q35,accel=whpx,kernel-irqchip=off' } else { 'q35,accel=tcg' }
    $cpu = if ($accel -eq 'whpx') { 'qemu64' } else { 'max' }
    $serial = Join-Path $Scratch "$accel-serial.log"
    $start = [Diagnostics.ProcessStartInfo]::new($qemu.FullName)
    $start.UseShellExecute = $false
    $start.WorkingDirectory = $Scratch
    $start.RedirectStandardError = $true
    foreach ($arg in @('-machine',$machine,'-cpu',$cpu,'-m','1024','-smp','2','-display','none','-monitor','none','-nic','none','-serial',"file:$serial",'-drive',"if=pflash,format=raw,readonly=on,file=$firmware",'-drive',"if=virtio,format=qcow2,snapshot=on,file=$disk",'-drive',"if=virtio,format=raw,readonly=on,file=$seed")) {
        $start.ArgumentList.Add($arg)
    }
    $vm = [Diagnostics.Process]::Start($start)
    try {
        $errors = $vm.StandardError.ReadToEndAsync()
        $deadline = [DateTime]::UtcNow.AddMinutes(8)
        while (-not $vm.HasExited -and [DateTime]::UtcNow -lt $deadline) {
            if ((Test-Path $serial) -and (Select-String -LiteralPath $serial -SimpleMatch 'GEMINI_CAGE_BOOT_OK 13 x86_64' -Quiet)) {
                Write-Host "DEBIAN_BOOT_OK accelerator=$accel guest=Debian-13-x86_64 network=none"
                return
            }
            Start-Sleep -Seconds 2
        }
        if ($vm.HasExited -and $accel -eq 'whpx' -and $errors.Result -match '(?i)whpx|hypervisor|accelerator') {
            Write-Host "托管主机未提供可运行的 WHPX，继续验证软件模拟启动。$($errors.Result.Trim())"
            continue
        }
        if (Test-Path $serial) { Get-Content $serial -Tail 12 | Write-Host }
        if ($vm.HasExited) { Write-Host $errors.Result.Trim() }
        throw "Debian 客机未通过启动验证（$accel）"
    } finally {
        if (-not $vm.HasExited) { $vm.Kill($true); $vm.WaitForExit() }
        $vm.Dispose()
    }
}
