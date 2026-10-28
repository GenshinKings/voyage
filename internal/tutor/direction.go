package tutor

// direction.go — 学习方向咨询：开课前先和 AI 助教讨论学习方向。
// 对话收敛后助教输出 [PROPOSE] 课程提案；用户确认「同步到学习」才真正建课。
// 目的：防止一句话开课的轻率，也防止方向跑偏——方向是讨论出来的，不是命令出来的。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"voyage/internal/llm"

	"voyage/internal/usererr"
)

// DirectionSession 方向讨论会话（独立于课程教学会话，持久化 direction.json）。
type DirectionSession struct {
	Messages []llm.Message `json:"messages"`
	Proposal *Proposal     `json:"proposal,omitempty"` // 助教最近一次课程提案（同步前一直保留）
	Updated  time.Time     `json:"updatedAt"`
}

// Proposal 助教给出的课程方案（同步后成为课程表）。
type Proposal struct {
	Subject string   `json:"subject"`
	Lessons []Lesson `json:"lessons"`
}

const directionSystem = `你是 Voyage 的学习方向顾问。用户想开启一段新的学习，但方向往往一开始是模糊的（「想学 AI」「对摄影感兴趣」）。

你的任务：通过 2-4 轮对话，帮用户把模糊的兴趣收敛成清晰、可执行的学习方向。
追问维度（挑当下最有价值的问，一次只问 1-2 个，别审问）：为什么学（动机/用途）、现在什么水平（起点）、每周能投入多少时间、想达到什么程度（能做出什么作品/能力）。

对话原则：
- 每次回复 ≤200 字；先回应用户说的内容，再追问。
- 用户说得笼统就帮他具体化；他说得很具体就别重复问。
- 不要一上来就给课程表；也不要无限追问。

当你判断方向已经足够清晰时：先用 2-3 句话确认这个方向（说清「为什么是这几课」），然后输出课程提案，格式严格如下：
[PROPOSE]
{"subject":"科目名（≤10字）","lessons":[{"title":"≤16字","brief":"学完能做什么"}]}

提案要求：4-8 课，从基础到进阶，每课都是 30 分钟内「讲解+检查题」能收尾的最小单元。
用户之后可能要求调整（加一课/去掉某课/换个侧重）——按反馈修改后重新输出 [PROPOSE] 块。用户说「同步/就这个/可以了」之类的话时，不要重复输出提案，简单确认即可（同步由系统按钮完成）。
用户明确转向新方向、或说明之前的讨论只是测试/不算数时：旧提案作废——在回复中单独一行输出 [PROPOSE-VOID]（系统会收起旧提案卡），然后正常回应新方向，绝不要把旧提案再挂出来。`

// LoadDirection 读取方向讨论会话（不存在= nil）。
func (t *Tutor) LoadDirection() *DirectionSession {
	b, err := os.ReadFile(filepath.Join(t.Dir, "direction.json"))
	if err != nil {
		return nil
	}
	var s DirectionSession
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// ResetDirection 清空方向讨论（开新课/用户要求重聊时）。
func (t *Tutor) ResetDirection() {
	_ = os.Remove(filepath.Join(t.Dir, "direction.json"))
}

// DirectionSend 方向讨论一轮：追加用户输入 → 顾问回应 → 解析 [PROPOSE] 提案。
func (t *Tutor) DirectionSend(userText string) (reply string, proposal *Proposal, err error) {
	if t.Client == nil {
		return "", nil, usererr.New("模型还没接上——去设置页配置一个就能用", "LLM 未配置（outpost2-data/llm.json）")
	}
	s := t.LoadDirection()
	if s == nil {
		s = &DirectionSession{}
	}
	userText = strings.TrimSpace(userText)
	if userText == "" {
		return "", s.Proposal, fmt.Errorf("内容为空")
	}
	// 用户确认类消息且已有提案 → 不再调 LLM（同步由按钮完成，避免重复提案）
	if s.Proposal != nil && isAffirmation(userText) {
		reply := "好——点上方「同步到学习」，这门课就正式开始。想调整的话直接说。"
		s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText}, llm.Message{Role: "assistant", Content: reply})
		s.Updated = time.Now()
		t.saveDirection(s)
		return reply, s.Proposal, nil
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	reply, err = t.Client.Chat(t.withAgents(directionSystem), s.Messages)
	if err != nil {
		return "", s.Proposal, err
	}
	reply = strings.TrimSpace(reply)
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	if p := parseProposal(reply); p != nil {
		s.Proposal = p
	} else if strings.Contains(reply, "[PROPOSE-VOID]") {
		// 用户转向新方向/声明之前是测试——旧提案作废（收起卡片）
		s.Proposal = nil
	}
	s.Updated = time.Now()
	t.saveDirection(s)
	return reply, s.Proposal, nil
}

// isAffirmation 用户在确认（已有提案时不再触发新一轮提案）。
func isAffirmation(text string) bool {
	for _, w := range []string{"同步", "就这个", "可以了", "没问题", "开始吧", "确认"} {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// parseProposal 从回复中提取 [PROPOSE] 后的 JSON 提案。
func parseProposal(reply string) *Proposal {
	i := strings.Index(reply, "[PROPOSE]")
	if i < 0 {
		return nil
	}
	rest := strings.TrimSpace(reply[i+len("[PROPOSE]"):])
	// 提案 JSON 可能到回复结尾，也可能后面还有文字——找平衡大括号
	start := strings.Index(rest, "{")
	if start < 0 {
		return nil
	}
	depth := 0
	end := -1
	inStr := false
	for j := start; j < len(rest); j++ {
		c := rest[j]
		if inStr {
			if c == '\\' {
				j++
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		} else if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				end = j + 1
				break
			}
		}
	}
	if end < 0 {
		return nil
	}
	var pr struct {
		Subject string `json:"subject"`
		Lessons []struct {
			Title string `json:"title"`
			Brief string `json:"brief"`
		} `json:"lessons"`
	}
	if err := json.Unmarshal([]byte(rest[start:end]), &pr); err != nil || len(pr.Lessons) == 0 {
		return nil
	}
	less := make([]Lesson, 0, len(pr.Lessons))
	for k, l := range pr.Lessons {
		if strings.TrimSpace(l.Title) == "" {
			continue
		}
		less = append(less, Lesson{Title: strings.TrimSpace(l.Title), Brief: strings.TrimSpace(l.Brief), Order: k + 1})
	}
	if len(less) == 0 {
		return nil
	}
	return &Proposal{Subject: strings.TrimSpace(pr.Subject), Lessons: less}
}

func (t *Tutor) saveDirection(s *DirectionSession) {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.MkdirAll(t.Dir, 0o755)
	_ = os.WriteFile(filepath.Join(t.Dir, "direction.json"), b, 0o644)
}

// SetSubjectWithLessons 把已确认的提案落地为课程表（替换式单科目；不调 LLM——方案已在对话中敲定）。
func (t *Tutor) SetSubjectWithLessons(subject string, lessons []Lesson) *Curriculum {
	cur := t.Curriculum()
	base := time.Now().Format("0102") + "-" + sanitize(subject)
	newLessons := make([]Lesson, 0, len(lessons))
	for i, l := range lessons {
		title := strings.TrimSpace(l.Title)
		if title == "" {
			continue
		}
		newLessons = append(newLessons, Lesson{
			ID:      fmt.Sprintf("%s-%02d", base, i+1),
			Subject: subject,
			Title:   title,
			Brief:   strings.TrimSpace(l.Brief),
			Status:  "active",
			Order:   i + 1,
		})
	}
	cur.Subjects = []string{subject}
	cur.Lessons = newLessons
	cur.UpdatedAt = time.Now()
	t.saveCurriculum(cur)
	return cur
}
