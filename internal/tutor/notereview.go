// notereview.go — AI 助教的每日笔记巡检（用户 2026-10-25 指令）。
//
// 对照「教学内容」（在学科目/已收尾知识点）检查学习者笔记：
// 有没有讲错的概念、漏掉的关键点、可以改进的结构——助教主动给出诊断，
// 结果主动可见（方向卡「笔记诊断」行）。这是「助教一点点把项目优化成
// 个人向」的日常机制：笔记越查越贴合这个人的思路。
package tutor

import (
	"fmt"
	"strings"

	"outpost2/internal/llm"
	"outpost2/internal/notes"
)

const noteReviewSystem = `你是前哨的 AI 助教，今天的工作之一：巡检学习者的笔记。
输入是「学习者当前教学内容」与「全部笔记」。逐篇检查并输出诊断报告：

每篇笔记给出（没有问题就明说没问题，不要硬凑）：
- ✅/⚠️ 开头标注总体状态
- 问题：与教学内容矛盾/讲错的概念（最重要——引用原文短句指出，给出正确说法）
- 改进：可以补的关键点、结构建议、和你教学时讲的例子对得上的延伸

最后给一段「总评」：笔记习惯的整体观察 + 1 条最值得做的改进。

纪律：
- 只依据「教学内容」判对错，教学内容没覆盖的笔记内容不下对错结论（可以说"超出当前课程，无法核对"）
- 语气：助教对学习者，直接、具体、不客套；问题引用原文短句
- 全文 ≤400 字；输出纯文本报告（不要 JSON）`

// ReviewNotes 每日笔记巡检：教学内容上下文 + 全部笔记 → 诊断报告。
func (t *Tutor) ReviewNotes(ctx string) (string, error) {
	if t.Client == nil {
		return "", fmt.Errorf("LLM 未配置")
	}
	transcript := notes.Transcript(t.NotesDir, 24000)
	if transcript == "" {
		return "", fmt.Errorf("还没有笔记（notes/ 目录为空——收尾一课后写第一篇）")
	}
	user := "当前教学内容：\n" + ctx + "\n\n全部笔记：\n" + transcript + "\n\n开始巡检，输出诊断报告。"
	raw, err := t.Client.Chat(t.withAgents(noteReviewSystem), []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("巡检输出为空")
	}
	return raw, nil
}
