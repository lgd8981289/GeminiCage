package workbench

// The agent adapter uses the same authenticated local workload as the desktop.
// It never follows the desktop gateway's currently selected remote connection.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/sylvinus/agent-vm/internal/diagnostics"
	"github.com/sylvinus/agent-vm/internal/remotehost"
)

const AgentProtocol = 1

type AgentOperation struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Mutates      bool           `json:"mutates"`
	InputSchema  map[string]any `json:"input_schema"`
	method, path string
}

// Named operations are discoverable shortcuts for the same production handlers.
// AgentRequest also accepts UI endpoints so the shortcut list is not a permission boundary.
func AgentOperations() []AgentOperation {
	object := func(fields map[string]string) map[string]any {
		properties := map[string]any{}
		for name, kind := range fields {
			if kind == "array" {
				properties[name] = map[string]any{"type": kind, "items": map[string]string{"type": "string"}}
			} else {
				properties[name] = map[string]string{"type": kind}
			}
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	}
	from := func(value any) map[string]any { return agentSchema(reflect.TypeOf(value)) }

	var ops []AgentOperation
	add := func(name, description, method, path string, schema map[string]any) {
		ops = append(ops, AgentOperation{Name: name, Description: description, Mutates: method != "GET", InputSchema: schema, method: method, path: path})
	}
	get := func(name, description, path string) { add(name, description, "GET", path, object(nil)) }
	post := func(name, description, path string, schema map[string]any) {
		add(name, description, "POST", path, schema)
	}
	get("status", "查看环境是否就绪、网络检查结果和当前任务", "/api/status")
	get("account.status", "查看本地账户是否存在、是否锁定；结果不含凭据", "/api/account")
	post("account.create", "在空配置中创建账户。密码由用户设置，返回的 recovery_key 仅显示一次，请私下交给用户妥善保存", "/api/account/create", object(map[string]string{"username": "string", "password": "string", "confirm_password": "string", "avatar": "string"}))
	post("account.unlock", "不支持此 HTTP 请求方式。", "/api/account/unlock", object(map[string]string{"password": "string"}))
	get("settings.get", "读取设置，结果不含代理凭据", "/api/settings")
	post("settings.patch", "在操作锁内合并指定字段；dry_run 仅校验，不写入", "/api/agent/settings", object(map[string]string{"patch": "object", "dry_run": "boolean"}))
	// Include the production Settings schema inside the patch field.
	ops[len(ops)-1].InputSchema["properties"].(map[string]any)["patch"] = from(Settings{})
	ops[len(ops)-1].InputSchema["properties"].(map[string]any)["patch"].(map[string]any)["properties"].(map[string]any)["exit_policy"] = object(map[string]string{"mode": "string", "scope": "string", "ip": "string", "country_code": "string", "region": "string", "city": "string"})
	post("settings.restore", "恢复上次保存的设置并重启环境。请先查看状态，确认用户允许中断当前工作", "/api/restore", object(nil))
	get("proxy.status", "列出来源和节点编号，结果不含订阅地址或节点密钥", "/api/proxy")
	proxy := from(proxyInput{})
	for _, action := range []string{"import", "refresh", "edit", "delete", "select", "route", "restart", "stop"} {
		post("proxy."+action, "管理代理。import 新增来源，refresh 更新已有来源，select 和 route 应用线路。重试前请先读取当前状态", "/api/proxy/"+action, proxy)
	}
	get("guest.options", "查看可用客机系统及可继续的安装任务", "/api/environment/guest-systems")
	get("guest.match", "查看与主机匹配的系统镜像方案。调用 environment.start 时，将 key 作为 clone_key，将 plan.host.system 作为 guest_system", "/api/environment/guest-systems/clone")
	post("guest.activate", "切换到已安装的客机；切换后可能需要重新连接后台", "/api/environment/guest-systems/activate", object(nil))
	get("initialization.status", "查看首次设置进度", "/api/environment/initialization")
	post("initialization.set", "action 可选 begin、install 或 complete；检查通过后才能完成", "/api/environment/initialization", object(map[string]string{"action": "string"}))
	post("environment.start", "启动环境或继续安装。已有环境可省略 guest_system；可用系统请查询 guest.options", "/api/start", from(guestInstallRequest{}))
	post("environment.stop", "停止环境和活动终端，保留已保存的数据", "/api/stop", object(nil))
	get("environment.versions", "查看已安装的环境组件与工具版本", "/api/environment/versions")
	post("environment.update", "将工具更新到本版本指定的版本", "/api/environment/update", object(nil))
	post("environment.tools", "Update tools to this build's versions", "/api/environment/tools", object(nil))
	get("resources.get", "查看当前平台的资源上限和建议配置", "/api/environment/resources")
	post("resources.set", "用 allocation={automatic,memory_gib,cpus} 保存资源配置，再单独发送 {} 应用并重启", "/api/environment/resources", object(map[string]string{"allocation": "object"}))
	ops[len(ops)-1].InputSchema["properties"].(map[string]any)["allocation"] = object(map[string]string{"automatic": "boolean", "memory_gib": "integer", "cpus": "integer"})
	get("storage.get", "查看存储位置和迁移条件", "/api/environment/storage")
	post("storage.set", "使用 storage.get 返回的原目录及 has_environment 迁移存储；迁移会停止当前任务", "/api/environment/storage", object(map[string]string{"directory": "string", "previous": "string", "migrate": "boolean"}))
	post("network.check", "运行网络与隔离检查；通过后可能恢复联网", "/api/safety/check", object(nil))
	post("network.pause", "暂停客机外网连接并保存暂停状态", "/api/safety/close", object(nil))
	post("project.open", "只授权用户选中的目录；设置 add=true 可保存授权", "/api/projects/open", object(map[string]string{"workspace": "string", "add": "boolean", "activate": "boolean"}))
	post("project.save", "保存项目名称及用户授权的文件夹。新项目省略 workspace，以第一个目录为默认目录；编辑时使用原有 workspace 并保留该目录。若 macOS 要求授权，请在批准后重试原请求", "/api/projects/save", from(projectGroup{}))
	post("project.remove", "移除项目并撤销所有关联目录的共享。请先关闭项目终端、运行中的会话和编辑器；文件和历史记录会保留", "/api/projects/remove", object(map[string]string{"workspace": "string"}))
	post("project.refresh", "刷新 Linux 客机的项目文件名及属性缓存，立即显示主机上的改动", "/api/projects/refresh", object(nil))

	for _, action := range []string{"profile", "password", "lock", "logout", "suspend"} {
		schema := object(nil)
		if action == "profile" {
			schema = object(map[string]string{"username": "string", "avatar": "string"})
		}
		if action == "password" {
			schema = object(map[string]string{"old_password": "string", "password": "string", "confirm_password": "string"})
		}
		post("account."+action, "执行账户操作，采用与工作台相同的验证方式", "/api/account/"+action, schema)
	}
	get("appearance.get", "读取外观设置和已保存主题", "/api/appearance")
	post("appearance.set", "按工作台的校验规则保存完整外观设置", "/api/appearance", from(Appearance{}))
	get("appearance.codex", "读取可从本机 Codex 导入的外观设置", "/api/appearance/codex")
	post("settings.probe", "按当前填写的完整设置检测实际网络出口位置", "/api/settings/probe", from(Settings{}))
	get("proxy.connections", "查看代理的实时连接", "/api/proxy/connections")
	get("proxy.logs", "查看代理日志", "/api/proxy/logs")
	post("proxy.delay", "测量单个代理节点的延迟", "/api/proxy/delay", object(map[string]string{"node_id": "string"}))
	add("guest.reselect", "清除尚未启动且可在界面重新选择的安装任务", "DELETE", "/api/environment/guest-systems", object(map[string]string{"id": "string"}))
	get("claude-accounts.get", "查看兼容终端的账号、配额和调度设置", "/api/claude-accounts")
	post("claude-accounts.change", "加入或取消账号、修改设置、调度、刷新或移除，与工作台行为一致", "/api/claude-accounts", from(claudeAccountInput{}))
	get("vault.get", "查看条目资料，密码保持隐藏", "/api/vault")
	post("vault.change", "保存、删除或取消阻止；显示密码时需通过 account_password 再次验证", "/api/vault", object(map[string]string{"action": "string", "entry": "object", "id": "string", "origin": "string", "account_password": "string"}))
	ops[len(ops)-1].InputSchema["properties"].(map[string]any)["entry"] = from(vaultEntry{})
	post("vault.browser", "执行浏览器密码库已有的保存、填充或阻止操作", "/api/vault/browser", from(vaultBrowserInput{}))
	get("mobile.get", "查看手机配对状态", "/api/mobile-pair")
	post("mobile.pair", "生成手机配对邀请", "/api/mobile-pair", object(map[string]string{"admin": "string", "relay": "string"}))
	add("mobile.remove", "移除已配对的设备", "DELETE", "/api/mobile-pair", object(nil))
	get("tailscale.get", "读取客机专用网络状态", "/api/tailscale")
	for _, action := range []string{"install", "connect", "login", "disconnect", "logout", "add-key", "remove-key"} {
		schema := object(nil)
		if action == "add-key" || action == "remove-key" {
			schema = object(map[string]string{"key": "string"})
		}
		post("tailscale."+action, "执行工作台支持的 Tailscale 操作", "/api/tailscale/"+action, schema)
	}
	get("browser.preferences.get", "读取浏览器主页", "/api/browser/preferences")
	post("browser.preferences.set", "保存浏览器主页；失败时按工作台的规则恢复", "/api/browser/preferences", object(map[string]string{"browser_home_url": "string"}))
	get("browser.tabs", "读取浏览器标签页和状态", "/api/browser/tabs")
	for _, action := range []string{"settings", "cookies", "extensions", "internal-page", "info", "new", "activate", "close", "navigate", "back", "forward", "reload", "editor", "editor-restart", "split"} {
		post("browser."+action, "执行工作台支持的浏览器操作", "/api/browser/"+action, from(browserInput{}))
	}
	get("favorites.get", "读取书签和书签文件夹", "/api/favorites")
	post("favorites.change", "保存、移动、重新排序或删除当前集合中的条目", "/api/favorites", from(favoriteChange{}))
	get("terminals.get", "查看终端会话", "/api/terminals")
	post("terminals.create", "在所选项目中新建工作台终端", "/api/terminals", from(terminalRequest{}))
	get("client.context", "使用 desktop 作用域查看当前桌面连接标识", "/api/client/context")
	get("client.hosts", "使用 desktop 作用域查看已保存的 SSH 主机", "/api/client/hosts")
	post("client.hosts.create", "将 SSH 主机保存到本机桌面配置", "/api/client/hosts", from(remotehost.HostProfile{}))
	get("client.local-status", "从桌面环境选择页查看本机工作环境", "/api/client/local-status")
	for _, action := range []string{"candidate", "commit", "cancel", "disconnect", "local-stop"} {
		post("client."+action, "通过桌面环境选择器选定连接或候选环境", "/api/client/"+action, from(switchRequest{}))
	}
	post("client.trusted-login", "通过桌面的可信登录模块执行 create、resume、enable、disable 或 protect", "/api/client/trusted-login", object(map[string]string{"action": "string", "password": "string", "connection_id": "string"}))
	post("native.submit", "使用 desktop 作用域，向桌面窗口提交原生操作或 clientPreferences 请求", "/api/agent/native", object(map[string]string{"request": "object", "connection_id": "string"}))

	get("diagnostics.status", "查看日志采集状态", "/api/diagnostics")
	return ops
}

type AgentError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	NextAction string `json:"next_action"`
	Retryable  bool   `json:"retryable"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func (e *AgentError) Error() string { return e.Message }

func agentHTTPError(code int) *AgentError {
	e := &AgentError{Code: "request_rejected", Message: "工作台拒绝了操作，请核对输入并运行 doctor。", NextAction: "doctor", HTTPStatus: code}
	switch code {
	case 401:
		e.Code = "authentication_failed"
		e.NextAction = "service.start"
	case 423:
		e.Code = "account_locked"
		e.Message = "请在 Gemini Cage 工作台解锁账户，然后重试。"
		e.NextAction = "account.unlock"
	case 403:
		e.Code = "user_action_required"
		e.Message = "请在本机系统或账户界面完成此操作。"
		e.NextAction = "open_desktop"
	case 409:
		e.Code = "state_conflict"
		e.Message = "当前环境状态不允许此操作，请先读取 status 再重试。"
		e.NextAction = "status"
	case 410:
		e.Code = "job_not_found"
		e.Message = "原任务已结束或不存在，请先查看状态再提交。"
		e.NextAction = "status"
	case 404:
		e.Code = "unsupported_operation"
		e.Message = "当前后台或客机不支持此操作。"
		e.NextAction = "capabilities"
	case 429, 503:
		e.Code = "service_busy"
		e.NextAction = "status"
		e.Retryable = true
	}
	return e
}

// AgentStartService starts only an absent service. It never hands off, upgrades
// or stops an existing controller as a side effect of an agent call.
func AgentStartService(ctx context.Context, configPath string) error {
	profile := filepath.Dir(configPath)
	c, err := connectController(profile)
	if err == nil {
		defer c.close()
		return agentCheckController(c, configPath)
	}
	if !errors.Is(err, errControllerUnavailable) || !controllerLockFree(profile) {
		return &AgentError{Code: "service_unavailable", Message: "无法验证本机后台身份，请保留现有数据并查看诊断记录。", NextAction: "doctor"}
	}
	if err := startController(configPath); err != nil {
		return &AgentError{Code: "service_start_failed", Message: "无法启动本机工作台后台，请运行 doctor 检查原因。", NextAction: "doctor"}
	}
	timer := time.NewTicker(100 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return &AgentError{Code: "service_start_timeout", Message: "后台尚未就绪，可能仍在启动，请稍后再试。", NextAction: "doctor", Retryable: true}
		case <-timer.C:
			c, err := connectController(profile)
			if err == nil {
				defer c.close()
				return agentCheckController(c, configPath)
			}
		}
	}
}

func agentCheckController(c *controllerClient, configPath string) error {
	want, err := currentController()
	if err != nil || !want.matches(c.launch.Controller) || c.launch.DesktopGateway || c.launch.ClientOnly {
		return &AgentError{Code: "controller_mismatch", Message: "请使用与当前运行的工作台同一版本的命令行工具。", NextAction: "open_desktop"}
	}
	if filepath.Clean(c.launch.ConfigPath) != filepath.Clean(configPath) {
		return &AgentError{Code: "config_path_mismatch", Message: "此目录的后台使用另一份配置文件，请使用原配置文件的绝对路径。", NextAction: "doctor"}
	}
	return nil
}

func agentConnect(ctx context.Context, configPath string) (*controllerClient, error) {
	c, err := connectController(filepath.Dir(configPath))
	if err != nil {
		return nil, &AgentError{Code: "service_unavailable", Message: "本机工作台后台未连接，请运行 service.start 启动服务，或运行 doctor 查看原因。", NextAction: "service.start"}
	}
	fail := func(err error) (*controllerClient, error) { c.close(); return nil, err }
	if err := agentCheckController(c, configPath); err != nil {
		return fail(err)
	}
	u, _ := url.Parse(c.launch.URL)
	code, err := c.callContext(ctx, "POST", "/api/session", map[string]string{"token": u.Fragment}, nil)
	if err != nil || code != 200 {
		return fail(agentHTTPError(401))
	}
	c.setTrustedViewCookie(u)
	return c, nil
}

func AgentCall(ctx context.Context, configPath, operation string, input json.RawMessage) (json.RawMessage, error) {
	return AgentCallTarget(ctx, configPath, AgentTarget{}, operation, input)
}

func agentValidateInput(input json.RawMessage, schema map[string]any) error {
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(input, &value) != nil || value == nil {
		return &AgentError{Code: "invalid_input", Message: "输入必须是单个 JSON 对象。", NextAction: "capabilities"}
	}
	properties := schema["properties"].(map[string]any)
	for key := range value {
		if _, ok := properties[key]; !ok {
			return &AgentError{Code: "invalid_input", Message: "输入含未知字段，请查看该操作的 input_schema。", NextAction: "capabilities"}
		}
	}
	return nil
}

func AgentJob(ctx context.Context, configPath, id string) (json.RawMessage, error) {
	if len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
		return nil, &AgentError{Code: "invalid_input", Message: "请输入 12 个字符的任务编号。", NextAction: "status"}
	}
	return agentRequest(ctx, configPath, "GET", "/api/agent/jobs/"+id, nil)
}

func agentRequest(ctx context.Context, configPath, method, path string, input json.RawMessage) (json.RawMessage, error) {
	response, err := AgentRequest(ctx, configPath, AgentTarget{}, method, path, strings.NewReader(string(input)), "application/json", "")
	return response.Data, err
}

func (s *Server) agentRoutes(mux routeRegistrar) {
	mux.HandleFunc("GET /api/agent/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job := s.jobs.snapshot()
		if job.ID == "" || job.ID != r.PathValue("id") {
			http.Error(w, "原任务已结束或不存在，请刷新任务状态。", http.StatusGone)
			return
		}
		jsonReply(w, job)
	})
	mux.HandleFunc("POST /api/agent/settings", s.agentSettings)
}

func agentRedact(data string) string {
	var value any
	if json.Unmarshal([]byte(data), &value) != nil {
		return diagnostics.Redact(data)
	}
	var clean func(any)
	clean = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if text, ok := item.(string); ok && (key == "error" || key == "log" || key == "message" || key == "reason" || strings.HasSuffix(key, "_error")) {
					v[key] = diagnostics.Redact(text)
				} else {
					clean(item)
				}
			}
		case []any:
			for _, item := range v {
				clean(item)
			}
		}
	}
	clean(value)
	body, _ := json.Marshal(value)
	return string(body)
}

func (s *Server) agentSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Patch  map[string]json.RawMessage `json:"patch"`
		DryRun bool                       `json:"dry_run"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if home, ok := in.Patch["browser_home_url"]; ok && !in.DryRun {
		if len(in.Patch) != 1 {
			http.Error(w, "请通过 browser.preferences.set 单独修改浏览器主页 browser_home_url。", 400)
			return
		}
		var value string
		if json.Unmarshal(home, &value) != nil {
			http.Error(w, "浏览器主页地址无效，请输入完整的 HTTP 或 HTTPS 地址。", 400)
			return
		}
		s.saveBrowserHome(w, r, value)
		return
	}
	if len(in.Patch) == 0 {
		http.Error(w, "请至少选择一项要修改的设置。", 400)
		return
	}
	propose := func() (savedProfile, error) {
		old, err := loadConfig(s.configPath)
		if err != nil {
			return savedProfile{}, err
		}
		s.mu.Lock()
		p := s.preferences
		s.mu.Unlock()
		data, _ := json.Marshal(settingsView(old, p))
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(data, &fields)
		if err := mergeAgentSettings(fields, in.Patch); err != nil {
			return savedProfile{}, err
		}
		data, _ = json.Marshal(fields)
		var input Settings
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil {
			return savedProfile{}, errors.New("设置项不存在或值无效。")
		}
		if _, err := validateBrowserHome(input.BrowserHomeURL); err != nil {
			return savedProfile{}, err
		}
		c, p, err := proposedSettings(input, old, s.profile)
		return savedProfile{Config: c, Preferences: p}, err
	}
	// Validate even when a different job is running; the real mutation reloads
	// current settings only after acquiring the shared operation lock.
	v, err := propose()
	if err != nil {
		requestError(w, err)
		return
	}
	if in.DryRun {
		jsonReply(w, settingsView(v.Config, v.Preferences))
		return
	}
	s.launch(w, "settings", func() error {
		v, err := propose()
		if err != nil {
			return err
		}
		return s.apply(v, true)
	})
}

// An exit_policy patch must retain its existing IP/region anchor when only one
// field changes. Null never clears a setting implicitly.
func mergeAgentSettings(current, patch map[string]json.RawMessage) error {
	for key, value := range patch {
		value = bytes.TrimSpace(value)
		if bytes.Equal(value, []byte("null")) {
			return errors.New("设置内容不能为 null。")
		}
		var nested map[string]json.RawMessage
		if len(value) > 0 && value[0] == '{' && json.Unmarshal(value, &nested) == nil {
			existing := map[string]json.RawMessage{}
			_ = json.Unmarshal(current[key], &existing)
			if existing == nil {
				existing = map[string]json.RawMessage{}
			}
			if err := mergeAgentSettings(existing, nested); err != nil {
				return err
			}
			value, _ = json.Marshal(existing)
		}
		current[key] = value
	}
	return nil
}

func agentSchema(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		properties := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			if f.Anonymous {
				for name, schema := range agentSchema(f.Type)["properties"].(map[string]any) {
					properties[name] = schema
				}
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				properties[name] = agentSchema(f.Type)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": agentSchema(t.Elem())}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": agentSchema(t.Elem())}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{"type": "string"}
	}
}
