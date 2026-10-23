// profile.go — 学习者画像蒸馏：方向讨论（对话事件流）→ 持久事实（memory/profile.md）。
// 触发点：方向提案「同步到学习」成功后异步执行（server 侧 goroutine），失败不影响开课。
package tutor

import (
	"fmt"
	"strings"

	"outpost2/internal/llm"
	"outpost2/internal/profile"
)

// DistillProfile 从方向讨论记录中提取学习者画像，与现有画像合并写回 profile.md。
// 蒸馏是整理任务，不走 withAgents（教学纪律约束与它无关）。
func (t *Tutor) DistillProfile(sess *DirectionSession) error {
	if t.Client == nil {
		return fmt.Errorf("LLM 未配置")
	}
	if sess == nil || len(sess.Messages) == 0 {
		return fmt.Errorf("没有方向讨论记录")
	}
	sys := `你是学习者画像整理员。输入是「已有画像」与「一段学习方向讨论记录」。
任务：从讨论记录中提取关于学习者的持久事实（职业背景 / 已有水平 / 动机 / 每周时间投入 / 目标产出 / 学习偏好），与已有画像合并成一份新画像。
规则：
- 只记持久事实，不记具体课程内容（"要学决策树"这种不记）
- 每条一行，以"- "开头
- 已有画像里仍然成立的事实保留；与讨论记录矛盾或已过时的，以记录为准
- 全文 ≤20 行；只输出画像正文（保留"# 学习者画像"标题），不要代码块、不要解释`
	user := "已有画像：\n" + profile.Load(t.ProfilePath) + "\n\n讨论记录：\n" + transcript(sess.Messages)
	raw, err := t.Client.Chat(sys, []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("画像蒸馏输出为空")
	}
	return profile.Save(t.ProfilePath, raw)
}

// transcript 方向讨论记录 → 纯文本（蒸馏输入）。
func transcript(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "user":
			b.WriteString("学习者：" + m.Content + "\n")
		case "assistant":
			b.WriteString("顾问：" + m.Content + "\n")
		}
	}
	return b.String()
}
