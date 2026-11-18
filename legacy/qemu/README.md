# Gemini Cage

> 这里保存的是独立 QEMU 启动器的旧版源码。Gemini Cage 2.x 使用[新版工作台和使用说明](../../README.md)。

双击「Gemini Cage.app」。Linux 桌面会自动启动 Antigravity。点击 Google 登录后，在同一台虚拟机里的 Firefox ESR 完成授权。如果浏览器询问是否打开 Antigravity，允许它回到桌面应用。项目可以保存在客机的 `~/Projects`。

退出时先保存项目，再双击同目录的「关闭 Gemini Cage.command」。它通过 Gemini Cage 的私有 QMP 套接字请求 Debian 关机。下次打开会复用原磁盘、应用设置和登录状态。

本机已验证从 Antigravity 的「Continue with Google」进入带有「to continue to Google Antigravity」字样的 Google 账号页。验证停在邮箱输入框。当前代理偶尔会出现连接超时，可在客机浏览器内刷新。如果授权已过期，可回到 Antigravity 再点一次登录。

## 隔离和出口

- QEMU 启动参数包含 `-nodefaults -nic none`。客机有自己的内核和磁盘，只能通过一条 virtio 串行通道向宿主申请 HTTPS CONNECT。当前没有宿主文件共享、剪贴板转发或 SSH agent 转发。
- 宿主只接受程序中列出的 Google、Debian 和出口检测域名的 443 端口。非浏览器程序需要支持 HTTPS 代理。域名清单在 `guest/relay.py`，回环 IP 和局域网 IP 都不在其中。
- 虚拟机和启动器独立于 Claude Cage。这个实现复用了当前代理链，Claude Cage 工作台的定时出口复检和授权租约属于原程序。
- 初始化时只读提取 Claude Cage 已应用的代理链。副本使用独立的 Mihomo 进程、随机认证信息和宿主回环端口 27896。应用验证监听者确实是自己启动的进程。代理中断后，客机没有备用网卡或直连路径。
- 后续在 Claude Cage 里换节点不会自动更新副本。如果需要重新同步，先关闭 Gemini Cage，再使用具有 PyYAML 的 Python 运行 `import-route.py`。该脚本只写 Gemini Cage 的私有目录。
- VM 磁盘和代理凭据位于 `~/Library/Application Support/Gemini Cage/`，目录仅限当前 macOS 用户访问。退出后复制整个目录即可做本机备份。磁盘最大 28 GiB，实际占用按写入增长。

## 本机依赖与源码

这份应用面向 Apple Silicon Mac。运行需要 Homebrew QEMU、系统 Python 3 和已安装的 Cage.app 自带的 Mihomo 可执行文件。QEMU 和 Mihomo 各自作为单独的程序运行。Cage 的虚拟磁盘、控制服务和账号数据库均由原程序独立管理。

`Main.m` 是应用入口。`run.py` 配置虚拟硬件并管理进程。`guest/install.sh` 安装 Linux 桌面、Firefox ESR 与 Google 官方 Antigravity 桌面包，注册浏览器和授权回调。`power.py` 负责正常关机。

在这台 Mac 修改启动器后，可运行 `./build.sh` 重建同目录上一级的应用副本。重新制备系统盘时，`prepare-disk.py 官方镜像 新盘路径` 只接受校验匹配的全新目标文件；脚本需要 Python 3.11+、qemu-img 和 e2fsprogs。`seed.py 空的临时目录 Antigravity.tar.gz 新ISO路径` 生成只读安装介质。日常使用无需运行制备脚本。

- [Claude Cage 源码与网络边界](https://github.com/Lee-xeo/Claude-Cage)
- [Debian ARM64 镜像和校验清单](https://cloud.debian.org/images/cloud/trixie/20260712-2537/)
- [Google Antigravity 官方下载](https://antigravity.google/download?os=linux)
