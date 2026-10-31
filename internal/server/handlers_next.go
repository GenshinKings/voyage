package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"voyage/internal/llm"
	"voyage/internal/run"
)

// 按错误出题（P3 的核心）：读你的卡点分布，出一道针对它的题。
//
// 为什么不能用模板轮换：模板按关卡顺序推进，和「你实际栽在哪」无关。
// 这里把 RetroCount（concept/condition/env/slip/unfinished）当输入，
// 让题目从错误长出来，而不是从章节长出来。
const nextTaskSystem = `你是出题人。学习者刚做完一关，并留下了「卡点类型」和「下次怎么办」。

你的任务：出一道**新题**，专门练他这次栽的那一类，但场景要换（不能是同一件事再写一遍）。

规则：
- goal：一句话，30~60 分钟能做完，用他熟悉的领域（他做电商选品/笔记/求职），不要凭空编场景
- check：可观察的验收标准（跑出结果、截图、别人照着能做），禁止「理解了/掌握了」
- hint：只给第一步或方向，不给答案
- 如果他反复栽在 env（环境）类，题目应该先修环境再干活
- 如果他栽在 concept（概念），题目要逼他把概念用在具体判断上，不是背定义

输出严格 JSON（无围栏）：{"goal":"...","check":"...","hint":"...","why":"为什么针对这个卡点"}`

// registerNextTaskHandlers 装配「按错误出题」与「直接开一张自定义任务卡」。
func (s *Server) registerNextTaskHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/run/next", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		if s.RunState == nil {
			writeErr(w, 400, "还没开始一个项目")
			return
		}
		counts := s.RunState.RetroCount()
		if len(counts) == 0 {
			writeErr(w, 400, "还没有复盘记录——先给至少一关写复盘，题目才有依据")
			return
		}
		if !s.hasModel() {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个，才能按卡点出题")
			return
		}
		draft, err := s.nextTask(r.Context(), counts, s.RunState.DominantKind())
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "draft": draft, "basedOn": counts, "kind": s.RunState.DominantKind()})
	})

	// 直接开一张自定义任务卡（不走收件箱，用于「出题器给的题」）
	mux.HandleFunc("/api/run/start-custom", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			Goal  string `json:"goal"`
			Check string `json:"check"`
			Hint  string `json:"hint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, "请求格式不对")
			return
		}
		if strings.TrimSpace(req.Check) == "" {
			writeErr(w, 400, "任务卡必须有验收标准——没有验收的关卡不许开")
			return
		}
		goal := strings.TrimSpace(req.Goal)
		if goal == "" {
			goal = "把这件小事做掉"
		}
		s.RunState = newSingleStageRun("自定义任务", goal, req.Check, req.Hint)
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "stage": s.RunState.CurrentStage(), "progress": s.RunState.Progress()})
	})
}

// nextTaskDraft 出题结果。
type nextTaskDraft struct {
	Goal  string `json:"goal"`
	Check string `json:"check"`
	Hint  string `json:"hint"`
	Why   string `json:"why"`
}

// nextTask 让模型按卡点分布出一道题。
func (s *Server) nextTask(ctx context.Context, counts map[string]int, dominant string) (nextTaskDraft, error) {
	payload := map[string]any{
		"卡点分布": counts,
		"主导卡点": dominant,
		"说明":   "主导卡点是 concept/condition/env/slip/unfinished 之一",
	}
	b, _ := json.Marshal(payload)
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, nextTaskSystem,
		[]llm.Message{{Role: "user", Content: string(b)}})
	if err != nil {
		return nextTaskDraft{}, err
	}
	var d nextTaskDraft
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nextTaskDraft{}, err
	}
	d.Goal = strings.TrimSpace(d.Goal)
	d.Check = strings.TrimSpace(d.Check)
	d.Hint = strings.TrimSpace(d.Hint)
	if d.Check == "" {
		d.Check = "（模型没给出可观察的验收标准，请自己写一条：做完能看到什么）"
	}
	return d, nil
}

// newSingleStageRun 造一个只有一关的 Run（自定义任务/出题器给的题用）。
func newSingleStageRun(title, goal, check, hint string) *run.Run {
	return &run.Run{
		ProjectID: "custom",
		Title:     title,
		Tier:      run.TierTool,
		Stages:    []run.Stage{{ID: "custom-1", Goal: goal, Check: check, Hint: hint}},
		Results:   map[string]string{},
		Started:   time.Now().Format(time.RFC3339),
	}
}
