package server

import (
	"encoding/json"
	"net/http"

	"voyage/internal/review"
)

// registerReviewHandlers 第三方判定的接口（单人本地版：判卷人就是用户自己）。
//
// 设计见 docs/teach/external-judge.md：裁判只回答「过不过、差在哪」，不讲解。
func (s *Server) registerReviewHandlers(mux *http.ServeMux) {
	if s.Reviews == nil {
		s.Reviews = review.New(s.Dir)
		s.Reviews.Load()
	}

	// 待判列表
	mux.HandleFunc("/api/reviews", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "只支持 GET")
			return
		}
		pending := s.Reviews.Pending()
		writeJSON(w, map[string]any{"pending": pending, "count": len(pending)})
	})

	// 下结论：POST /api/reviews/verdict {"id": "...", "pass": true, "where": "", "action": ""}
	mux.HandleFunc("/api/reviews/verdict", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			ID     string `json:"id"`
			Pass   bool   `json:"pass"`
			Where  string `json:"where"`
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "请求格式不对")
			return
		}
		if req.ID == "" {
			writeErr(w, 400, "缺少 id")
			return
		}
		item, ok := s.Reviews.Decide(req.ID, req.Pass, req.Where, req.Action, "self")
		if !ok {
			writeErr(w, 404, "没有这条待判记录（或已经判过）")
			return
		}
		// 判过之后要把结果落回闯关状态：只有「当前这一关」能直接推进，
		// 免得判卷人点了通过却发现进度没动。
		advanced := false
		if item.StageID != "" && s.RunState != nil {
			if st := s.RunState.CurrentStage(); st != nil && st.ID == item.StageID {
				if ok2, _ := s.RunState.SubmitJudged(item.Output, item.Status == review.StatusApproved, ""); ok2 {
					s.saveState()
					advanced = true
				}
			}
		}
		writeJSON(w, map[string]any{"ok": true, "item": item, "advanced": advanced})
	})
}
