# 在 Windows 上使用 Gemini Cage

Windows x64 测试版面向搭载 Intel 或 AMD 处理器的电脑。建议先在 Windows 11 上使用 Debian 13 客机。在这台虚拟机内，可以打开 Gemini 网页、安装 Linux 版 Antigravity、运行终端并共享选定的项目。

## 下载安装

1. 打开 [Windows 测试版发行页](https://github.com/lgd8981289/GeminiCage/releases/tag/v2.0.5-beta.1)，下载 `GeminiCage-2.0.5-beta.1-windows-amd64-setup-unsigned.exe` 和同一页的 `SHA256SUMS`。
2. 在下载目录打开 PowerShell，运行以下命令，核对输出是否与校验文件中对应安装包的一行一致。此测试版没有 Windows 发布者签名，系统可能显示「未知发布者」。组织管理的电脑请遵循管理员的软件安装策略。

   ```powershell
   Get-FileHash -Algorithm SHA256 .\GeminiCage-2.0.5-beta.1-windows-amd64-setup-unsigned.exe
   ```

3. 运行安装包，按向导选择目录和快捷方式。默认使用固定磁盘上的 `D:\GeminiCage`，没有可用的固定 D 盘时使用 `%LOCALAPPDATA%\Programs\GeminiCage`。从开始菜单打开 **Gemini Cage**。
4. 在「启用或关闭 Windows 功能」里启用「Windows 虚拟机监控程序平台」，按系统提示重启。固件也需要开启 CPU 虚拟化，可在任务管理器的「性能 → CPU」查看其状态。

## 第一次进入环境

1. 创建本地工作台账号，保存恢复密钥，在首次设置里选择 **Debian 13**。可把虚拟磁盘放到容量足够的本地磁盘上。
2. 在「网络代理」添加自己的节点或订阅，选择并应用节点，继续「安装与检查」。代理地址和密码只填在本机工作台。
3. 等环境显示就绪，点击左侧「浏览器」旁的四角星打开 **Gemini 网页版**。也可打开 **Antigravity**，安装后点击打开，在客机浏览器里完成自己的 Google 登录。选择项目时，授予具体项目目录的访问权限。
4. 若客机没有启动，点「查看操作详情」，核对虚拟化、磁盘空间和代理检测结果。测试报告会区分 Windows 安装器、工作台窗口和实际客机启动。
5. 保存工作后在工作台停止环境，再退出 Gemini Cage。下次启动会继续使用原来的虚拟磁盘。

安装客机时，也可以在微信搜一搜中搜索 **程序员Sunday**，关注公众号获取后续教程。

<a href="images/wechat-sunday.jpg"><img src="images/wechat-sunday.jpg" alt="微信公众号「程序员Sunday」" width="500"></a>

## 已安装 Claude Cage 的电脑

两个应用有独立的安装标识、卸载项、开始菜单目录、配置、浏览器数据和后台连接。Gemini Cage 的默认配置在 `%USERPROFILE%\.gemini-cage`。如果需要使用相同网络出口，请在 Gemini Cage 中单独配置自己的代理。已有 Claude Cage 的安装目录、配置及磁盘目录应留给原应用。

要更新时，先保存工作并退出 Gemini Cage，再运行新版安装包。要卸载时，在 Windows 的「设置 → 应用 → 已安装的应用」中选择 **Gemini Cage**。卸载会保留项目、个人配置和虚拟磁盘。

## 从源码构建 Windows 安装包

使用 Windows x64、PowerShell 7、与 `global.json` 匹配的 .NET SDK、与 `go.mod` 匹配的 Go、Node.js 24、uv、7-Zip 和 NSIS。在干净的仓库根目录执行以下命令。构建脚本会校验指定版本的上游介质，复用 QEMU、OpenSSH、WebView2 等运行时，并从本仓库编译工作台、桌面启动器和客机助手。

```powershell
$base = 'https://github.com/Lee-xeo/Claude-Cage/releases/download/v0.1.8'
curl.exe -fL "$base/Cage-0.1.8-windows-amd64-setup.exe" -o "$env:TEMP\cage-kit.exe"
curl.exe -fL "$base/qemu-11.1.0-windows-full-source.tar.gz" -o "$env:TEMP\qemu-source.tar.gz"
./scripts/build-windows.ps1 -UpstreamInstaller "$env:TEMP\cage-kit.exe"
./scripts/package-windows.ps1 -SkipBuild -QemuSourceArchive "$env:TEMP\qemu-source.tar.gz"
```

安装包、对应源码与校验文件生成在 `_output/releases/2.0.5-beta.1/windows/`。可在运行记录查看编译和安装器共存测试结果。[Windows 构建记录](https://github.com/lgd8981289/GeminiCage/actions/workflows/windows-beta.yml)
