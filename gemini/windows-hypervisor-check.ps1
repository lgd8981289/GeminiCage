# Probe the packaged emulator with the same accelerator used for Debian.
param([Parameter(Mandatory)][string]$AppDirectory)
$ErrorActionPreference = 'Stop'
$start = [Diagnostics.ProcessStartInfo]::new((Resolve-Path (Join-Path $AppDirectory 'resources/runtime/qemu/qemu-system-x86_64.exe')))
$start.UseShellExecute = $false
$start.CreateNoWindow = $true
$start.RedirectStandardInput = $start.RedirectStandardOutput = $start.RedirectStandardError = $true
foreach ($arg in @('-machine','q35,accel=whpx,kernel-irqchip=off','-m','256','-nodefaults','-display','none','-S','-qmp','stdio')) { $start.ArgumentList.Add($arg) }
$p = [Diagnostics.Process]::Start($start)
try {
    $out = $p.StandardOutput.ReadToEndAsync()
    $err = $p.StandardError.ReadToEndAsync()
    try {
        foreach ($command in @('qmp_capabilities','query-status','quit')) { $p.StandardInput.WriteLine((@{execute=$command} | ConvertTo-Json -Compress)) }
        $p.StandardInput.Close()
    } catch [System.IO.IOException] { }
    if (-not $p.WaitForExit(20000)) { $p.Kill($true); $p.WaitForExit() }
    if ($p.ExitCode -eq 0 -and $out.Result -match '"status"\s*:\s*"prelaunch"') {
        Write-Host 'WHPX_READY=true；虚拟 CPU 已初始化。'
    } else { Write-Host "WHPX_READY=false；本机运行时诊断：$($err.Result.Trim())" }
} finally { $p.Dispose() }
