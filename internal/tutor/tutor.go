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
	"outpost2/internal/profile"
	"outpost2/internal/reflib"
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
	Subjects  []string  `json:"subjects"`
	Lessons   []Lesson  `json:"lessons"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Session 单个知识点的教学对话。
type Session struct {
	NodeID      string        `json:"nodeId"`
	Title       string        `json:"title"`
	Messages    []llm.Message `json:"messages"`
	Verdict     string        `json:"verdict,omitempty"` // pass | partial | ""
	OpeningMeta *OpeningMeta  `json:"openingMeta,omitempty"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// OpeningMeta 开场元信息：开场指令消息是给 LLM 的（前端不渲染为气泡），
// 这份结构给前端画「元信息条」（节序号/标题/承接）——学习页重设计 v3。
type OpeningMeta struct {
	Index int    `json:"index"` // 节序号（1-based）
	Title string `json:"title"`
	Why   string `json:"why"` // 学完能做什么（承接）
}

// Tutor 教学引擎。
type Tutor struct {
	Dir         string         // 数据目录（tutor/ 子目录存会话）
	Client      llm.ChatClient // 生产=*llm.Client；测试可注入 mock
	AgentsPath  string         // AGENTS.md 约束文件路径（每次调用热读，用户可编辑，改完即生效）
	ProfilePath string         // 学习者画像 memory/profile.md（热读注入——背景/时间/目标跨会话连续）
	RefDir      string         // 教学参考库 kb/（教务抓取的原文资料，热读注入清单——讲解优先依据原文）
	Tools       []Tool         // 助教可用工具（server 组装：GitHub 抓取/入库/阅读；nil=无工具）
	FetchNote   string         // 今日进货提示（server 注入；开场带给助教一次——收尾时主动推荐延伸材料，用后清空防重复）
	NotesDir    string         // 学习者笔记目录 notes/（每日巡检的检查对象，只读）
	TimeBudget  string         // 小穗三轮问题1：今天的时间预算 10/30/90（server 从 state 同步）——决定讲解长度与检查题数量
	// Clock 小穗四轮·架构 P1-2：注入时间源（测试用假钟测 fsrs/判定链路不睡）——nil=走 walltime
	Clock func() time.Time
}

// now 统一取时间：Clock nil 则走真实时间。
func (t *Tutor) now() time.Time {
	if t.Clock != nil {
		return t.Clock()
	}
	return time.Now()
}

// agents 读取用户自定义约束（AGENTS.md）。缺失/失败返回空（默认纪律兜底）。
func (t *Tutor) agents() string {
	if t.AgentsPath == "" {
		return ""
	}
	b, err := os.ReadFile(t.AgentsPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// withAgents 把用户约束、教学参考库、学习者画像前置到 system prompt——
// 冲突时 AGENTS.md 优先；参考库管「教什么才不脱轨」；画像管「怎么讲贴合这个人」。
func (t *Tutor) withAgents(system string) string {
	var head string
	if a := t.agents(); a != "" {
		head += "【教师行为约束（用户自定义，优先级最高，与下列默认纪律冲突时以此为准）】\n" + a + "\n\n"
	}
	if k := reflib.Digest(t.RefDir, 30); k != "" {
		head += "【教学参考库（教务精选抓取的原文资料；讲解这些主题时优先依据原文、保持准确不脱轨，引用注明《标题》）】\n" + k + "\n\n"
	}
	if p := profile.Block(t.ProfilePath); p != "" {
		head += p + "\n\n"
	}
	return head + system
}

const teachSystem = `你是前哨 2.0 的 AI 助教，正在一对一把一个知识点教给学习者。

核心纪律（最高优先）：先给「动作」，再给「内容」——学习者最先要看到的是「现在动手做什么」，不是概念。一次只推进一步。

开场（你的第一条消息）固定四段，缺一不可，每段单独成行、标题原样保留：
【今天做什么】一句话，说清这节课学完能干什么。
【现在就做这一件】一个动作，必须含具体对象：哪个文件/哪条命令/哪段代码/在哪个输入框写什么。不允许出现没有对象的泛泛建议。
【怎么算做对了】可观察的判据：输出长什么样 / 报错消失 / 能解释一句为什么。
【卡住了点这里】一句提示，指向「我卡住了」按钮。
四段之后可以有最多 3 段讲解，每段不超过 5 行；概念铺垫一律放在四段之后。

样例效应：先展示一个可运行/可模仿的 worked example，再让学习者改它——不许先讲后做。
错误反馈纪律（Dweck 成长型思维）：指出错必须含一句「具体差在哪一步」；禁止「天赋/聪明/不适合」类评价词。
防习得性无助：同一类点连续 2 次错 → 降一档、给一个更小的同类题再走。
时间预算（小穗三轮问答随档生效）：
- 预算=两分钟版 → 一道最小可验题收尾（一次动作+一次贴输出）
- 预算=10 分钟 → 一句话讲完（≤3 句）+ 1 道检查题
- 预算=30 分钟 → 三段以内 + 两道检查题
- 未填 → 正常展开
一次只推进一步：
- 每条回复只允许包含一个待办动作；学习者完成后才给下一步，不许一条消息里堆多步。
- 学习者回复的任何内容先按「动作结果」判定：做出来了→肯定+下一步动作；做不出来→给一步最小提示，不给完整答案。

检查题判定式（不考复述，考做出来）：
- 题面必须是可执行验证的：「做完上面那一步，把你的输出贴上来」「运行它，把报错贴上来」「把 X 改成 Y，说出会发生什么」。
- 禁止「请解释 X 的原理/谈谈你的理解」这类没有输出的题。
- 判定标准：能做出来=通过；做不出来=给下一步最小提示（一处、一句），连续两次做不出来才给完整解答。
- 答对 → 先肯定他做对的关键点，再给一个加深的小追问或真实应用场景，然后单独一行输出标记 [PASS]；[PASS:partial]（半懂）同理。

轻量出口（小穗五轮三「看见人」）：
- 学习者消息以「(这个我会)」开头 → 出一道最有挑战的判定式验收题，过了直接 [PASS]（履历记「已验证」，不重新讲基础）；没过就当正常教学继续，不羞辱。
- 学习者消息以「(太浅了)」开头 → 整体加深一档：少铺垫、多硬货，保持四段动作模板不变。

其余纪律：
- 学习者岔开话题，温和拉回「我们先把《当前知识点》收尾」。
- 每条回复 ≤250 字；永远以让学习者动一次脑/动一次手收尾。
- [PASS] 与 [PASS:partial] 必须单独成行，前后不要加别的字。
- 加深追问必须单独成一条消息并等学习者回答：绝不在同一条消息里既抛新问题又给收尾标记。收尾标记只出现在学习者回答完你最后一次追问之后的那条回复里。
- 收尾自查：判定 [PASS]/[PASS:partial] 时，如果这个知识点还有你完全没讲到、学习者也没问到的要点，在标记行之前先输出一行「【还有没讲的：某某要点——要继续学吗？】」等学习者决定；都讲到了就不加这一行。`

// ---------- 课程 ----------

// normalizeSubject 科目名归一化：口语输入映射到规范学科（拍照→摄影）；已是规范名原样返回。
// LLM 不可用或解析失败时用原名（不阻塞开课）。
func (t *Tutor) normalizeSubject(name string) string {
	if t.Client == nil {
		return name
	}
	raw, err := t.Client.ChatJSON(t.withAgents(
		`你是学科名归一化助手。学习者输入的想学内容可能口语化、笼统（如"拍照""修图""炒股"）。
输出 JSON {"subject":"规范学科名"}：若输入已是规范学科名则原样返回；否则映射到最贴近的通用规范学科（拍照→摄影，修图→图像处理，炒股→投资学）。学科名不超过 8 个字。`),
		[]llm.Message{{Role: "user", Content: name}})
	if err != nil {
		return name
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &wrap) != nil {
		return name
	}
	for _, k := range []string{"subject", "name"} {
		if r, ok := wrap[k]; ok {
			var s string
			if json.Unmarshal(r, &s) == nil && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return name
}

// StartSubject 用户给科目 → 归一化 → LLM 分解成 4-8 个有序知识点。
// 单科目在学：开新课=替换旧科目（旧会话文件与履历保留，历史仍可从沉淀进入）。
// 已在学同名科目则直接返回（幂等）。
func (t *Tutor) StartSubject(name string) (*Curriculum, error) {
	if t.Client == nil {
		return nil, fmt.Errorf("LLM 未配置")
	}
	name = t.normalizeSubject(name)
	cur := t.Curriculum()
	for _, s := range cur.Subjects {
		if s == name {
			return cur, nil
		}
	}
	var lessons []Lesson
	var perr error
	var err error
	for attempt := 0; attempt < 2; attempt++ { // LLM 偶发格式漂移：解析失败自动重试一次
		var raw string
		raw, err = t.Client.ChatJSON(t.withAgents(
			`你是课程设计师。学习者想学一个科目，请把它分解成 4-8 个有序知识点（从基础到进阶）。
每个知识点：title（≤16字，名词短语）、brief（一句话：学完能做什么）。
分解原则：每个知识点都是 30 分钟内可以「讲解+一道检查题」收尾的最小单元；先概念后应用。
JSON 顶层键必须是 "nodes"。`),
			[]llm.Message{{Role: "user", Content: "科目：" + name}})
		if err != nil {
			continue
		}
		lessons, perr = parseCourse(raw)
		if perr == nil && len(lessons) > 0 {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	if perr != nil {
		return nil, fmt.Errorf("课程分解解析失败: %w", perr)
	}
	if len(lessons) == 0 {
		return nil, fmt.Errorf("课程分解为空")
	}
	// 替换式：在学课程只能有一个
	cur.Subjects = []string{name}
	base := t.now().Format("0102") + "-" + sanitize(name)
	newLessons := make([]Lesson, 0, len(lessons))
	for i, n := range lessons {
		newLessons = append(newLessons, Lesson{
			ID:      fmt.Sprintf("%s-%02d", base, i+1),
			Subject: name,
			Title:   n.Title,
			Brief:   n.Brief,
			Status:  "active",
			Order:   i + 1,
		})
	}
	cur.Lessons = newLessons
	cur.UpdatedAt = t.now()
	t.saveCurriculum(cur)
	return cur, nil
}

// Curriculum 读课程（无文件返回空）。
func (t *Tutor) Curriculum() *Curriculum {
	cur := &Curriculum{UpdatedAt: t.now()}
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
	s.UpdatedAt = t.now()
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
	s.OpeningMeta = &OpeningMeta{Index: lesson.Order, Title: lesson.Title, Why: lesson.Brief}
	sys := teachSystem
	// 小穗三轮问题1：开场同样守时间预算——10 分钟档开场一句话+一道题
	switch t.TimeBudget {
	case "2":
		sys += "\n\n【今天的时间预算】两分钟版——只给一道最小可验题：一句动作+贴个输出即收尾，不讲理论。"
	case "10":
		sys += "\n\n【今天的时间预算】学习者今天只有 10 分钟——开场一句话讲完（最多 3 句），立刻给一道检查题。宁可少讲，不要展开。"
	case "30":
		sys += "\n\n【今天的时间预算】学习者今天有半小时——开场三段以内，检查题最多两道。"
	}
	user := fmt.Sprintf("知识点：《%s》——%s\n科目：%s\n现在开始教学：输出你的开场（讲解+检查题）。",
		lesson.Title, lesson.Brief, lesson.Subject)
	if t.FetchNote != "" {
		// AI 主导（宪法操作原则）：助教知道今天进了什么货，收尾时主动推荐延伸
		user += "\n\n【今日进货】以下是你刚抓取入库的学习材料——若与本次知识点相关，收尾时主动推荐给学习者（说清为什么值得读/做）；无关则不提：\n" + t.FetchNote
		t.FetchNote = "" // 只带一次，防每课重复推销
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: user})
	reply, err := t.Client.Chat(t.withAgents(sys), s.Messages)
	if err != nil {
		return nil, false, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	t.save(s)
	return s, true, nil
}

// Verdict 判定结果。
type Verdict struct {
	Kind  string `json:"kind"` // ""=继续对话 | pass | partial
	Reply string `json:"reply"`
}

// Send 学习者发言 → 助教回复；解析 [PASS] 判定并落履历标记。
// 返回：回复文本、判定（pass/partial 时调用方记履历）、错误。
func (t *Tutor) Send(lesson Lesson, userText string) (Verdict, error) {
	s := t.Load(lesson.ID)
	if s == nil {
		return Verdict{}, fmt.Errorf("会话不存在（先 open）")
	}
	if s.Verdict != "" {
		// 收尾后不锁对话：学习者可能在回答收尾前的加深追问，或继续追问
		return t.followup(s, userText)
	}
	if t.Client == nil {
		return Verdict{}, fmt.Errorf("LLM 未配置")
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	// 小穗三轮问题1：时间预算进助教——量随人走，不随课程表
	sys := teachSystem
	switch t.TimeBudget {
	case "10":
		sys += "\n\n【今天的时间预算】学习者今天只有 10 分钟——一句话讲完（最多 3 句），然后立刻给一道检查题。宁可少讲，不要展开；检查题只出一道。"
	case "30":
		sys += "\n\n【今天的时间预算】学习者今天有半小时——讲解控制在三段以内，检查题最多两道。"
	default:
		// 90 或未填：正常展开
	}
	reply, conv, err := t.runAgent(t.withAgents(sys), s.Messages, t.Tools)
	if err != nil {
		return Verdict{}, err
	}
	s.Messages = conv // 含工具帧的完整对话（教学上下文不丢）
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

// followup 收尾后的陪聊：学习者可能在回答收尾前的加深追问，或继续追问。
// 正常回应，但不改变判定、不再输出收尾标记（履历已记，不重复）。
func (t *Tutor) followup(s *Session, userText string) (Verdict, error) {
	if t.Client == nil {
		return Verdict{}, fmt.Errorf("LLM 未配置")
	}
	sys := `这个知识点已经学习收尾。学习者还在继续输入——可能是在回答你收尾前的加深追问，也可能是新问题。
正常回应：如果他在回答你的追问，判断对错并点评；如果是新问题，直接解答。≤250 字，保持助教口吻。
不要再输出 [PASS] 或 [PASS:partial] 标记（履历已记录，不重复）。`
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	reply, err := t.Client.Chat(sys, s.Messages)
	if err != nil {
		return Verdict{}, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	t.save(s)
	return Verdict{Kind: "", Reply: reply}, nil
}

// MarkLessonDone 课程状态推进（履历由调用方写）。
func (t *Tutor) MarkLessonDone(nodeID string) {
	cur := t.Curriculum()
	for i := range cur.Lessons {
		if cur.Lessons[i].ID == nodeID {
			cur.Lessons[i].Status = "done"
		}
	}
	cur.UpdatedAt = t.now()
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
