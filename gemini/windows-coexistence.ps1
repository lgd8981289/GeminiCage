# Only run on an empty, disposable Windows test user.
[CmdletBinding()]
param([Parameter(Mandatory)][string]$Installer, [Parameter(Mandatory)][string]$UpstreamInstaller)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') { throw '共存测试在一次性 Windows 托管运行器上执行。' }
if ((Get-FileHash $UpstreamInstaller).Hash -ne '4b069b1e82e5d0ed0fd5a718222428679cf392b2b5f3e7e1a8efddc30634f0c4') { throw '上游安装包校验失败' }
$original = Join-Path $env:RUNNER_TEMP 'ClaudeCage-original'
$key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeCage'
if ((Test-Path $original) -or (Test-Path $key) -or (Test-Path "$env:USERPROFILE/.claude-cage")) { throw '需要全新的 Windows 测试用户' }
function Start-Checked([string]$File, [string[]]$Arguments, [int]$Expected = 0) {
    $p = Start-Process $File -ArgumentList $Arguments -PassThru
    if (-not $p.WaitForExit(300000) -or $p.ExitCode -ne $Expected) { throw "测试程序退出异常（$($p.ExitCode)）" }
}
$newProfile = Join-Path $env:USERPROFILE '.gemini-cage'
if ((Test-Path $newProfile) -or $env:GEMINI_CAGE_CONFIG) { throw '测试要求空的默认 Gemini Cage 配置' }
Start-Checked $UpstreamInstaller @('/S', "/D=$original")
function Original-State {
    $reg = Get-ItemProperty -LiteralPath $key
    if ($reg.InstallLocation -ne $original) { throw '原软件安装登记发生变化' }
    $result = @((Get-Item $key).GetValueNames() | Sort-Object | ForEach-Object { "$_=$($reg.$_)" })
    $files = @("$original/Cage.exe", "$original/resources/cage.exe", "$original/Cage.install-id", "$original/Uninstall.exe", "$([Environment]::GetFolderPath('DesktopDirectory'))/Cage.lnk", "$([Environment]::GetFolderPath('Programs'))/Cage/Cage.lnk", "$([Environment]::GetFolderPath('Programs'))/Cage/Uninstall Cage.lnk")
    foreach ($file in $files) { $result += (Get-FileHash -LiteralPath $file).Hash }
    return $result -join '|'
}
$before = Original-State
$native = Join-Path $PSScriptRoot '../_output/windows-amd64/app'
$info = & "$native/resources/cage.exe" agent capabilities | ConvertFrom-Json
if ($LASTEXITCODE -or $info.config_path -ne (Join-Path $newProfile 'config.json')) { throw '命令行默认配置与 Gemini Cage 不一致' }
$defaultReport = Join-Path $env:RUNNER_TEMP 'gemini-default-window.json'
Start-Checked "$native/Cage.exe" @('--smoke-test', "`"$defaultReport`"")
if (-not (Test-Path "$newProfile/webview2") -or (Test-Path "$env:USERPROFILE/.claude-cage")) { throw '桌面工作台未隔离默认配置' }
Start-Checked $Installer @('/S', "/D=$original") 2
Start-Checked $Installer @('/S', "/D=$original\empty-child") 2
$alias = Join-Path $env:RUNNER_TEMP 'original-junction'
New-Item -ItemType Junction -Path $alias -Target $original | Out-Null
Start-Checked $Installer @('/S', "/D=$alias\new-folder") 2
& (Join-Path $PSScriptRoot '../scripts/test-windows-install.ps1') -Installer $Installer
if ((Original-State) -cne $before) { throw '原应用、注册表或快捷方式发生变化' }
# Open the upstream workbench after our installer is gone. Keep its own smoke
# profile outside the byte-for-byte preservation fixture.
$env:CLAUDE_CAGE_CONFIG = Join-Path $env:RUNNER_TEMP 'original-runtime/config.json'
$report = Join-Path $env:RUNNER_TEMP 'original-window.json'
Start-Checked "$original/Cage.exe" @('--smoke-test', "`"$report`"")
if (-not (Test-Path $report) -or (Original-State) -cne $before) { throw '原工作台启动验证失败' }
Write-Host '共存通过：原目录拒绝安装，安装、启动、更新和卸载后原工作台仍可打开，原配置与快捷方式保留。'
