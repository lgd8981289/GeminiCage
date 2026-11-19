package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// AgentSocket carries the same WebSocket messages as the UI without exporting
// session cookies. One NDJSON line in/out is {type:"text"|"binary",data:...};
// binary data uses JSON's []byte base64 encoding.
func AgentSocket(ctx context.Context, configPath string, target AgentTarget, path string, input io.ReadCloser, output io.Writer) (returned error) {
	if !strings.HasPrefix(path, "/api/") || strings.ContainsAny(path, "\r\n\\") || strings.Contains(path, "#") || strings.Contains(path, "://") {
		return &AgentError{Code: "invalid_input", Message: "请使用以 /api/ 开头的相对 WebSocket 路径。", NextAction: "routes"}
	}
	c, err := agentConnectTarget(ctx, configPath, target)
	if err != nil {
		return err
	}
	defer c.close()
	if target.Scope == "desktop" {
		return &AgentError{Code: "invalid_input", Message: "连接工作环境的 WebSocket 时，请使用 local 或 selected 作用域。", NextAction: "client.context"}
	}
	if target.Scope == "client" && target.Connection == "" {
		return &AgentError{Code: "invalid_input", Message: "建立客户端连接时必须明确指定当前连接标识。", NextAction: "client.context"}
	}
	connection := "local"
	if target.Connection != "" {
		connection = target.Connection
	}
	req, _ := http.NewRequest("GET", c.origin, nil)
	headers := http.Header{"Origin": []string{c.origin}, connectionHeader: []string{connection}}
	for _, cookie := range c.client.Jar.Cookies(req.URL) {
		headers.Add("Cookie", cookie.String())
	}
	dialer := websocket.Dialer{Proxy: nil, HandshakeTimeout: 15 * time.Second}
	ws, response, err := dialer.DialContext(ctx, "ws"+strings.TrimPrefix(c.origin, "http")+path, headers)
	if err != nil {
		if response != nil {
			return agentHTTPError(response.StatusCode)
		}
		return &AgentError{Code: "connection_failed", Message: "WebSocket 连接失败，请检查工作台服务。", NextAction: "status"}
	}
	defer ws.Close()
	ws.SetReadLimit(32 << 20)
	stop := context.AfterFunc(ctx, func() { ws.Close() })
	defer stop()
	type frame struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	var mutex sync.Mutex
	emit := func(value any) error {
		mutex.Lock()
		defer mutex.Unlock()
		return json.NewEncoder(output).Encode(value)
	}
	if err := emit(map[string]any{"protocol": AgentProtocol, "status": "connected"}); err != nil {
		return err
	}
	inputDone := make(chan error, 1)
	var closing, inputFailed atomic.Bool
	defer func() {
		closing.Store(true)
		input.Close()
		<-inputDone
		if inputFailed.Load() {
			returned = &AgentError{Code: "invalid_input", Message: "WebSocket 输入不符合 NDJSON 格式，该条输入未执行。", NextAction: "capabilities"}
		}
	}()
	go func() {
		decoder := json.NewDecoder(input)
		for {
			var value frame
			if err := decoder.Decode(&value); err != nil {
				if !errors.Is(err, io.EOF) && !closing.Load() {
					inputFailed.Store(true)
					ws.Close()
				}
				inputDone <- err
				return
			}
			var body []byte
			kind := websocket.TextMessage
			switch value.Type {
			case "text":
				var text string
				if json.Unmarshal(value.Data, &text) != nil {
					inputFailed.Store(true)
					inputDone <- io.ErrUnexpectedEOF
					ws.Close()
					return
				}
				body = []byte(text)
			case "binary":
				kind = websocket.BinaryMessage
				if json.Unmarshal(value.Data, &body) != nil {
					inputFailed.Store(true)
					inputDone <- io.ErrUnexpectedEOF
					ws.Close()
					return
				}
			default:
				inputFailed.Store(true)
				inputDone <- io.ErrUnexpectedEOF
				ws.Close()
				return
			}
			ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
			if err := ws.WriteMessage(kind, body); err != nil {
				inputDone <- err
				ws.Close()
				return
			}
		}
	}()
	for {
		kind, body, err := ws.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			return &AgentError{Code: "connection_closed", Message: "WebSocket 已关闭，请先查看原会话状态再连接。", NextAction: "status"}
		}
		var value any
		switch kind {
		case websocket.TextMessage:
			value = map[string]any{"type": "text", "data": string(body)}
		case websocket.BinaryMessage:
			value = map[string]any{"type": "binary", "data": body}
		default:
			continue
		}
		if err := emit(value); err != nil {
			return err
		}
	}
}
