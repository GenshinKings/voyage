// verdict.go — 第八轮-P0-1：收尾判定双通道。
//
// 通道一（marker）：解析模型回复里的收尾标记。容错：全角【】/［］、全角冒号、
// 括号内空白、行内出现；出现在代码围栏内=误写，记 warning 不判过。
// 通道二（structured）：标记缺失时，把学生回答+验收标准丢给一次独立小调用，
// 输出 {pass, reason, cause}，同样落库——闭环不再依赖模型「记得吐标记」。
// 每次判定事件写一条 diagnostics（通道/耗时/是否二次校验）进会话。
package tutor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"voyage/internal/llm"
)

// Diag 一次判定事件的诊断记录（Session.Diagnostics，逐条追加）。
type Diag struct {
	At         time.Time `json:"at"`
	Channel    string    `json:"channel"`           // marker | structured
	Verdict    string    `json:"verdict"`           // pass | partial | fail | fu-pass | fu-fuzzy | error
	DurationMs int64     `json:"durationMs"`        // 结构化通道的调用耗时
	Retried    bool      `json:"retried,omitempty"` // 是否二次校验（JSON 首解析失败重试一次）
	Note       string    `json:"note,omitempty"`    // warning（如「标记出现在代码块里」）
}

var (
	fenceRe   = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	partialRe = regexp.MustCompile(`(?i)[[【［]\s*PASS\s*[:：]\s*partial\s*[]】］]`)
	passRe    = regexp.MustCompile(`(?i)[[【［]\s*PASS\s*[]】］]`)
)

// ParsePassMark 容错解析收尾标记。只认代码围栏之外的标记；
// 围栏内有疑似标记而围栏外没有 → 视为误写，记 warning 不判过。
// 返回 kind（"" | pass | partial）与 warning。
func ParsePassMark(reply string) (kind, warning string) {
	outside := fenceRe.ReplaceAllString(reply, "")
	inside := strings.Join(fenceRe.FindAllString(reply, -1), "\n")
	switch {
	case partialRe.MatchString(outside):
		return "partial", ""
	case passRe.MatchString(outside):
		return "pass", ""
	case partialRe.MatchString(inside) || passRe.MatchString(inside):
		return "", "收尾标记出现在代码块里——视为误写，不判过"
	}
	return "", ""
}

const judgeSystem = `你是收尾判定器。学生刚回答了助教的检查题，助教没有输出收尾标记——由你独立判定这次回答是否达到「可收尾」水平。

判定口径（与助教纪律一致）：
- 能做出来 / 答对关键点 → pass=true
- 没做完要求的步骤、概念性错误、答非所问、只复述没验证 → pass=false（宁可再教一轮，不放松放水）
- 部分对但有明显洞 → pass=false，cause 标「concept」

cause 只有六种：concept（概念没懂）、condition（看漏条件）、env（环境/报错）、slip（拼写/命令打错）、unfinished（没做完步骤）、none（通过时用）。
输出严格 JSON（无围栏）：{"pass":true,"reason":"一句话依据","cause":"none","where":"卡在哪个具体点（没卡就给空串）","action":"让他下一步做的一件小事，一句话，不要给答案"}`

// judgeStructured 结构化判定通道：独立小调用，不依赖教学会话的标记习惯。
// retried=true 表示第一次 JSON 解析失败、重试了一次（二次校验）。
func (t *Tutor) judgeStructured(ctx context.Context, lesson Lesson, s *Session) (r JudgeResult, err error) {
	if t.Client == nil {
		return JudgeResult{}, fmt.Errorf("LLM 未配置")
	}
	q, a := "", s.Messages[len(s.Messages)-1].Content
	for i := len(s.Messages) - 2; i >= 0; i-- {
		if s.Messages[i].Role == "assistant" {
			q = s.Messages[i].Content
			break
		}
	}
	user := fmt.Sprintf("知识点：《%s》——%s\n\n检查题（助教最后一条）：\n%s\n\n学生回答：\n%s\n\n判定。",
		lesson.Title, lesson.Brief, truncRunes(q, 400), truncRunes(a, 400))
	for attempt := 0; attempt < 2; attempt++ {
		retried := attempt > 0
		raw, jerr := llm.ChatJSONWith(ctx, t.Client, t.withAgents(judgeSystem), []llm.Message{{Role: "user", Content: user}})
		if jerr != nil {
			return JudgeResult{Retried: retried}, jerr
		}
		var out struct {
			Pass   bool   `json:"pass"`
			Reason string `json:"reason"`
			Cause  string `json:"cause"`
			Where  string `json:"where"`
			Action string `json:"action"`
		}
		if uerr := json.Unmarshal([]byte(raw), &out); uerr != nil {
			continue // JSON 漂移：重试一次
		}
		if out.Cause == "" {
			out.Cause = "none"
		}
		return JudgeResult{Pass: out.Pass, Reason: out.Reason, Cause: out.Cause, Where: out.Where, Action: out.Action, Retried: retried}, nil
	}
	return JudgeResult{}, fmt.Errorf("判定 JSON 两次解析失败")
}

// judgeWarranted 是否值得走结构化通道：助教刚问过问题（检查题在等答案）、
// 学生认真作答（非卡住/跳过/加深的显式前缀）。闲聊不烧判定调用。
func judgeWarranted(s *Session, userText string) bool {
	trim := strings.TrimSpace(userText)
	for _, p := range []string{"(我卡住了)", "(这个我会)", "(太浅了)"} {
		if strings.HasPrefix(trim, p) {
			return false
		}
	}
	if len(s.Messages) < 2 {
		return false
	}
	for i := len(s.Messages) - 2; i >= 0; i-- {
		if s.Messages[i].Role == "assistant" {
			return strings.Contains(s.Messages[i].Content, "？") || strings.Contains(s.Messages[i].Content, "?")
		}
	}
	return false
}

// addDiag 追加一条判定诊断（封顶 50 条——只留近期）。
func (s *Session) addDiag(d Diag) {
	s.Diagnostics = append(s.Diagnostics, d)
	if len(s.Diagnostics) > 50 {
		s.Diagnostics = s.Diagnostics[len(s.Diagnostics)-50:]
	}
}

// truncRunes 按字符截断（判定提示词的体量控制）。
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// JudgeResult 结构化判定的结果：不只过/不过，还要说清「错在哪类、卡在哪、下一步做什么」。
// 这是 docs/teach/spec.md 里「懂教」的最小要求——只说「再想想」等于没反馈。
type JudgeResult struct {
	Pass    bool   `json:"pass"`
	Reason  string `json:"reason"`           // 一句话依据
	Cause   string `json:"cause"`            // concept | condition | env | slip | unfinished | none
	Where   string `json:"where,omitempty"`  // 卡在哪个具体点（没卡给空串）
	Action  string `json:"action,omitempty"` // 下一步做的一件小事（不给答案）
	Retried bool   `json:"retried,omitempty"`
}
