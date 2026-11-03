package server

import (
	"context"
	"encoding/json"
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
		for _, x := range res {
			if x.Pass {
				pass++
			}
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
