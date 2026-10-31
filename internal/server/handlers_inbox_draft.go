package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"voyage/internal/llm"
)

// 任务卡自动起草（P1 第二半）：把一句「真麻烦」变成 goal / check / hint 草案。
//
// 设计约束：
//   - 只出草案，不直接开任务。人必须看一眼再 promote（草图 ≠ 承诺）
//   - 验收标准必须是「可观察」的：跑得起来 / 有输出 / 别人能照做，不许写「理解了」
//   - 没有模型时明确拒绝，不编一个假草案糊弄
const draftSystem = `你是任务卡的起草员。学习者给你一句「真实麻烦」，你要把它变成一张能立刻动手的任务卡。

规则：
- goal：一句话说清「做完什么算把这事解决」，要小到 30~60 分钟能做完
- check：验收标准，必须是可观察的（跑出一条命令看到输出、贴出结果、别人照着能做）。
  绝对不许写「理解了」「掌握了」这种没法验证的话
- hint：只给方向或第一步（该先看什么、先敲什么），不给完整答案
- 如果这句麻烦大到一次做不完，就把 goal 收窄成「第一个能验证的小步」

输出严格 JSON（无围栏）：{"goal":"...","check":"...","hint":"..."}`

// registerInboxDraftHandler 装配起草接口。
func (s *Server) registerInboxDraftHandler(mux *http.ServeMux) {
	mux.HandleFunc("/api/inbox/draft", func(w http.ResponseWriter, r *http.Request) {
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
		if !s.hasModel() {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个，才能自动起草任务卡")
			return
		}
		// 找到那条麻烦（只从 open 里起草：已处理的不该再生成）
		var text string
		for _, it := range s.Inbox.Open() {
			if it.ID == req.ID {
				text = it.Text
				break
			}
		}
		if text == "" {
			writeErr(w, 404, "没有这条待处理的麻烦")
			return
		}
		draft, err := s.draftTask(r.Context(), text)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "draft": draft, "item": text})
	})
}

// taskDraft 起草结果。
type taskDraft struct {
	Goal  string `json:"goal"`
	Check string `json:"check"`
	Hint  string `json:"hint"`
}

// draftTask 让模型把麻烦变成任务卡草案。
func (s *Server) draftTask(ctx context.Context, text string) (taskDraft, error) {
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, draftSystem,
		[]llm.Message{{Role: "user", Content: "麻烦：" + text}})
	if err != nil {
		return taskDraft{}, err
	}
	var d taskDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return taskDraft{}, err
	}
	d.Goal = strings.TrimSpace(d.Goal)
	d.Check = strings.TrimSpace(d.Check)
	d.Hint = strings.TrimSpace(d.Hint)
	if d.Check == "" {
		// 模型没给出可观察的验收：不给半成品草案，让人自己写
		d.Check = "（模型没给出可观察的验收标准，请自己写一条：做完能看到什么）"
	}
	return d, nil
}
