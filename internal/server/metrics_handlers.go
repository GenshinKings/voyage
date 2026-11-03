package server

import (
	"log"
	"net/http"
	"strconv"

	"voyage/internal/metrics"
)

// 北极星三信号：/api/metrics。
// 只算三件事——保持率、迁移命中、自发回归；不做图表、不做页面。
// 依据：docs/audit/2026-11-03-偏离审查.md（偏离一：北极星没被测量）。

func (s *Server) metricsStore() metrics.Store { return metrics.New(s.Dir) }

func (s *Server) registerMetricsHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		days := 7
		if v := r.URL.Query().Get("days"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				days = n
			}
		}
		st := s.metricsStore()
		writeJSON(w, map[string]any{
			"window":  days,
			"recent":  st.Summarize(days),
			"allTime": st.Summarize(0),
		})
	})
}

// 下面三个是「尽力而为」档（docs/eng/error-policy.md 第二档）：
// 记忆北极星数据失败不该挡住学习，但必须在日志里留痕，不许静默。

// recordRetention 复测到期作答时记一次。
// rating：1=忘了 2=想不起来 3=想起来 4=轻松。>=3 算答上来了。
func (s *Server) recordRetention(cardID string, rating int) {
	if cardID == "" {
		return
	}
	if err := s.metricsStore().Record(metrics.Event{
		Kind: metrics.KindRetention,
		Key:  cardID,
		OK:   rating >= 3,
		Note: "rating=" + strconv.Itoa(rating),
	}); err != nil {
		log.Printf("保持率没记上（不影响这次复测）: %v", err)
	}
}

// recordTransfer 复测过的知识点，在后面实战里被用上了。
func (s *Server) recordTransfer(key string) {
	if err := s.metricsStore().Record(metrics.Event{
		Kind: metrics.KindTransfer, Key: key, OK: true,
	}); err != nil {
		log.Printf("迁移信号没记上: %v", err)
	}
}

// markSession 每次打开界面调一次；相隔 >24h 又回来算自发回归。
func (s *Server) markSession() {
	if _, err := s.metricsStore().MarkSession(); err != nil {
		log.Printf("会话信号没记上: %v", err)
	}
}
