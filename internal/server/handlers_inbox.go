package server

import (
	"encoding/json"
	"net/http"
	"time"

	"voyage/internal/inbox"
	"voyage/internal/run"
)

// 麻烦收件箱接口（P1）：一句话 → 一张任务卡。
//
//	GET  /api/inbox                 全部条目（新的在前）
//	POST /api/inbox                 {"text":"..."} 收一条
//	POST /api/inbox/promote         {"id","goal","check","hint"} 变成任务卡
//	POST /api/inbox/drop            {"id"} 决定不做
func (s *Server) registerInboxHandlers(mux *http.ServeMux) {
	if s.Inbox == nil {
		s.Inbox = inbox.New(s.Dir)
		s.Inbox.Load()
	}

	mux.HandleFunc("/api/inbox", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"items": s.Inbox.List(), "open": len(s.Inbox.Open())})
		case http.MethodPost:
			var req struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, 400, "请求格式不对")
				return
			}
			it, err := s.Inbox.Add(req.Text)
			if err != nil {
				writeErr(w, 400, err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "item": it})
		default:
			writeErr(w, 405, "只支持 GET / POST")
		}
	})

	mux.HandleFunc("/api/inbox/promote", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			ID    string `json:"id"`
			Goal  string `json:"goal"`
			Check string `json:"check"`
			Hint  string `json:"hint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeErr(w, 400, "需要 id")
			return
		}
		if req.Check == "" {
			writeErr(w, 400, "任务卡必须有验收标准——没有验收的关卡不许开")
			return
		}
		it, ok := s.Inbox.Promote(req.ID, req.Goal, req.Check, req.Hint)
		if !ok {
			writeErr(w, 404, "没有这条、或它已经不是「待处理」")
			return
		}
		writeJSON(w, map[string]any{"ok": true, "item": it})
	})

	mux.HandleFunc("/api/inbox/drop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeErr(w, 400, "需要 id")
			return
		}
		it, ok := s.Inbox.Drop(req.ID)
		if !ok {
			writeErr(w, 404, "没有这条、或它已经不是「待处理」")
			return
		}
		writeJSON(w, map[string]any{"ok": true, "item": it})
	})

	// 从收件箱直接开一关：只有这一关，验收标准来自任务卡。
	mux.HandleFunc("/api/inbox/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			ID    string `json:"id"`
			Goal  string `json:"goal"`
			Check string `json:"check"`
			Hint  string `json:"hint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			writeErr(w, 400, "需要 id")
			return
		}
		check := req.Check
		if check == "" {
			writeErr(w, 400, "任务卡必须有验收标准——没有验收的关卡不许开")
			return
		}
		goal := req.Goal
		if goal == "" {
			goal = "把这件事做掉"
		}
		// 先改收件箱状态再动 RunState：反过来会出现「报错了但关卡已经变了」的脏状态
		if _, ok := s.Inbox.Promote(req.ID, goal, check, req.Hint); !ok {
			writeErr(w, 400, "这条已经处理过了（或者不存在）")
			return
		}
		s.RunState = &run.Run{
			ProjectID: "inbox",
			Title:     "收件箱任务",
			Tier:      run.TierTool,
			Stages:    []run.Stage{{ID: "inbox-1", Goal: goal, Check: check, Hint: req.Hint}},
			Results:   map[string]string{},
			Started:   time.Now().Format(time.RFC3339),
		}
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "stage": s.RunState.CurrentStage(), "progress": s.RunState.Progress()})
	})
}
