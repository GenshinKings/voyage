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
- hint：只给方向（该先看什么），不给完整答案
- firstStep：**照抄就能动的那一下**——打开哪个软件、在哪个目录、敲哪条命令、点哪个按钮；再附一句「做完你会看到什么」。新手卡住的不是不会做，是不知道从哪下手，所以这栏不许写「学习/了解」
- 先判断「有没有现成工具、软件或系统功能能解决」：有的话 goal 写成「用现成工具把这件事做掉」，
  hint 写该用哪个工具、先点哪里；不要默认让人自己写代码造一个
- 只有现成工具确实解决不了，才写「自己做一个小的」
- 如果这句麻烦大到一次做不完，就把 goal 收窄成「第一个能验证的小步」

输出严格 JSON（无围栏）：{"goal":"...","check":"...","hint":"...","firstStep":"...","expect":"做完会看到什么"}`

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
	Goal      string `json:"goal"`
	Check     string `json:"check"`
	Hint      string `json:"hint"`
	FirstStep string `json:"firstStep"`
	Expect    string `json:"expect"`
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
	d.FirstStep = strings.TrimSpace(d.FirstStep)
	d.Expect = strings.TrimSpace(d.Expect)
	if d.FirstStep == "" {
		// 没有第一步的任务卡等于把新手扔在原地：不当作可用草案
		d.FirstStep = "（模型没给出第一步，请自己写：打开什么、敲哪条命令）"
	}
	if d.Check == "" {
		// 模型没给出可观察的验收：不给半成品草案，让人自己写
		d.Check = "（模型没给出可观察的验收标准，请自己写一条：做完能看到什么）"
	}
	return d, nil
}
