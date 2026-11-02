// handlers_insight.go —— 重要发现：看、手动补一条。
package server

import (
	"encoding/json"
	"net/http"

	"voyage/internal/insight"
)

func (s *Server) registerInsight(mux *http.ServeMux) {
	mux.HandleFunc("/api/insights", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"items": insight.Load(s.Dir)})
		case http.MethodPost:
			var req struct {
				Text string `json:"text"`
				From string `json:"from"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, 400, "请求格式不对")
				return
			}
			it, created, err := insight.Add(s.Dir, req.Text, req.From)
			if err != nil {
				writeErr(w, 500, "没记上")
				return
			}
			writeJSON(w, map[string]any{"ok": true, "created": created, "item": it})
		default:
			writeErr(w, 405, "只接受 GET/POST")
		}
	})
}
