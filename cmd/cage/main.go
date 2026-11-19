package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	agentvm "github.com/sylvinus/agent-vm"
	"github.com/sylvinus/agent-vm/internal/appupdate"
	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/cli"
	"github.com/sylvinus/agent-vm/internal/containerenv"
	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/guestos"
	"github.com/sylvinus/agent-vm/internal/guestprofile"
	"github.com/sylvinus/agent-vm/internal/host"
	"github.com/sylvinus/agent-vm/internal/hostproxy"
	"github.com/sylvinus/agent-vm/internal/limaembed"
	"github.com/sylvinus/agent-vm/internal/managedproxy"
	"github.com/sylvinus/agent-vm/internal/workbench"
	"io"
	"os/signal"
	"runtime/debug"
	"sync"
	"syscall"
)

const help = `Gemini Cage — Gemini 和 Antigravity 隔离工作台

cage desktop       打开桌面工作台
cage agent         查看自动化接口及其 JSON 输入输出格式
cage serve         启动仅供本机访问的控制服务
cage server-entry --json  生成用于 SSH 远程连接的私有入口
cage container-configure --json  从标准输入读取 JSON 并配置 Linux 容器
cage init          创建独立配置，随后在工作台添加并选择代理节点
cage check         检查代理连接，无需启动客机
cage guest-os --json  检查主机是否满足安装同类客机的条件
                      Windows 可用 --vm-id GUID 指定一台 Hyper-V 客机
cage proxy-start   按已应用的节点启动代理服务
cage proxy-stop    停止代理服务并关闭客机外网
cage setup         通过指定代理准备基础镜像
cage up            启动工作环境、编辑器和浏览器
cage login         在客机中启动兼容终端的登录流程
cage access        查看旧版查看器的凭据，仅用于诊断
cage shell         打开客机终端
cage run COMMAND   在客机中运行指定命令
cage stop          停止工作环境和代理；加 --keep-proxy 可在重启环境时保留代理
cage list          列出当前配置中的客机
cage doctor        诊断虚拟机问题
cage diagnostics --json PATH.zip   导出脱敏诊断包
cage version       显示核心版本；用 --min VERSION 检查是否达到最低版本
cage licenses      显示第三方许可声明

配置路径：~/Library/GeminiCage/config.json，可通过 GEMINI_CAGE_CONFIG 指定其他路径。
每个配置目录对应一个独立环境。客机只能访问已授权的项目文件夹，主机登录凭据不会自动导入。
`

func main() {
	defer func() {
		if failure := recover(); failure != nil {
			diagnostics.Log("error", "command", fmt.Sprintf("panic: %v\n%s", failure, debug.Stack()))
			panic(failure)
		}
	}()
	code := run()
	if code != 0 {
		diagnostics.Log("error", "command", "operation exited unsuccessfully")
	}
	os.Exit(code)
}
func run() int {
	hostproxy.CaptureEnvironment()
	// Apply this before desktop/serve dispatch: the control service and every
	// child process must keep Cage's isolation even when launched from a shell
	// configured to relax agent-vm's protections.
	for _, name := range []string{
		"AGENT_VM_UNSAFE_OPEN_NETWORK",
		"AGENT_VM_UNSAFE_WRITABLE_GIT",
		"AGENT_VM_UNSAFE_DISABLE_SECURITY_PROMPTS",
	} {
		os.Unsetenv(name)
	}
	// The shared commands otherwise name agent-vm and suggest its installer.
	cli.Command = "gemini-cage"
	cli.UpdateHint = "从 GeminiCage 项目的发行页面下载新版安装包。"
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "host-download" {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := hostDownload(ctx, args[1:], os.Stdout); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Print(help)
		return 0
	}
	// These read only this build's own metadata. Answering them before the
	// profile is opened keeps `cage version --min` usable on a fresh install.
	if args[0] == "version" || args[0] == "--version" || args[0] == "-V" || args[0] == "licenses" {
		return cli.Main(args, cli.Stdio())
	}
	if args[0] == "agent" {
		return runAgent(args[1:], os.Stdin, os.Stdout)
	}
	if args[0] == "verify-web-assets" {
		if len(args) != 1 {
			return 2
		}
		if err := workbench.VerifyWebAssets(); err != nil {
			return fail(err)
		}
		return 0
	}
	if len(args) == 1 && (args[0] == "verify-macos-guest" || args[0] == "verify-macos-trial") {
		if !macGuestLoginAvailable() {
			return fail(fmt.Errorf("此版本缺少虚拟 Mac 登录窗口组件。"))
		}
		return 0
	}
	macGuestCommand := len(args) >= 2 && args[0] == "guest-os" && args[1] == "macos"
	if args[0] == "guest-os" && !macGuestCommand {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := writeGuestOSInspection(ctx, args[1:], os.Stdout); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "verify-vm-assets" {
		if len(args) != 1 {
			return 2
		}
		dir, err := os.MkdirTemp("", "cage-vm-assets-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(dir)
		share, err := limaembed.WriteShare(dir)
		if err != nil {
			return fail(err)
		}
		if runtime.GOOS == "darwin" {
			arch := "aarch64"
			if runtime.GOARCH == "amd64" {
				arch = "x86_64"
			}
			if _, err := share.GuestAgent("Darwin", arch); err != nil {
				return fail(err)
			}
		}
		return 0
	}
	if err := host.ConfigureWindowsRuntime(); err != nil {
		return fail(err)
	}
	path := os.Getenv(cage.ConfigEnv)
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, "Library", "GeminiCage", "config.json")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return fail(err)
	}
	store := diagnostics.New(filepath.Dir(path))
	diagnostics.SetDefault(store)
	if args[0] == "windows-network-host" {
		if len(args) != 1 {
			return fail(fmt.Errorf("windows-network-host 使用当前环境已绑定的配置。"))
		}
		if err := workbench.ServeWindowsPacketNetwork(path); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "container-runtime" {
		if len(args) != 2 {
			return fail(fmt.Errorf("用法：container-runtime prepare|start|stop|inspect"))
		}
		environment, err := containerenv.Load(filepath.Dir(path))
		if err != nil {
			return fail(err)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		switch args[1] {
		case "prepare":
			err = environment.Prepare(ctx, os.Stdout)
		case "start":
			err = environment.Start(ctx, path, os.Stdout)
		case "stop":
			err = environment.Stop(ctx)
		case "inspect":
			inspection, inspectErr := environment.Inspect(ctx)
			err = inspectErr
			if err == nil {
				err = json.NewEncoder(os.Stdout).Encode(inspection)
			}
		default:
			err = fmt.Errorf("用法：container-runtime prepare|start|stop|inspect")
		}
		if err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "container-configure" {
		if len(args) != 2 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：container-configure --json < configuration.json"))
		}
		if err := configureContainer(path); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "container-install-script" {
		if len(args) != 2 {
			return fail(fmt.Errorf("用法：container-install-script tools|services"))
		}
		script, err := containerInstallScript(args[1])
		if err != nil {
			return fail(err)
		}
		_, err = io.WriteString(os.Stdout, script)
		if err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "container-network" {
		if len(args) != 1 {
			return 2
		}
		environment, err := containerenv.Load(filepath.Dir(path))
		if err != nil {
			return fail(err)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := environment.ServeNetwork(ctx, path); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "container-tap" {
		if len(args) > 2 {
			return 2
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := containerenv.RunTAP(ctx, os.Stdin, os.Stdout, args[1:]...); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "server-entry" {
		if len(args) != 2 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：server-entry --json"))
		}
		entry, err := workbench.ServerEntry(path)
		if err != nil {
			return fail(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(entry); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "diagnostic-event" {
		var event struct {
			Source  string `json:"source"`
			Message string `json:"message"`
		}
		if json.NewDecoder(io.LimitReader(os.Stdin, 32<<10)).Decode(&event) != nil {
			return 2
		}
		if event.Source != "native" {
			return 2
		}
		if err := store.Record("error", event.Source, event.Message); err != nil {
			return 1
		}
		return 0
	}
	if args[0] == "diagnostics" {
		connection := ""
		if len(args) == 5 && args[1] == "--json" && args[2] == "--connection" {
			connection = args[3]
			args = []string{args[0], args[1], args[4]}
		}
		if len(args) != 3 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：diagnostics --json /absolute/path.zip"))
		}
		if err := workbench.ExportDiagnosticsForConnection(workbench.DesktopHelperConfig(path, connection), connection, args[2]); err != nil {
			diagnostics.Log("error", "command", err.Error())
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "ok"})
		return 0
	}
	if args[0] == "log-relay" {
		if len(args) != 2 {
			return 2
		}
		return relay(path, args[1])
	}
	if args[0] == "app-update" {
		return appupdate.Run(path, args[1:], os.Stdout)
	}
	connectionID := ""
	if (args[0] == "clipboard-files" || args[0] == "upload-files" || args[0] == "browser-file" || args[0] == "desktop-file" || args[0] == "share-project") && len(args) >= 3 && args[2] == "--connection" {
		if len(args) < 4 || args[3] == "" {
			return fail(fmt.Errorf("缺少文件所属的连接标识。"))
		}
		connectionID = args[3]
		args = append(append([]string{}, args[:2]...), args[4:]...)
	}
	if args[0] == "clipboard-files" {
		if len(args) < 4 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：clipboard-files --json --connection ID send|receive PATH..."))
		}
		result, err := workbench.ClipboardFilesForConnection(workbench.DesktopHelperConfig(path, connectionID), connectionID, args[2], args[3:])
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	if args[0] == "desktop-file" {
		if len(args) != 3 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：desktop-file --json --connection ID REQUEST"))
		}
		result, err := workbench.DesktopFileForConnection(workbench.DesktopHelperConfig(path, connectionID), connectionID, args[2])
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	if args[0] == "share-project" {
		if connectionID == "" || len(args) != 4 || args[1] != "--json" || (args[2] != "read" && args[2] != "write") {
			return fail(fmt.Errorf("用法：share-project --json --connection ID read|write DIRECTORY"))
		}
		result, err := workbench.ShareNativeCloudProject(workbench.DesktopHelperConfig(path, connectionID), connectionID, args[3], args[2] == "read")
		if err != nil {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	if args[0] == "upload-files" {
		if len(args) < 5 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：upload-files --json browser|desktop|terminal ID PATH..."))
		}
		result, err := workbench.UploadHostFilesForConnection(workbench.DesktopHelperConfig(path, connectionID), connectionID, args[2], args[3], args[4:])
		if err != nil {
			diagnostics.Log("error", "command", err.Error())
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	if args[0] == "browser-file" {
		if len(args) < 4 || len(args) > 5 || args[1] != "--json" {
			return fail(fmt.Errorf("用法：browser-file --json save|open|reveal ID [destination]"))
		}
		destination := ""
		if len(args) == 5 {
			destination = args[4]
		}
		result, err := workbench.BrowserFileForConnection(workbench.DesktopHelperConfig(path, connectionID), connectionID, args[2], args[3], destination)
		if err != nil {
			diagnostics.Log("error", "command", err.Error())
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
			return 1
		}
		_ = json.NewEncoder(os.Stdout).Encode(result)
		return 0
	}
	if args[0] == "serve-desktop" {
		if err := workbench.ServeDesktop(path); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "serve" {
		if err := workbench.Serve(path); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "desktop" {
		options := workbench.DesktopOptions{}
		returnJSON := false
		for _, argument := range args[1:] {
			switch argument {
			case "--json":
				returnJSON = true
			case "--complete-update":
				options.CompleteUpdate = true
			case "--existing":
				options.UseExisting = true
			default:
				return fail(fmt.Errorf("无法识别桌面选项 %s", argument))
			}
		}
		entry, err := workbench.DesktopEntry(path, options)
		if err != nil {
			if returnJSON {
				diagnostics.Log("error", "command", err.Error())
				_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
				return 1
			}
			return fail(err)
		}
		// Native launchers consume this on a pipe, never in a shared log.
		if returnJSON {
			_ = json.NewEncoder(os.Stdout).Encode(entry)
		} else {
			if entry.URL == "" {
				return fail(fmt.Errorf("%s", entry.Message))
			}
			if err := openWorkbench(entry.URL); err != nil {
				return fail(err)
			}
			fmt.Println("Cage workbench opened.")
		}
		return 0
	}
	if args[0] == "prepare-update" {
		stop, returnJSON := false, false
		for _, argument := range args[1:] {
			switch argument {
			case "--stop":
				stop = true
			case "--json":
				returnJSON = true
			default:
				return fail(fmt.Errorf("无法识别更新准备选项 %s", argument))
			}
		}
		result, err := workbench.PrepareDesktopUpdate(path, stop)
		if err != nil {
			if returnJSON {
				diagnostics.Log("error", "command", err.Error())
				_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "error", "message": err.Error()})
				return 1
			}
			return fail(err)
		}
		if returnJSON {
			_ = json.NewEncoder(os.Stdout).Encode(result)
		} else if result.Status != "prepared" {
			return fail(fmt.Errorf("%s", result.Message))
		}
		return 0
	}
	if args[0] == "init" {
		if err = host.EnsurePrivateDir(filepath.Dir(path)); err != nil {
			return fail(err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return fail(err)
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		err = enc.Encode(cage.DefaultConfig())
		f.Close()
		if err != nil {
			return fail(err)
		}
		fmt.Println("Configuration created:", path)
		return 0
	}
	// The workbench owns the proxy lifetime during internal environment
	// restarts. A normal CLI stop still shuts down both the guest and core.
	keepProxy := len(args) > 1 && args[0] == "stop" && args[1] == "--keep-proxy"
	if keepProxy {
		args = append([]string{"stop"}, args[2:]...)
	}
	cfg, err := cage.Load(path)
	management := args[0] == "stop" || args[0] == "list" || args[0] == "info"
	if err != nil && !management {
		return fail(err)
	}

	os.Setenv(cage.ConfigEnv, path)
	profile := filepath.Dir(path)
	state := filepath.Join(profile, "state")
	os.Unsetenv("CAGE_LIMA_CACHE_DIR")
	os.Unsetenv("CAGE_LOGICAL_LIMA_HOME")
	if !containerenv.Configured(profile) {
		if err = guestos.RecoverWindowsStorage(context.Background(), profile); err != nil {
			return fail(err)
		}
		lease, location, err := cage.AcquireStorageUse(profile)
		if err != nil {
			return fail(err)
		}
		defer lease.Close()
		// Keep image caches on the same volume the initialization page shows,
		// including the default location. The saved path survives app updates.
		os.Setenv("CAGE_LIMA_CACHE_DIR", filepath.Join(location.Directory, "cache", "lima"))
		if location.Custom {
			// Resolve the profile prefix once, as Lima did before migration.
			// Keep state itself logical so its original disk and socket paths survive.
			realProfile, err := filepath.EvalSymlinks(profile)
			if err != nil {
				return fail(err)
			}
			state = filepath.Join(realProfile, "state")
			os.Setenv("CAGE_LOGICAL_LIMA_HOME", filepath.Join(state, "lima"))
		}
	}
	os.Setenv("AGENT_VM_STATE_DIR", state)
	os.Setenv("AGENT_VM_LIMA_HOME", filepath.Join(state, "lima"))
	// Do not inherit host proxy settings or credentials into VM operations.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "ftp_proxy", "no_proxy", "ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"} {
		os.Unsetenv(name)
	}
	// Lima's host-side image downloader follows system proxy settings/TUN.
	// Guest traffic keeps the isolated hostagent hooks in limaembed.Run.
	http.DefaultTransport = hostproxy.NewTransport()
	if limaembed.Run(args) {
		return 0
	}
	if macGuestCommand {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := runMacGuest(ctx, profile, state, args[2:]); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "proxy-start" || args[0] == "proxy-stop" {
		if err := proxyCommand(args[0], filepath.Dir(path), cfg); err != nil {
			return fail(err)
		}
		return 0
	}
	if handled, err := containerOperation(path, args, cfg, keepProxy); handled {
		if err != nil {
			return fail(err)
		}
		return 0
	}
	// Every Cage operation targets this profile's one persistent environment.
	switch args[0] {
	case "up", "login", "shell", "run", "claude", "stop", "doctor", "access", "name":
		directory := cage.EnvironmentDir(filepath.Dir(path))
		if err := os.MkdirAll(directory, 0700); err != nil {
			return fail(err)
		}
		if err := os.Chdir(directory); err != nil {
			return fail(err)
		}
	}
	if management {
		code := cli.Main(args, cli.Stdio())
		if code == 0 && args[0] == "stop" && !keepProxy {
			if err := managedproxy.Stop(filepath.Dir(path)); err != nil {
				return fail(err)
			}
		}
		return code
	}
	if err := cfg.RequireManagedProxy(filepath.Dir(path)); err != nil {
		return fail(err)
	}
	if err := cfg.CheckRunning(state); err != nil {
		return fail(err)
	}

	if args[0] == "check" {
		e, err := cage.NewEgress(cfg)
		if err != nil {
			return fail(err)
		}
		client := &http.Client{Timeout: 25 * time.Second, Transport: &http.Transport{DialContext: e.DialContext, Proxy: nil}}
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://1.1.1.1/cdn-cgi/trace", nil)
		r, err := client.Do(req)
		if err != nil {
			return fail(fmt.Errorf("代理连接检查失败，请检查已选节点；客机未切换为直连。"))
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			return fail(fmt.Errorf("代理检测返回 HTTP %d", r.StatusCode))
		}
		fmt.Println("Proxy transport OK. Guest traffic still requires VM acceptance checks.")
		return 0
	}
	if args[0] == "setup" {
		if cfg.GuestProfile == nil {
			loc, e := cage.GetStorageLocation(filepath.Dir(path))
			if e != nil {
				return fail(e)
			}
			if !loc.HasEnvironment {
				cfg.GuestProfile, e = guestprofile.Create(filepath.Dir(path), guestprofile.Options{})
				if e != nil {
					return fail(e)
				}
			}
		}
		if err = os.MkdirAll(state, 0700); err != nil {
			return fail(err)
		}
		if err = os.WriteFile(filepath.Join(state, "setup.sh"), []byte(cfg.SetupScript()), 0600); err != nil {
			return fail(err)
		}
		memory, cpus, resourceErr := cage.ConfiguredResourceLimits(path)
		if resourceErr != nil {
			return fail(resourceErr)
		}
		args = append([]string{"setup", "--memory", fmt.Sprint(memory), "--cpus", fmt.Sprint(cpus), "--disk", "24", "--preinstall=node,python,chromium,gh,claude,code-server"}, args[1:]...)
	}
	if args[0] == "up" || args[0] == "login" {
		result := cli.Main([]string{"run", "true"}, cli.Stdio())
		if result != 0 {
			return result
		}
		if err := workbench.StartGuestServices(context.Background(), path, os.Stdout, os.Stderr); err != nil {
			return fail(err)
		}
		fmt.Println("Cage local services are ready. Open Cage or run cage desktop to complete the required network checks. External networking stays closed until the workbench approves this VM; no local access password is required.")
		if args[0] == "login" {
			return cli.Main([]string{"run", "--tty", "bash", "-lc", "source /etc/profile.d/claude-cage.sh; exec claude auth login"}, cli.Stdio())
		}
		return 0
	}
	if args[0] == "update-tools" {
		if result := cli.Main([]string{"run", "true"}, cli.Stdio()); result != 0 {
			return result
		}
		if err := workbench.RunMaintenanceScript(context.Background(), path, cage.ToolsUpdateScript(), os.Stdout, os.Stderr); err != nil {
			return fail(err)
		}
		return 0
	}
	if args[0] == "access" {
		args = []string{"run", "bash", "-c", `printf 'Editor password: '; sed -n 's/^password: //p' "$HOME/.config/claude-cage/code-server.yaml"; printf 'Browser password: '; cat "$HOME/.config/claude-cage/browser-password"; printf '\n'`}
	}
	allowed := map[string]bool{"setup": true, "run": true, "shell": true, "claude": true, "stop": true, "list": true, "doctor": true, "info": true, "name": true}
	if !allowed[args[0]] {
		return fail(fmt.Errorf("无法识别命令 %q，请运行 cage help 查看用法。", args[0]))
	}
	return cli.Main(args, cli.Stdio())
}

func containerInstallScript(kind string) (string, error) {
	switch kind {
	case "services":
		return cage.GuestSetup, nil
	case "tools":
		script := "set -e\nexport CLAUDE_CAGE=1\n" + cage.APTBootstrapScript + "\n" + cage.ToolInstallPrelude()
		enabled := map[string]bool{"NODE": true, "PYTHON": true, "CHROMIUM": true, "GH": true, "CLAUDE": true, "CODE_SERVER": true}
		for _, name := range []string{"PYTHON", "NODE", "RUBY", "RUST", "GOLANG", "DOCKER", "CHROMIUM", "GH", "CLAUDE", "OPENCODE", "CODEX", "VIBE", "PI", "CODE_SERVER", "CODE_CLAUDE", "CODE_OPENCODE", "CODE_CODEX", "CODE_VIBE", "MCP_CHROME", "MCP_PLAYWRIGHT"} {
			value := "0"
			if enabled[name] {
				value = "1"
			}
			script += "export AGENT_VM_INSTALL_" + name + "=" + value + "\n"
		}
		return script + agentvm.SetupScript, nil
	default:
		return "", fmt.Errorf("用法：container-install-script tools|services")
	}
}

// Service assets exceed Windows' command-line limit. Give only this internal
// invocation a script stream; interactive login and ordinary run keep stdin.
func runGuestServiceStart(script string, stdio cli.IO, invoke func([]string, cli.IO) int) int {
	stdio.Stdin = strings.NewReader(script)
	return invoke([]string{"run", "bash", "-s"}, stdio)
}

func fail(err error) int {
	diagnostics.Log("error", "command", err.Error())
	fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
	return 1
}

func openWorkbench(address string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", address).Start()
	}
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	return exec.Command(command, address).Run()
}

// Relay owns stdout/stderr capture outside the child so a Go runtime panic or
// abrupt child exit is recorded without ever persisting raw output.
func relay(configPath, kind string) int {
	var cmd *exec.Cmd
	source := "controller"
	if kind == "controller" {
		exe, err := os.Executable()
		if err != nil {
			return fail(err)
		}
		cmd = exec.Command(exe, "serve")
	} else if kind == "proxy" {
		core, err := managedproxy.CorePath()
		if err != nil {
			return fail(err)
		}
		profile := filepath.Dir(configPath)
		cmd = exec.Command(core, "-d", managedproxy.Dir(profile), "-f", filepath.Join(managedproxy.Dir(profile), "config.yaml"))
		source = "proxy"
	} else {
		return 2
	}
	configureRelayCommand(cmd)
	cmd.Env = append(os.Environ(), cage.ConfigEnv+"="+configPath)
	store := diagnostics.New(filepath.Dir(configPath))
	out := store.Stream(source, "info")
	defer out.Close()
	stderr := store.Stream(source, "error")
	defer stderr.Close()
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	diagnostics.Log("info", source, "process started")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	done := make(chan struct{})
	var wg sync.WaitGroup
	var forwarded os.Signal
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case sig := <-signals:
			forwarded = sig
			_ = cmd.Process.Signal(sig)
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	wg.Wait()
	if err != nil {
		if expectedRelayStop(err, forwarded) {
			diagnostics.Log("info", source, "process stopped on request")
			return 0
		}
		diagnostics.Log("error", source, "进程已退出："+err.Error())
		return 1
	}
	diagnostics.Log("info", source, "process exited normally")
	return 0
}

func expectedRelayStop(err error, forwarded os.Signal) bool {
	signal, ok := forwarded.(syscall.Signal)
	if !ok {
		return false
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == signal || exit.ExitCode() == 128+int(signal)
}
