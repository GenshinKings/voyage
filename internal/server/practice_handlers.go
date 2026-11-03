package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"voyage/internal/llm"
	"voyage/internal/project"
)

// 「给一个项目 + 一份 README，我自己做，助教对照验收」（主人 2026-11-03 23:03）。
//
// 落在：桌面\小穗\10-项目\voyage-练习\<项目名>\
// 验收项必须是可执行的（跑什么命令 → 该看到什么），检查时逐条跑。

// practiceRoot 练习根目录：主人定的位置优先，机器上没有就退到 data 下。
func practiceRoot(s *Server) string {
	return project.PickRoot([]string{
		`D:\桌面\小穗\10-项目\voyage-练习`,
		filepath.Join(s.Dir, "practice"),
	})
}

func (s *Server) registerProjectHandlers(mux *http.ServeMux) {
	// 出题 + 建文件夹
	mux.HandleFunc("/api/practice/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只能用 POST")
			return
		}
		var req struct {
			Brief string `json:"brief"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Brief) == "" {
			writeErr(w, 400, "说说你想练什么（一句话就行）")
			return
		}
		if s.Tutor == nil || s.Tutor.Client == nil {
			writeErr(w, 400, "模型没连上，出不了题")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		chat := func(ctx context.Context, sys, user string) (string, error) {
			out, err := s.Tutor.Client.Chat(sys, []llm.Message{{Role: "user", Content: user}})
			if err != nil {
				return "", err
			}
			return out, nil
		}
		root := practiceRoot(s)
		// 闭环：没过关的副本挡在前面，不许开新的（「过关则副本完成」不是装饰）
		if blocked, yes := project.Blocking(root); yes {
			writeErr(w, 409, "还有一个副本没过：「"+blocked.Title+"」。先把这一关过了，再开新的")
			return
		}
		if err := project.EnsureRoot(root); err != nil {
			writeErr(w, 500, "建不了练习目录："+err.Error())
			return
		}
		rd, err := project.Generate(ctx, chat, req.Brief)
		if err != nil {
			writeErr(w, 502, "出题不合格，退回去重出："+err.Error())
			return
		}
		dir, err := project.Create(root, rd)
		if err != nil {
			writeErr(w, 500, "写不出项目："+err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "dir": dir, "readme": rd})
	})

	// 助教按「目标 + 当前学到哪 + 卡过什么」安排下一个副本（主人 2026-11-03 23:57：
	// 副本该按学习目标和当前情况制定，不是摆一排让人挑）。
	mux.HandleFunc("/api/practice/next", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只能用 POST")
			return
		}
		if s.Tutor == nil || s.Tutor.Client == nil {
			writeErr(w, 400, "模型没连上，安排不了")
			return
		}
		root := practiceRoot(s)
		// 已经有一个没过关的：就练它，不再生成新的（不摆货架）。
		if cur, yes := project.Blocking(root); yes {
			writeJSON(w, map[string]any{"ok": true, "reuse": true, "dir": cur.Dir, "item": cur})
			return
		}

		st := project.State{}
		if cur := s.Tutor.TodayLesson(); cur != nil {
			st.Goal = cur.Subject
			st.CurrentLsn = cur.Title
			st.Checklist = append(st.Checklist, cur.Brief)
		}
		for _, wp := range s.Weak.Recent(6) {
			st.Weak = append(st.Weak, wp.Topic)
		}
		for _, it := range project.ListDirs(root) {
			if it.State == project.StatusPassed {
				st.Passed = append(st.Passed, it.Title)
			} else {
				st.Failed = append(st.Failed, it.Title)
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
		defer cancel()
		chat := func(ctx context.Context, sys, user string) (string, error) {
			return s.Tutor.Client.Chat(sys, []llm.Message{{Role: "user", Content: user}})
		}
		plan, err := project.PlanNext(ctx, chat, st)
		if err != nil {
			writeErr(w, 502, "助教没排出安排："+err.Error())
			return
		}
		if err := project.EnsureRoot(root); err != nil {
			writeErr(w, 500, "建不了练习目录："+err.Error())
			return
		}
		rd, err := project.Generate(ctx, chat, plan.Brief)
		if err != nil {
			writeErr(w, 502, "出题不合格："+err.Error())
			return
		}
		dir, err := project.Create(root, rd)
		if err != nil {
			writeErr(w, 500, "写不出项目："+err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "dir": dir, "plan": plan, "readme": rd})
	})
	// 列出所有副本与状态
	mux.HandleFunc("/api/practice/list", func(w http.ResponseWriter, r *http.Request) {
		root := practiceRoot(s)
		list := project.ListDirs(root)
		blocked, hasBlocked := project.Blocking(root)
		writeJSON(w, map[string]any{
			"root":       root,
			"items":      list,
			"hasBlocked": hasBlocked,
			"blocked":    blocked,
		})
	})
	// 检查完成情况：逐条跑验收
	mux.HandleFunc("/api/practice/check", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		if dir == "" {
			writeErr(w, 400, "要给 dir")
			return
		}
		rd, err := project.Load(dir)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		res := project.Run(dir, rd)
		pass := 0
		var failed []string
		for _, x := range res {
			if x.Pass {
				pass++
				continue
			}
			failed = append(failed, x.Note)
		}
		summary := fmt.Sprintf("%d/%d", pass, len(res))
		passed := pass == len(res)
		// 没过 = 有病灶：把没过的每条写成弱项，助教下次讲课/出题就会对着它使劲。
		// 这是「副本 → 教学」的回流，不然副本过了没过都白过。
		if !passed && s.Weak != nil {
			for _, it := range failed {
				s.Weak.Add(it, "副本「"+rd.Title+"」这条没过（"+summary+"）", "dungeon")
			}
		}
		// 提交即定状态：过关才算副本完成；没过退回 working，留在这一关。
		if _, err := project.Submit(dir, passed, failed, summary); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, map[string]any{
			"dir":   dir,
			"pass":  pass,
			"total": len(res),
			"items": res,
			// 没过就留在这一条，不往下走
			"done": pass == len(res),
		})
	})
}
