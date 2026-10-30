package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"voyage/internal/llm"
	"voyage/internal/run"
)

// 关卡判定提示词：只判「这一步做到没有」，不替他做，也不给答案。
const stageJudgeSystem = `你是开源项目闯关的验收员。学习者刚交了某一关的产物，你只回答一件事：这一步达到验收标准没有。

口径：
- 只看有没有真的做到验收里写的那件事，不看文风、不看长度
- 说「我做了」但没有证据（命令输出/文件内容/链接/截图说明）→ 不算做到
- 做对了但顺带做了别的 → 算做到
- 做不到就指出差在哪里，并给一句「下一步做的那件小事」，不要直接给答案

输出严格 JSON（无围栏）：{"pass":true,"reason":"一句话依据","action":"下一步做的一件小事（通过时为空）"}`

// judgeStage 用模型判定一关（只在该关 Expect 为空时调用）。
// 没有可用模型时返回 ok=false, 可用=false —— 调用方退回「非空即过」。
func (s *Server) judgeStage(ctx context.Context, st run.Stage, output string) (pass bool, reason, action string, usable bool) {
	if s.Tutor == nil || s.Tutor.Client == nil {
		return false, "", "", false
	}
	user := fmt.Sprintf("关卡：%s\n目标：%s\n验收标准：%s\n\n学习者交上来的东西：\n%s",
		st.ID, st.Goal, st.Check, strings.TrimSpace(output))

	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, stageJudgeSystem,
		[]llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return false, "", "", false
	}
	var out struct {
		Pass   bool   `json:"pass"`
		Reason string `json:"reason"`
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil {
		return false, "", "", false
	}
	return out.Pass, out.Reason, out.Action, true
}
