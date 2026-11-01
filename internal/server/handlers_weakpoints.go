package server

// handlers_weakpoints.go —— 让「助教觉得你哪里理解错了」看得见、也能删。
//
// 为什么必须有这一屏：昨天把薄弱点记下来了（批注时逐条落盘），但用户看不到，
// 等于系统在他背后给人贴了标签却从不告诉他。而且清单只能增不能删，
// 时间一长就没人看——所以这里一并给出「删掉这条」。

import (
	"encoding/json"
	"net/http"
	"strings"

	"voyage/internal/weakpoints"
)

func (s *Server) registerWeakpointHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/weakpoints", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			list := s.weakList()
			writeJSON(w, map[string]any{"items": list, "count": len(list)})
		case http.MethodPost:
			// 自己也能标一条：有些东西助教没看出来，但我知道自己没懂。
			var in struct {
				Topic  string `json:"topic"`
				Detail string `json:"detail"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				writeErr(w, 400, "请求格式不对")
				return
			}
			if strings.TrimSpace(in.Topic) == "" {
				writeErr(w, 400, "写个知识点名字")
				return
			}
			it := s.Weak.Add(in.Topic, in.Detail, "self")
			writeJSON(w, map[string]any{"ok": true, "item": it, "items": s.weakList()})
		case http.MethodDelete:
			var req struct {
				Topic string `json:"topic"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, 400, "请求格式不对")
				return
			}
			if strings.TrimSpace(req.Topic) == "" {
				writeErr(w, 400, "要删哪一条？传 topic")
				return
			}
			if !s.weakRemove(req.Topic) {
				writeErr(w, 404, "没这条")
				return
			}
			writeJSON(w, map[string]any{"ok": true, "items": s.weakList()})
		default:
			writeErr(w, 405, "只支持 GET/POST/DELETE")
		}
	})
}

// weakList 读薄弱点（顺带兜住 Weak 为 nil 的情况）。
func (s *Server) weakList() []weakpoints.Item {
	if s.Weak == nil {
		return []weakpoints.Item{}
	}
	return s.Weak.List()
}

// weakRemove 删一条薄弱点。
func (s *Server) weakRemove(topic string) bool {
	if s.Weak == nil {
		return false
	}
	return s.Weak.Remove(topic)
}
