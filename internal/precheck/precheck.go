// Package precheck —— 序列级难度闸门。
//
// 由来（docs/audit/2026-11-03-偏离审查.md 偏离三）：
// 主人实测「到第二课就被术语砸懵」，而系统里没有任何地方判断
// 「这一课对现在的他会不会太难」。
//
// 这里只做一件事：开课前给出一个判定（放行 / 降一档 / 先补地基）+ 一句理由。
// 刻意不自动改课程、不自动跳过：判定是建议，改课是人的事。
package precheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 三种判定。
const (
	VerdictOK      = "ok"         // 可以上
	VerdictTooHard = "too-hard"   // 降一档：换更小的题，或先补一个前置概念
	VerdictBase    = "needs-base" // 先补地基：他手里没有可调用的直觉
)

// Verdict 一次判定结果。
type Verdict struct {
	Verdict    string   `json:"verdict"`
	Why        string   `json:"why"`
	Suggestion string   `json:"suggestion"`
	Terms      []string `json:"terms,omitempty"` // 这节课会出现的术语（他没见过的）
}

// ChatFunc 抽出来为了可测：真跑时接 LLM，测试时接假函数。
type ChatFunc func(ctx context.Context, system string, user string) (string, error)

// Learner 交给判官的「他现在手里有什么」。
type Learner struct {
	KnownTerms []string `json:"knownTerms"` // 他在之前的课里已经见过并解释过的术语
	WeakPoints []string `json:"weakPoints"` // 卡过的点
	LastChecks []string `json:"lastChecks"` // 上一节的必须掌握清单
	DoneCount  int      `json:"doneCount"`  // 已经完成几节
}

// Lesson 要判的那节课。
type Lesson struct {
	Title    string   `json:"title"`
	Goal     string   `json:"goal"`
	Terms    []string `json:"terms"`    // 这节课会用到的术语
	FirstAct string   `json:"firstAct"` // 第一件事是什么（判断能否直接动手）
}

const system = `你是课程难度审查员。任务：判断「这节课对现在的这个学习者会不会太难」。
学习者是完全新手，之前被术语砸懵过。规则：
1. 只要课里出现他没见过、也没在本课第一件事之前解释过的术语，就判 needs-base（先补地基）。
2. 他卡过的点如果这节课直接依赖，判 too-hard（降一档）。
3. 只有：术语他见过 或 本课第一件事能直接动手且不依赖术语 —— 才判 ok。
只输出 JSON，不要别的话：
{"verdict":"ok|too-hard|needs-base","why":"一句话理由","suggestion":"一句话建议","terms":["他没见过的术语"]}`

// Judge 出判定。
func Judge(ctx context.Context, chat ChatFunc, l Learner, lesson Lesson) (Verdict, error) {
	payload, err := json.Marshal(map[string]any{"learner": l, "lesson": lesson})
	if err != nil {
		return Verdict{}, err
	}
	out, err := chat(ctx, system, string(payload))
	if err != nil {
		return Verdict{}, err
	}
	return Parse(out)
}

// Parse 从模型输出里抠 JSON。抠不出来就判 needs-base——
// 宁可让他多补一步，也不要把人扔进看不懂的课里。
func Parse(raw string) (Verdict, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Verdict{Verdict: VerdictBase, Why: "审查员没给出可解析的判定，按「先补地基」处理"}, nil
	}
	var v Verdict
	if err := json.Unmarshal([]byte(raw[start:end+1]), &v); err != nil {
		return Verdict{Verdict: VerdictBase, Why: "判定格式坏了，按「先补地基」处理"}, nil
	}
	if !valid(v.Verdict) {
		return Verdict{Verdict: VerdictBase, Why: fmt.Sprintf("判定值不认识（%s），按「先补地基」处理", v.Verdict)}, nil
	}
	if strings.TrimSpace(v.Why) == "" {
		v.Why = "（没写理由）"
	}
	return v, nil
}

func valid(s string) bool {
	switch s {
	case VerdictOK, VerdictTooHard, VerdictBase:
		return true
	}
	return false
}
