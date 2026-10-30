package exercise

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"voyage/internal/llm"
	"voyage/internal/profile"

	"voyage/internal/usererr"
)

// Exercise 一道实操练习：助教按实时进度出题，学习者作答，助教批改。
// [PASS] 收尾与教学同构；完成即记「用过」级履历。
// 语境式练习（i+1）：题面主线考察核心知识点，解题路径自然穿过语境知识点——
// 旧概念在新语境中被调用（Krashen 可理解性输入 / 螺旋式课程），不做背诵式复习。
type Exercise struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Topic    string        `json:"topic"`
	Context  string        `json:"context,omitempty"`  // 语境知识点（解题必经的旧概念）
	Subject  string        `json:"subject,omitempty"`  // 归属科目（难度策略用）
	LessonID string        `json:"lessonId,omitempty"` // 归属知识点（bandit 奖励结算的臂）
	Review   bool          `json:"review,omitempty"`   // 错题回归题（核心知识点曾做错，换场景重出）
	Status   string        `json:"status"`             // active/done
	Messages []llm.Message `json:"messages"`
}

// Engine 出题+批改。生产 Client=*llm.Client；测试可注入 mock。
// AgentsPath=用户约束文件（AGENTS.md），每次调用热读，优先级高于内置纪律。
// ProfilePath=学习者画像（热读注入——出题贴合背景与目标场景）。
type Engine struct {
	Client      llm.ChatClient
	AgentsPath  string
	ProfilePath string
	// 第六轮-P1-2 取材注入：学习者真实仓库摘要（文件名+函数名+片段），server 每次生成前最新写
	GenerationMaterial string
}

func (e *Engine) withAgents(system string) string {
	var head string
	if e.AgentsPath != "" {
		if b, err := os.ReadFile(e.AgentsPath); err == nil {
			if a := strings.TrimSpace(string(b)); a != "" {
				head += "【出题与批改行为约束（用户自定义，优先级最高，与下列默认纪律冲突时以此为准）】\n" + a + "\n\n"
			}
		}
	}
	if p := profile.Block(e.ProfilePath); p != "" {
		head += p + "\n\n"
	}
	return head + system
}

type rawEx struct {
	Title   string `json:"title"`
	Topic   string `json:"topic"`
	Context string `json:"context"`
	Problem string `json:"problem"`
}

// Generate 按当前在学的知识点出 3 道语境式实操题。difficultyNote 为空串则不加难度约束。
// 第六轮-P1-2：GenerationMaterial（学习者的真实仓库摘要）非空时优先取材于它——
// 题面引用真实文件名与函数名；材料不足时由 LLM 退回通用例子，不硬凑。
func (e *Engine) Generate(ctx context.Context, topics []string, difficultyNote string) ([]Exercise, error) {
	if e.Client == nil {
		return nil, usererr.New("今天的出题还没接上模型，去设置里贴一个就能用",
			"[exercise] 出题失败：模型未配置（outpost2-data/llm.json）")
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("今天还没有能出题的知识点，先上完一节再来")
	}
	system := `你是实操练习出题助教。给出的每道题都有「核心知识点」（题面主线考察它）与「语境知识点」（解题路径必须自然经过它）。
语境式出题原则：像一篇 90% 词汇都认识的英语文章——旧概念在新语境中被自然调用就能巩固，不要在题面里重新讲解语境概念。
只出课上讲过的内容——对话里没教到的概念一律不碰。出实操题：动手算、动手写一小段代码或命令、或真实场景决策——不要背诵默写题。
【真实仓库取材（若下方提供学习者代码摘要）】：优先其中 1-2 道题直接取材他的真实代码——形式如「main.go 里的 thisFunc 在什么输入下会返回错的值？」「这行能不能删？为什么？」，题面必须出现真实文件名与真实函数名；取材要与核心知识点真正相关，不相关就不硬凑，退回通用例子。
输出 JSON：{"exercises":[{"title":"不超过12个字","topic":"核心知识点","context":"用到的语境知识点","problem":"完整题面：任务+已知条件+要求产出，不要包含答案"}]}`
	payload := "正在学的知识点与课上讲过的内容：\n" + strings.Join(topics, "\n\n") + difficultyNote
	if m := strings.TrimSpace(e.GenerationMaterial); m != "" {
		payload += "\n\n【学习者的真实仓库摘要（出题优先取材）】\n" + m
		e.GenerationMaterial = "" // 用后即清——下一轮材料要重取（代码可能变了）
	}
	raw, err := llm.ChatJSONWith(ctx, e.Client, e.withAgents(system), []llm.Message{{Role: "user", Content: payload}})
	if err != nil {
		return nil, err
	}
	list := parseList(raw)
	if len(list) == 0 {
		return nil, usererr.New("出题没解析出来，再点一次「去做题」；老失败就反映给作者",
			"出题解析失败："+truncate(raw))
	}
	now := time.Now()
	out := make([]Exercise, 0, len(list))
	for i, r := range list {
		if strings.TrimSpace(r.Problem) == "" {
			continue
		}
		opening := "【实操练习】" + r.Problem + "\n\n（把你的解法/算式/代码直接发给我，我来批改。）"
		if strings.TrimSpace(r.Context) != "" {
			opening = "【实操练习 · 语境式】主线考察「" + r.Topic + "」，解题会自然用到「" + r.Context + "」。\n\n" + r.Problem + "\n\n（把你的解法/算式/代码直接发给我，我来批改。）"
		}
		out = append(out, Exercise{
			ID:       now.Format("0102-150405") + fmt.Sprintf("-%02d", i+1),
			Title:    firstNonEmpty(r.Title, "实操练习"),
			Topic:    r.Topic,
			Context:  strings.TrimSpace(r.Context),
			Status:   "active",
			Messages: []llm.Message{{Role: "assistant", Content: opening}},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("出题为空：%s", truncate(raw))
	}
	return out, nil
}

// Answer 批改一次作答；完全正确时回复末尾出现 [PASS]，练习转 done。
// 作答文本若是一个存在的本地文件路径（写代码的题），读文件内容作为提交物进批改上下文。
func (e *Engine) Answer(ctx context.Context, ex *Exercise, text string) (string, bool, error) {
	if e.Client == nil {
		return "", false, usererr.New("评卷要用模型，先去设置里接上",
			"[exercise] 评卷失败：模型未配置（outpost2-data/llm.json）")
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
	reply, err := llm.ChatWith(ctx, e.Client, e.withAgents(system), ex.Messages)
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
