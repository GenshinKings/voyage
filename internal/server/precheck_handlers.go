package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"voyage/internal/llm"
	"voyage/internal/precheck"
)

// 序列级难度闸门：开课前先判「这节对他是不是太难」。
// 由来：docs/audit/2026-11-03-偏离审查.md 偏离三（第二课就被术语砸懵，没有闸门）。
//
// 闸门只给建议，不擅自改课——改课是人的事；但它会留痕，
// 方便复盘「哪节课一直超纲」。

func (s *Server) registerPrecheckHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/lesson/precheck", func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("nodeId")
		lesson := findLesson(s, nodeID)
		if lesson == nil {
			writeErr(w, 404, "这节课不存在")
			return
		}
		if s.Tutor == nil || s.Tutor.Client == nil {
			writeErr(w, 400, "模型没连上，判不了")
			return
		}

		// 判官要知道「他手里现在有什么」：卡过的点 + 上一节讲过的内容。
		learner := precheck.Learner{}
		if prev := s.Tutor.TodayLesson(); prev != nil {
			learner.LastChecks = append(learner.LastChecks, prev.Brief)
		}
		for _, wp := range s.Weak.Recent(6) {
			learner.WeakPoints = append(learner.WeakPoints, wp.Topic)
		}

		target := precheck.Lesson{
			Title: lesson.Title,
			Goal:  lesson.Brief,
		}

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		chat := func(ctx context.Context, sys, user string) (string, error) {
			// ChatClient 没有带 ctx 的方法，这里用 goroutine + 超时守住不无限等。
			type res struct {
				out string
				err error
			}
			ch := make(chan res, 1)
			go func() {
				out, err := s.Tutor.Client.Chat(sys, []llm.Message{{Role: "user", Content: user}})
				ch <- res{out, err}
			}()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case got := <-ch:
				return got.out, got.err
			}
		}

		v, err := precheck.Judge(ctx, chat, learner, target)
		if err != nil {
			writeErr(w, 502, "审查员没回话："+err.Error())
			return
		}
		if v.Verdict != precheck.VerdictOK {
			log.Printf("难度闸门：%s → %s（%s）", lesson.Title, v.Verdict, v.Why)
		}
		writeJSON(w, map[string]any{
			"nodeId": nodeID,
			"title":  lesson.Title,
			"judge":  v,
		})
	})
}
