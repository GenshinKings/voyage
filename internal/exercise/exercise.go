package exercise

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"outpost2/internal/llm"
)

// Exercise 一道实操练习：助教按实时进度出题，学习者作答，助教批改。
// [PASS] 收尾与教学同构；完成即记「用过」级履历。
type Exercise struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Topic    string        `json:"topic"`
	Status   string        `json:"status"` // active/done
	Messages []llm.Message `json:"messages"`
}

// Engine 出题+批改。生产 Client=*llm.Client；测试可注入 mock。
type Engine struct {
	Client llm.ChatClient
}

type rawEx struct {
	Title   string `json:"title"`
	Topic   string `json:"topic"`
	Problem string `json:"problem"`
}

// Generate 按当前在学的知识点出 3 道实操题。difficultyNote 为空串则不加难度约束。
func (e *Engine) Generate(topics []string, difficultyNote string) ([]Exercise, error) {
	if e.Client == nil {
		return nil, fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("还没有可出题的知识点——先开课")
	}
	system := `你是实操练习出题助教。下面给出学习者正在学的知识点，以及每个知识点「课上实际讲过的内容摘要」。
只出能考这些讲过的内容的题——对话里没教到的概念一律不碰。出 3 道实操练习题：动手算、动手写一小段代码或命令、或真实场景决策——不要背诵默写题。
输出 JSON：{"exercises":[{"title":"不超过12个字","topic":"对应知识点","problem":"完整题面：任务+已知条件+要求产出，不要包含答案"}]}`
	raw, err := e.Client.ChatJSON(system, []llm.Message{{Role: "user", Content: "正在学的知识点与课上讲过的内容：\n" + strings.Join(topics, "\n\n") + difficultyNote}})
	if err != nil {
		return nil, err
	}
	list := parseList(raw)
	if len(list) == 0 {
		return nil, fmt.Errorf("出题解析失败：%s", truncate(raw))
	}
	now := time.Now()
	out := make([]Exercise, 0, len(list))
	for i, r := range list {
		if strings.TrimSpace(r.Problem) == "" {
			continue
		}
		out = append(out, Exercise{
			ID:     now.Format("0102-150405") + fmt.Sprintf("-%02d", i+1),
			Title:  firstNonEmpty(r.Title, "实操练习"),
			Topic:  r.Topic,
			Status: "active",
			Messages: []llm.Message{{Role: "assistant", Content: "【实操练习】" + r.Problem + "\n\n（把你的解法/算式/代码直接发给我，我来批改。）"}},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("出题为空：%s", truncate(raw))
	}
	return out, nil
}

// Answer 批改一次作答；完全正确时回复末尾出现 [PASS]，练习转 done。
// 作答文本若是一个存在的本地文件路径（写代码的题），读文件内容作为提交物进批改上下文。
func (e *Engine) Answer(ex *Exercise, text string) (string, bool, error) {
	if e.Client == nil {
		return "", false, fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	content := text
	if p := strings.TrimSpace(text); isSubmissionFile(p) {
		b, err := os.ReadFile(p)
		if err != nil {
			return "", false, fmt.Errorf("读不了提交的文件 %s: %w", p, err)
		}
		if len(b) > 64<<10 {
			return "", false, fmt.Errorf("文件太大（>64KB）：%s", p)
		}
		content = fmt.Sprintf("（以本地文件提交：%s）\n%s", p, string(b))
	}
	ex.Messages = append(ex.Messages, llm.Message{Role: "user", Content: content})
	system := `你是实操练习的批改助教。对照题面评学习者这次作答：先说对在哪、错在哪、漏了什么关键点；如果解法已完整正确，在回复最后单独一行输出 [PASS]；否则只给一步提示，不要直接报答案。`
	reply, err := e.Client.Chat(system, ex.Messages)
	if err != nil {
		return "", false, err
	}
	reply = strings.TrimSpace(reply)
	ex.Messages = append(ex.Messages, llm.Message{Role: "assistant", Content: reply})
	done := strings.Contains(reply, "[PASS")
	if done {
		ex.Status = "done"
	}
	return reply, done, nil
}

// parseList 兼容三种返回形态：裸数组 / {exercises:[]} / {items:[]|questions:[]}。
func parseList(raw string) []rawEx {
	try := func(b []byte) []rawEx {
		var list []rawEx
		if json.Unmarshal(b, &list) == nil && len(list) > 0 {
			return list
		}
		return nil
	}
	if list := try([]byte(raw)); list != nil {
		return list
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &wrap) != nil {
		return nil
	}
	for _, key := range []string{"exercises", "items", "questions"} {
		if r, ok := wrap[key]; ok {
			if list := try(r); list != nil {
				return list
			}
		}
	}
	return nil
}

// isSubmissionFile 作答文本是否指向一个存在的本地文件（单行、≤260 字符、存在且非目录）。
func isSubmissionFile(p string) bool {
	if p == "" || strings.ContainsAny(p, "\n\r") || len(p) > 260 {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func truncate(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
