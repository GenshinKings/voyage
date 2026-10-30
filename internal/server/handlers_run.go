package server

import (
	"encoding/json"
	"net/http"

	"voyage/internal/review"
	"voyage/internal/run"
)

// registerRunHandlers 装配「从零做开源项目」的路由（三个接口，够前端接）。
func (s *Server) registerRunHandlers(mux *http.ServeMux) {
	// 模板列表：前端开场先让学习者选一档。
	mux.HandleFunc("/api/run/templates", func(w http.ResponseWriter, r *http.Request) {
		type tpl struct {
			ID    string `json:"id"`
			Tier  string `json:"tier"`
			Title string `json:"title"`
			Why   string `json:"why"`
			Steps int    `json:"steps"`
		}
		out := []tpl{}
		for _, p := range run.Templates() {
			out = append(out, tpl{ID: p.ID, Tier: run.TierName(p.Tier), Title: p.Title, Why: p.Why, Steps: len(p.Stages)})
		}
		writeJSON(w, map[string]any{"templates": out})
	})

	// 开始一档：只返回当前这一关，不给后面（避免「看完全程」的幻觉）。
	mux.HandleFunc("/api/run/start", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			TemplateID string `json:"templateId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		p, ok := run.TemplateByID(req.TemplateID)
		if !ok {
			writeErr(w, 400, "没有这个模板")
			return
		}
		r0, err := run.Start(p)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		s.RunState = r0
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "stage": r0.CurrentStage(), "progress": r0.Progress()})
	})

	// 交作业：过关才解锁下一关；不过关只给原因和「下一步」，绝不直接给答案。
	mux.HandleFunc("/api/run/submit", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Output string `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.RunState == nil {
			writeErr(w, 400, "还没开始一个项目")
			return
		}
		// Expect 为空的关卡机器看不出对错：交给模型判，判过再解锁。
		// 没配模型时退回「非空即过」，不让闯关卡死。
		var ok bool
		var reason string
		if st := s.RunState.CurrentStage(); st != nil && len(st.Expect) == 0 {
			pass, why, action, usable := s.judgeStage(r.Context(), *st, req.Output)
			if usable {
				ok, reason = s.RunState.SubmitJudged(req.Output, pass, why)
				if !ok && action != "" {
					reason = reason + " 下一步：" + action
				}
			} else {
				// 没有可用模型：不静默判过，进待判队列（第三方判定的兜底入口）
				it := s.Reviews.Enqueue(review.Item{
					StageID: st.ID,
					Target:  st.Goal,
					Output:  req.Output,
					Reason:  "没有可用模型，自动判定不了，转为待判",
				})
				writeJSON(w, map[string]any{"ok": false, "pendingReview": it.ID,
					"reason": "这一关机器判不了，已记成待判——去「待判」里自己判一下（或接个模型）",
					"hint":   st.Hint, "progress": s.RunState.Progress()})
				return
			}
		} else {
			ok, reason = s.RunState.Submit(req.Output)
		}
		s.saveState()
		resp := map[string]any{"ok": ok, "progress": s.RunState.Progress()}
		if !ok {
			resp["reason"] = reason
			resp["hint"] = currentHint(s.RunState)
		} else {
			resp["stage"] = s.RunState.CurrentStage()
			resp["finished"] = s.RunState.Finished
			if s.RunState.Finished {
				resp["deliverable"] = s.RunState.Deliverable()
			}
		}
		writeJSON(w, resp)
	})

	// 看进度：当前关 + 已完成几关。
	mux.HandleFunc("/api/run/state", func(w http.ResponseWriter, r *http.Request) {
		if s.RunState == nil {
			writeJSON(w, map[string]any{"started": false})
			return
		}
		writeJSON(w, map[string]any{
			"started":  true,
			"title":    s.RunState.Title,
			"tier":     run.TierName(s.RunState.Tier),
			"progress": s.RunState.Progress(),
			"stage":    s.RunState.CurrentStage(),
			"finished": s.RunState.Finished,
		})
	})

	// 教学指标（docs/teach/spec.md 第八节）：独立通过率 / 错误类型分布 / 退档后回升率。
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		if s.Tutor == nil {
			writeJSON(w, map[string]any{"judged": 0, "note": "助教还没起来"})
			return
		}
		writeJSON(w, s.Tutor.LoadStats().Snapshot())
	})
}

// currentHint 只取当前关卡的「下一步」，不给答案。
func currentHint(r *run.Run) string {
	if st := r.CurrentStage(); st != nil {
		return st.Hint
	}
	return ""

}
