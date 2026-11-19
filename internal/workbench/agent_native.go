package workbench

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type agentNativeItem struct {
	ID         string          `json:"id"`
	Connection string          `json:"connection_id"`
	Request    json.RawMessage `json:"request,omitempty"`
	Status     string          `json:"status"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	created    time.Time
}
type agentNativeQueue struct {
	mu      sync.Mutex
	items   []*agentNativeItem
	windows map[string]time.Time
}

// This queue belongs to the window listener, not the selected workload. It
// invokes the exact installed native bridge, including its platform checks.
func (s *Server) agentNativeRoutes(mux routeRegistrar) {
	mux.HandleFunc("GET /api/agent/native", s.agentNative)
	mux.HandleFunc("POST /api/agent/native", s.agentNative)
	mux.HandleFunc("POST /api/agent/native/window", s.agentNativeWindow)
	mux.HandleFunc("GET /api/agent/native/{id}", s.agentNative)
	mux.HandleFunc("POST /api/agent/native/{id}/complete", s.agentNative)
}

func (s *Server) agentNative(w http.ResponseWriter, r *http.Request) {
	q := &s.agentNativeQueue
	id := r.PathValue("id")
	if r.Method == "POST" && id == "" {
		if !s.hasTrustedView(r) {
			http.Error(w, "请通过本机启动器提交请求。", 403)
			return
		}
		var in struct {
			Request    json.RawMessage `json:"request"`
			Connection string          `json:"connection_id"`
		}
		if !readJSON(w, r, &in, 12<<20) {
			return
		}
		var request map[string]json.RawMessage
		if json.Unmarshal(in.Request, &request) != nil || request == nil || in.Connection == "" {
			http.Error(w, "本机操作需要提供请求内容和明确的 connection_id。", 400)
			return
		}
		var action string
		_ = json.Unmarshal(request["action"], &action)
		if action == "" {
			http.Error(w, "此操作需要在本机应用中完成。", 400)
			return
		}
		if s.remote != nil && s.remote.context().ConnectionID != in.Connection {
			http.Error(w, "工作环境已切换，请在当前环境重新操作。", 409)
			return
		}
		q.mu.Lock()
		defer q.mu.Unlock()
		now := time.Now()
		kept := q.items[:0]
		for _, item := range q.items {
			if item.Status == "pending" && now.Sub(item.created) > 2*time.Minute {
				item.Status = "failed"
				item.Error = "Desktop did not claim this request in time. No native action was executed."
				item.Request = nil
			}
			if now.Sub(item.created) < 30*time.Minute {
				kept = append(kept, item)
			}
		}
		q.items = kept
		if len(q.items) >= 32 {
			for i, item := range q.items {
				if item.Status == "completed" || item.Status == "failed" {
					q.items = append(q.items[:i], q.items[i+1:]...)
					break
				}
			}
		}
		if len(q.items) >= 32 {
			http.Error(w, "请先读取或完成待处理的本机请求，再提交新请求。", 409)
			return
		}
		item := &agentNativeItem{ID: randomToken()[:24], Request: append(json.RawMessage(nil), in.Request...), Connection: in.Connection, Status: "pending", created: now}
		q.items = append(q.items, item)
		w.WriteHeader(http.StatusAccepted)
		jsonReply(w, item)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if (r.Method == "GET" && id == "") || r.Method == "POST" {
		until, ok := q.windows[r.Header.Get("X-Cage-Agent-Window")]
		if !ok || time.Now().After(until) {
			http.Error(w, "请从正在使用的本机窗口重新发起操作。", 403)
			return
		}
	}
	if r.Method == "GET" && id == "" {
		if r.URL.Query().Get("consumer") != "window" {
			http.Error(w, "请先打开接收请求的桌面窗口。", 400)
			return
		}
		for _, item := range q.items {
			if item.Status == "pending" && item.Connection == requestConnection(r) {
				if time.Since(item.created) > 2*time.Minute {
					item.Status = "failed"
					item.Error = "Desktop did not claim this request in time. No native action was executed."
					item.Request = nil
					continue
				}
				item.Status = "running"
				jsonReply(w, item)
				return
			}
		}
		jsonReply(w, map[string]any{})
		return
	}
	for _, item := range q.items {
		if item.ID == id {
			if item.Status == "pending" && time.Since(item.created) > 2*time.Minute {
				item.Status = "failed"
				item.Error = "Desktop did not claim this request in time. No native action was executed."
				item.Request = nil
			}
			if r.Method == "POST" {
				if item.Status == "completed" || item.Status == "failed" {
					jsonReply(w, map[string]bool{"ok": true})
					return
				}
				if item.Status != "running" {
					http.Error(w, "本机窗口尚未接收此请求。", 409)
					return
				}
				var in struct {
					Result json.RawMessage `json:"result"`
					Error  string          `json:"error"`
				}
				if !readJSON(w, r, &in, 30<<20) {
					return
				}
				item.Result = in.Result
				item.Error = in.Error
				item.Request = nil
				item.Status = "completed"
				if in.Error != "" {
					item.Status = "failed"
				}
				jsonReply(w, map[string]bool{"ok": true})
				return
			}
			if !s.hasTrustedView(r) {
				http.Error(w, "请通过本机启动器提交操作结果。", 403)
				return
			}
			jsonReply(w, item)
			return
		}
	}
	http.Error(w, "找不到本机请求，请先确认操作状态再重试。", 410)
}

func (s *Server) agentNativeWindow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Ticket string `json:"ticket"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.consumeTicket(in.Ticket) {
		http.Error(w, "启动凭据已失效，请从本机应用重新打开工作台。", 403)
		return
	}
	q := &s.agentNativeQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.windows == nil {
		q.windows = map[string]time.Time{}
	}
	now := time.Now()
	for proof, until := range q.windows {
		if now.After(until) {
			delete(q.windows, proof)
		}
	}
	if len(q.windows) >= 64 {
		var oldest string
		var earliest time.Time
		for proof, until := range q.windows {
			if oldest == "" || until.Before(earliest) {
				oldest, earliest = proof, until
			}
		}
		delete(q.windows, oldest)
	}
	proof := randomToken()
	q.windows[proof] = now.Add(12 * time.Hour)
	jsonReply(w, map[string]string{"window_proof": proof})
}
