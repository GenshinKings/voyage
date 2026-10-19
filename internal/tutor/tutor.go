// Package tutor — 对话式教学会话（前哨 2.0 学习页的核心）。
//
// 流程：用户给科目 → LLM 分解成有序知识点 → 每天丢第一个未完成的 → 用户点开
// 进入对话 → 助教按「讲解+检查题→判→追问/点破」的护栏形态教 → [PASS] 判定
// → 履历记 Known（后续实践升 Used/Done）。
//
// 会话持久化：tutor/<nodeID>.json——跨天连续（DeepSeek 式左栏会话的底层）。
package tutor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"outpost2/internal/llm"
)

// Lesson 课程里的一个知识点。
type Lesson struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Title   string `json:"title"`
	Brief   string `json:"brief"`
	Status  string `json:"status"` // active | done
	Order   int    `json:"order"`
}

// Curriculum 科目 → 知识点序列（顺序制 v1）。
type Curriculum struct {
	Subjects []string  `json:"subjects"`
	Lessons  []Lesson  `json:"lessons"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Session 单个知识点的教学对话。
type Session struct {
	NodeID    string      `json:"nodeId"`
	Title     string      `json:"title"`
	Messages  []llm.Message `json:"messages"`
	Verdict   string      `json:"verdict,omitempty"` // pass | partial | ""
	UpdatedAt time.Time   `json:"updatedAt"`
}

// Tutor 教学引擎。
type Tutor struct {
	Dir    string         // 数据目录（tutor/ 子目录存会话）
	Client llm.ChatClient // 生产=*llm.Client；测试可注入 mock
}

const teachSystem = `你是前哨 2.0 的 AI 助教，正在一对一把一个知识点教给学习者。

教学流程（严格按阶段走）：
1. 开场（你的第一条消息）：用大白话+一个生活化类比讲清这个概念（≤300 字），讲完立刻抛出一道检查题——等学习者回答，绝不要自己把答案说出来。
2. 学习者回答后：判定对错。答对 → 先肯定他答对的关键点，再给一个加深的小追问或真实应用场景，然后单独一行输出标记 [PASS]；答错或不完整 → 明确指出误区（给一个能自己验证的反例），再给一条新提示让他重试，不要给终答。
3. 同一道检查题学习者连续两次没答对 → 给出完整透彻的讲解，然后单独一行输出 [PASS:partial]（记录为半懂，不丢人）。

纪律：
- 只教当前知识点；学习者岔开话题，温和拉回「我们先把《当前知识点》收尾」。
- 每条回复 ≤250 字；永远以让学习者动一次脑/动一次手收尾。
- [PASS] 与 [PASS:partial] 必须单独成行，前后不要加别的字。`

// ---------- 课程 ----------

// StartSubject 用户给科目 → LLM 分解成 4-8 个有序知识点。已存在同名科目则直接返回（幂等）。
func (t *Tutor) StartSubject(name string) (*Curriculum, error) {
	cur := t.Curriculum()
	for _, s := range cur.Subjects {
		if s == name {
			return cur, nil
		}
	}
	if t.Client == nil {
		return nil, fmt.Errorf("LLM 未配置")
	}
	raw, err := t.Client.ChatJSON(
		`你是课程设计师。学习者想学一个科目，请把它分解成 4-8 个有序知识点（从基础到进阶）。
每个知识点：title（≤16字，名词短语）、brief（一句话：学完能做什么）。
分解原则：每个知识点都是 30 分钟内可以「讲解+一道检查题」收尾的最小单元；先概念后应用。
JSON 顶层键必须是 "nodes"。`,
		[]llm.Message{{Role: "user", Content: "科目：" + name}})
	if err != nil {
		return nil, err
	}
	lessons, perr := parseCourse(raw)
	if perr != nil {
		return nil, fmt.Errorf("课程分解解析失败: %w", perr)
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("课程分解为空")
	}
	cur.Subjects = append(cur.Subjects, name)
	base := time.Now().Format("0102") + "-" + sanitize(name)
	for i, n := range lessons {
		cur.Lessons = append(cur.Lessons, Lesson{
			ID:      fmt.Sprintf("%s-%02d", base, i+1),
			Subject: name,
			Title:   n.Title,
			Brief:   n.Brief,
			Status:  "active",
			Order:   len(cur.Lessons),
		})
	}
	cur.UpdatedAt = time.Now()
	t.saveCurriculum(cur)
	return cur, nil
}

// Curriculum 读课程（无文件返回空）。
func (t *Tutor) Curriculum() *Curriculum {
	cur := &Curriculum{UpdatedAt: time.Now()}
	b, err := os.ReadFile(filepath.Join(t.Dir, "lessons.json"))
	if err == nil {
		_ = json.Unmarshal(b, cur)
	}
	return cur
}

func (t *Tutor) saveCurriculum(cur *Curriculum) {
	_ = os.MkdirAll(t.Dir, 0o755)
	b, _ := json.MarshalIndent(cur, "", "  ")
	_ = os.WriteFile(filepath.Join(t.Dir, "lessons.json"), b, 0o644)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 {
		return "subj"
	}
	return b.String()
}

// TodayLesson 今日该学的：第一个 active 的知识点（顺序课程）。
func (t *Tutor) TodayLesson() *Lesson {
	cur := t.Curriculum()
	for i := range cur.Lessons {
		if cur.Lessons[i].Status == "active" {
			return &cur.Lessons[i]
		}
	}
	return nil
}

// ---------- 会话 ----------

func (t *Tutor) sessionPath(nodeID string) string {
	return filepath.Join(t.Dir, "tutor", nodeID+".json")
}

// Load 读会话（无则 nil）。
func (t *Tutor) Load(nodeID string) *Session {
	b, err := os.ReadFile(t.sessionPath(nodeID))
	if err != nil {
		return nil
	}
	var s Session
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

func (t *Tutor) save(s *Session) {
	_ = os.MkdirAll(filepath.Join(t.Dir, "tutor"), 0o755)
	s.UpdatedAt = time.Now()
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(t.sessionPath(s.NodeID), b, 0o644)
}

// StartOrResume 开场或续聊：无会话 → 助教开场（讲解+检查题）；有会话 → 原样返回。
func (t *Tutor) StartOrResume(lesson Lesson) (*Session, bool, error) {
	if s := t.Load(lesson.ID); s != nil {
		return s, false, nil
	}
	if t.Client == nil {
		return nil, false, fmt.Errorf("LLM 未配置")
	}
	s := &Session{NodeID: lesson.ID, Title: lesson.Title}
	sys := teachSystem
	user := fmt.Sprintf("知识点：《%s》——%s\n科目：%s\n现在开始教学：输出你的开场（讲解+检查题）。",
		lesson.Title, lesson.Brief, lesson.Subject)
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: user})
	reply, err := t.Client.Chat(sys, s.Messages)
	if err != nil {
		return nil, false, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	t.save(s)
	return s, true, nil
}

// Verdict 判定结果。
type Verdict struct {
	Kind    string `json:"kind"` // ""=继续对话 | pass | partial
	Reply   string `json:"reply"`
}

// Send 学习者发言 → 助教回复；解析 [PASS] 判定并落履历标记。
// 返回：回复文本、判定（pass/partial 时调用方记履历）、错误。
func (t *Tutor) Send(lesson Lesson, userText string) (Verdict, error) {
	s := t.Load(lesson.ID)
	if s == nil {
		return Verdict{}, fmt.Errorf("会话不存在（先 open）")
	}
	if s.Verdict != "" {
		return Verdict{Kind: s.Verdict, Reply: "这一节已经收尾了。去学习页领下一个知识点吧。"}, nil
	}
	if t.Client == nil {
		return Verdict{}, fmt.Errorf("LLM 未配置")
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	reply, err := t.Client.Chat(teachSystem, s.Messages)
	if err != nil {
		return Verdict{}, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	kind := ""
	if strings.Contains(reply, "[PASS:partial]") {
		kind = "partial"
	} else if strings.Contains(reply, "[PASS]") {
		kind = "pass"
	}
	if kind != "" {
		s.Verdict = kind
	}
	t.save(s)
	return Verdict{Kind: kind, Reply: reply}, nil
}

// MarkLessonDone 课程状态推进（履历由调用方写）。
func (t *Tutor) MarkLessonDone(nodeID string) {
	cur := t.Curriculum()
	for i := range cur.Lessons {
		if cur.Lessons[i].ID == nodeID {
			cur.Lessons[i].Status = "done"
		}
	}
	cur.UpdatedAt = time.Now()
	t.saveCurriculum(cur)
}

// parseCourse 双 key 容错解析课程 JSON（LLM 输出健壮性：nodes / knowledge_points / lessons 都认）。
func parseCourse(raw string) ([]Lesson, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, err
	}
	for _, key := range []string{"nodes", "knowledge_points", "lessons"} {
		b, ok := probe[key]
		if !ok {
			continue
		}
		var items []struct {
			Title string `json:"title"`
			Brief string `json:"brief"`
		}
		if err := json.Unmarshal(b, &items); err != nil {
			return nil, err
		}
		out := []Lesson{}
		for _, it := range items {
			if strings.TrimSpace(it.Title) == "" {
				continue
			}
			out = append(out, Lesson{Title: it.Title, Brief: it.Brief})
		}
		return out, nil
	}
	return nil, fmt.Errorf("无已知键（nodes/knowledge_points/lessons）")
}
