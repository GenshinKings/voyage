package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"voyage/internal/llm"
	"voyage/internal/run"
)

// 「我不知道从哪下手」——新手真正的卡点不是不会做，是不知道第一下动哪。
//
// 设计：不给答案，只把台阶降一级——一条能照抄的命令/一次能点到的操作，
// 加上「做完你会看到什么」。降级结果存在这一关上，下次进来还在。
const scaffoldSystem = `你面前是一个没接触过编程的人。他已经拿到任务目标和验收标准，但说「不知道从哪下手」。

你要做的：把他要做的第一件小事拆到「照抄就能动」的程度。

规则：
- 只给第一步，不要给完整流程，也不要把答案写完
- 必须具体到可执行：打开哪个软件/在哪个目录/敲哪一条命令/点哪个按钮
- 附一句「做完你会看到什么」——他要能自己判断成功没有
- 如果缺前提（没装某软件、没建目录），第一步就是「先把前提装上」，并写清怎么看装好了
- 不要说「学习」「了解」「掌握」，只说动作
- 语气像旁边有人搭了一把手，不要教程腔

输出严格 JSON（无围栏）：{"firstStep":"...","expect":"做完会看到什么","ifStuck":"如果这一步就报错，先看什么"}`

type scaffoldDraft struct {
	FirstStep string `json:"firstStep"`
	Expect    string `json:"expect"`
	IfStuck   string `json:"ifStuck"`
}

// registerHelpHandlers 装配「我不知道从哪下手」。
func (s *Server) registerHelpHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/run/help", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		if s.RunState == nil || s.RunState.Started == "" {
			writeErr(w, 400, "还没有进行中的任务")
			return
		}
		st := s.RunState.CurrentStage()
		if st.ID == "" {
			writeErr(w, 400, "当前没有关卡")
			return
		}
		// 已经有更小台阶就直接给，不重复花 token
		if s.RunState.Scaffold != nil && s.RunState.Scaffold[st.ID] != "" {
			writeJSON(w, map[string]any{"ok": true, "text": s.RunState.Scaffold[st.ID], "cached": true})
			return
		}
		if !s.hasModel() {
			// 没模型也别让人卡死：给出静态降级
			text := "先把目标抄到一张纸上，然后回答三个问题：①这件事要在哪个软件/网页里做？②做完能看到什么？③现在手上缺哪个东西（软件、账号、文件）。第三个问题的答案就是你的第一步。"
			s.RunState.NoteScaffold(st.ID, text)
			s.saveState()
			writeJSON(w, map[string]any{"ok": true, "text": text})
			return
		}
		d, err := s.scaffold(r.Context(), st)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		text := strings.TrimSpace(d.FirstStep)
		if d.Expect != "" {
			text += "\n做完你会看到：" + strings.TrimSpace(d.Expect)
		}
		if d.IfStuck != "" {
			text += "\n如果这一步就报错：" + strings.TrimSpace(d.IfStuck)
		}
		s.RunState.NoteScaffold(st.ID, text)
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "text": text})
	})
}

// scaffold 让模型把台阶降一级。
func (s *Server) scaffold(ctx context.Context, st *run.Stage) (scaffoldDraft, error) {
	payload := map[string]any{
		"目标":   st.Goal,
		"验收标准": st.Check,
		"原提示":  st.Hint,
	}
	b, _ := json.Marshal(payload)
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, scaffoldSystem,
		[]llm.Message{{Role: "user", Content: string(b)}})
	if err != nil {
		return scaffoldDraft{}, err
	}
	var d scaffoldDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return scaffoldDraft{}, err
	}
	if strings.TrimSpace(d.FirstStep) == "" {
		d.FirstStep = "先把它拆成「在哪个软件里做」和「做完看到什么」两句话，写下来。"
	}
	return d, nil
}
