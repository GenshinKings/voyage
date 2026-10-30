// Package tutor — 对话式教学会话（前哨 2.0 学习页的核心）。
//
// 流程：用户给科目 → LLM 分解成有序知识点 → 每天丢第一个未完成的 → 用户点开
// 进入对话 → 助教按「讲解+检查题→判→追问/点破」的护栏形态教 → [PASS] 判定
// → 履历记 Known（后续实践升 Used/Done）。
//
// 会话持久化：tutor/<nodeID>.json——跨天连续（DeepSeek 式左栏会话的底层）。
package tutor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"voyage/internal/llm"
	"voyage/internal/profile"
	"voyage/internal/reflib"
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
	// 第六轮-P0-1 引导档位：推进挂在 Tutor（人）级——单课一次 [PASS] 即收尾，Session 级连击永远攒不满 3 次（测试暴露的设计缺陷，2026-10-28 修正）。
	// 此字段保留为「进课时的档位快照」（旧会话文件兼容），不再推进。
	Guide       int    `json:"guide,omitempty"`
	GuideStreak [2]int `json:"guideStreak,omitempty"` // 遗留字段，不再使用
	// 第六轮-P1-1 追问判真会：首次通过后挂起「你为什么会这么写」，答上才收尾（履历延迟入账，不违反「履历不撤回」）
	AskWhy bool `json:"askWhy,omitempty"`
	// 第八轮-P0-1 判定诊断：每次判定事件一条（通道/耗时/是否二次校验）——判定不再是无据可查的黑箱
	Diagnostics []Diag `json:"diagnostics,omitempty"`
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
	MistakeNote string         // 第六轮-P0-2 误因回看（server 注入；开场第一句必须先点它，用后清空）
	MemoryNote  string         // 第七轮-P0-3 记忆引用（server 注入；「你上次说……」式自然引用，必须逐字保留原文，用后清空）
	NotesDir    string         // 学习者笔记目录 notes/（每日巡检的检查对象，只读）
	TimeBudget  string         // 小穗三轮问题1：今天的时间预算 10/30/90（server 从 state 同步）——决定讲解长度与检查题数量
	// Clock 小穗四轮·架构 P1-2：注入时间源（测试用假钟测 fsrs/判定链路不睡）——nil=走 walltime
	Clock func() time.Time
	// 第六轮-P0-1 引导档位（随人不随课——挂在 Tutor 级跨课程累计；server 常驻期间有效）
	GuideLevel  int    `json:"-"` // 0=未设 → 视为样例档
	GuideStreak [2]int `json:"-"` // [连过数, 连没过数]
	// 教学指标（docs/teach/spec.md 第八节）：nil=未加载，取用时经 stats() 懒加载
	Stats *Stats `json:"-"`
}

// guideLevel 当前引导档位（未设=样例）。
func (t *Tutor) guideLevel() int {
	if t.GuideLevel == 0 {
		return GuideSample
	}
	return t.GuideLevel
}

// guideBump 一次判定信号（pass=true 通过）推进人级连击并换档。
func (t *Tutor) guideBump(pass bool) {
	if pass {
		t.GuideStreak[0]++
		t.GuideStreak[1] = 0
	} else {
		t.GuideStreak[1]++
		t.GuideStreak[0] = 0
	}
	t.GuideLevel = NextGuide(t.guideLevel(), t.GuideStreak[0], t.GuideStreak[1])
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
- 做不出来（检查题判定失败或半懂） → 回复末尾**必须**再单独一行输出误因标签，格式：[CAUSE:类别|证据一句话]。类别只有五种：concept（概念没懂）、condition（看漏条件）、env（环境/报错相关）、slip（拼写/命令打错——仅限字面错误）、unfinished（没做完要求的步骤——漏了要求里的某一步）。证据写具体差在哪一步（如「他写的 a==b 应该是 a!=b」）。标签行前后不加别的字；日常提问（非检查题作答）时不输出此标签。

轻量出口（小穗五轮三「看见人」）：
- 学习者消息以「(这个我会)」开头 → 出一道最有挑战的判定式验收题，过了直接 [PASS]（履历记「已验证」，不重新讲基础）；没过就当正常教学继续，不羞辱。
- 学习者消息以「(太浅了)」开头 → 整体加深一档：少铺垫、多硬货，保持四段动作模板不变。

其余纪律：
- 学习者岔开话题，温和拉回「我们先把《当前知识点》收尾」。
- 意图不明（乱码/半句话/与当前题面无关的输入）时：可以给出你的最佳猜测，但必须写成**疑问句等学习者确认**（「你是想问 X 吗？」），学习者确认前不执行任何下一步、不给完整解答——猜错会把人带偏，问一句永远更便宜。（学生实测 2026-10-28：把「?」猜成「路径写死」直接往下讲，猜对是运气。）
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
		st := "pending"
		if i == 0 {
			st = "active" // 学生实测④（2026-10-28）：只有当前该学的一节 active，其余 pending
		}
		newLessons = append(newLessons, Lesson{
			ID:      fmt.Sprintf("%s-%02d", base, i+1),
			Subject: name,
			Title:   n.Title,
			Brief:   n.Brief,
			Status:  st,
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
	// 学生实测④（2026-10-28）读取规范化：active 唯一性——老数据/外部改动可能多节 active，
	// 保留 Order 最小的一节，其余降 pending。幂等，对单 active 数据零影响。
	activeSeen := false
	ordered := make([]int, 0, len(cur.Lessons))
	for i := range cur.Lessons {
		ordered = append(ordered, i)
	}
	sort.Slice(ordered, func(a, b int) bool { return cur.Lessons[ordered[a]].Order < cur.Lessons[ordered[b]].Order })
	for _, i := range ordered {
		l := &cur.Lessons[i]
		if l.Status == "active" {
			if activeSeen {
				l.Status = "pending"
			} else {
				activeSeen = true
			}
		}
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
func (t *Tutor) StartOrResume(ctx context.Context, lesson Lesson) (*Session, bool, error) {
	if s := t.Load(lesson.ID); s != nil {
		return s, false, nil
	}
	if t.Client == nil {
		return nil, false, fmt.Errorf("LLM 未配置")
	}
	s := &Session{NodeID: lesson.ID, Title: lesson.Title}
	s.OpeningMeta = &OpeningMeta{Index: lesson.Order, Title: lesson.Title, Why: lesson.Brief}
	sys := teachSystem
	// 第六轮-P0-1：新知识点首触=样例档（完整例子+直接指令，不让新手被问懵）
	sys += guidePrompt(t.guideLevel())
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
	if t.MistakeNote != "" {
		// 第六轮-P0-2：误因回看——不重头讲一遍，开场第一句先点上次卡在哪
		user += "\n\n【误因回看】这个知识点学习者此前有过这些错误记录：" + t.MistakeNote + "\n开场第一句必须先点它（「上次你这里是想反了」式的回顾引用），而不是从头再讲；讲完再给检查题。"
		t.MistakeNote = ""
	}
	if t.MemoryNote != "" {
		// 第七轮-P0-3：记忆引用——把长期记忆变成教学动作。最多一条（server 侧已选好），
		// 引用必须逐字保留记录原文（验收：引用文本能在条目里逐字找到）；标注了旧结论就照措辞提醒。
		user += "\n\n【记忆引用】学习者的长期记忆里有这条相关记录：" + t.MemoryNote + "\n开场时用一句自然的话引用它（「你上次说……」式），引号内的记录原文必须逐字保留、不许改写；若记录标注了「旧结论」，按原措辞提醒学习者现在可能已经变化。只引用这一条，没有其他记忆就不要再提「上次」。"
		t.MemoryNote = ""
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: user})
	reply, err := llm.ChatWith(ctx, t.Client, t.withAgents(sys), s.Messages)
	if err != nil {
		return nil, false, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	t.save(s)
	return s, true, nil
}

// Verdict 判定结果。
type Verdict struct {
	Kind  string `json:"kind"` // ""=继续对话 | pass | partial | fu-pass | fu-fuzzy
	Reply string `json:"reply"`
	Guide int    `json:"guide"` // 第六轮-P0-1：本轮后的引导档位（前端「讲」卡小字与 state.guideLevels 用）
	// 第八轮-P0-1 双通道：本轮判定来自哪个通道；结构化判定不通过时的误因（server 记入错因分布）
	Channel  string `json:"channel,omitempty"` // marker | structured | ""
	Cause    string `json:"cause,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	// 结构化判定带出来的「卡在哪」和「下一步做什么」：只说「再想想」等于没反馈
	Where  string `json:"where,omitempty"`
	Action string `json:"action,omitempty"`
}

// Send 学习者发言 → 助教回复；解析 [PASS] 判定并落履历标记。
// 返回：回复文本、判定（pass/partial 时调用方记履历）、错误。
func (t *Tutor) Send(ctx context.Context, lesson Lesson, userText string) (Verdict, error) {
	s := t.Load(lesson.ID)
	if s == nil {
		return Verdict{}, fmt.Errorf("会话不存在（先 open）")
	}
	if s.Verdict != "" {
		// 收尾后不锁对话：学习者可能在回答收尾前的加深追问，或继续追问
		return t.followup(ctx, s, userText)
	}
	if t.Client == nil {
		return Verdict{}, fmt.Errorf("LLM 未配置")
	}
	// 第六轮-P0-1：档位语义进 system——随人切换（连过升/连败降在 Send 末尾推进）
	// 点「我卡住了」=显式退档信号：立即退一档（不等 2 次累计——验收口径「点了卡住→退回一级」）。
	// stuckRound 传递到判定尾部：卡住轮不做失败累计/误因记录（自查修复 2026-10-28——该轮样例讲解
	// 可能顺带输出 [CAUSE]，若再走失败分支会双降一档并误记误因分布）。
	stuckRound := strings.HasPrefix(strings.TrimSpace(userText), "(我卡住了)")
	if stuckRound {
		t.GuideLevel = t.guideLevel() - 1
		if t.GuideLevel < GuideSample {
			t.GuideLevel = GuideSample
		}
		t.GuideStreak = [2]int{}
	}
	sys := teachSystem + guidePrompt(t.guideLevel())
	if stuckRound {
		sys += "\n\n【刚收到「我卡住了」】已退回一档——接下来按完整样例给他一颗定心丸：一个最小可运行的例子+一句可直接执行的动作。"
	}
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	// 小穗三轮问题1：时间预算进助教——量随人走，不随课程表
	switch t.TimeBudget {
	case "10":
		sys += "\n\n【今天的时间预算】学习者今天只有 10 分钟——一句话讲完（最多 3 句），然后立刻给一道检查题。宁可少讲，不要展开；检查题只出一道。"
	case "30":
		sys += "\n\n【今天的时间预算】学习者今天有半小时——讲解控制在三段以内，检查题最多两道。"
	default:
		// 90 或未填：正常展开
	}
	reply, conv, err := t.runAgent(ctx, t.withAgents(sys), s.Messages, t.Tools)
	if err != nil {
		return Verdict{}, err
	}
	s.Messages = conv // 含工具帧的完整对话（教学上下文不丢）
	// 第八轮-P0-1 判定双通道：先解析标记（容错），缺失时走结构化判定
	kind, markWarn := ParsePassMark(reply)
	channel := "marker"
	var vCause, vReason, vWhere, vAction string
	switch {
	case kind != "":
		s.addDiag(Diag{At: t.now(), Channel: "marker", Verdict: kind, Note: markWarn})
	case !stuckRound && judgeWarranted(s, userText):
		// 结构化通道：把学生回答+验收标准丢给一次独立小调用——闭环不依赖模型记得吐标记
		start := t.now()
		jres, jerr := t.judgeStructured(ctx, lesson, s)
		d := Diag{At: t.now(), Channel: "structured", DurationMs: t.now().Sub(start).Milliseconds(),
			Retried: jres.Retried, Note: markWarn}
		if jerr != nil {
			d.Verdict = "error"
			d.Note += " " + jerr.Error()
		} else if jres.Pass {
			kind, d.Verdict = "pass", "pass"
		} else {
			d.Verdict = "fail"
			vCause, vReason = jres.Cause, jres.Reason
			vWhere, vAction = jres.Where, jres.Action
		}
		channel = "structured"
		s.addDiag(d)
	case markWarn != "":
		s.addDiag(Diag{At: t.now(), Channel: "marker", Verdict: "", Note: markWarn})
	}
	if kind != "" {
		s.Verdict = kind
	}
	// stuckSelfCheck 自查修复（2026-10-28）：卡住轮已在 HasPrefix 分支立即退档；
	// 该轮助教给完整样例时可能顺带输出 [CAUSE] 标签——若再走失败分支会双降一档，
	// 且卡住不是「答错」，不应记入误因分布。
	if kind == "pass" {
		t.guideBump(true)
	} else if kind == "partial" {
		// 半过=过了但有洞：清零连败不计升格，也别当失败惩罚
		t.GuideStreak[0] = 0
		t.GuideStreak[1] = 0
	} else if !stuckRound {
		reason, _ := ParseCause(reply)
		structuredFail := channel == "structured" && vCause != ""
		if ValidCause(reason) || structuredFail {
			// 第六轮-P0-2：检查题判定失败（显式 [CAUSE] 标签或结构化通道 fail）= 一次未通过，进入引导降档判据
			t.guideBump(false)
		}
	}
	t.save(s)
	t.recordVerdict(kind, stuckRound, vCause, channel)
	return Verdict{Kind: kind, Reply: reply, Guide: t.guideLevel(), Channel: channel, Cause: vCause, Evidence: vReason, Where: vWhere, Action: vAction}, nil
}

// followup 收尾后的陪聊：学习者可能在回答收尾前的加深追问，或继续追问。
// 正常回应，但不改变判定、不再输出收尾标记（履历已记，不重复）。
//
// 第六轮-P1-1：若「为什么这么写」追问挂起（AskWhy），这一轮改为真会度判定：
// 答得出 → [FU-PASS]（调用方收尾入账）；答不上/含糊 → [FU-FUZZY]（不收尾 + 给一道更小的同类题）。
func (t *Tutor) followup(ctx context.Context, s *Session, userText string) (Verdict, error) {
	if t.Client == nil {
		return Verdict{}, fmt.Errorf("LLM 未配置")
	}
	if s.AskWhy {
		sys := `学习者刚刚判定通过这是「真会度」追问「你为什么会这么写？一句话。」的答复。只判这一句：
- 答得出原理/说得清理由（哪怕不完美）→ 回复先简短点评一句，然后单独一行输出标记 [FU-PASS]。
- 答不上/含糊其辞/只是复述表象（如「我看着对」「因为教程这么写」）→ 单独一行输出标记 [FU-FUZZY]，然后直接给一道**更小的同类题**（比刚才的检查题更简单、更具体、给半个开头），不批评不说教。
[FU-PASS] 与 [FU-FUZZY] 必须单独成行。`
		s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
		reply, err := llm.ChatWith(ctx, t.Client, t.withAgents(sys), s.Messages)
		if err != nil {
			return Verdict{}, err
		}
		s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
		kind := ""
		if strings.Contains(reply, "[FU-FUZZY]") {
			kind = "fu-fuzzy"
			// 真会度未过：会话判定降为半懂（履历本就未记账，无「撤回」冲突）
			s.Verdict = "partial"
		} else if strings.Contains(reply, "[FU-PASS]") || strings.Contains(reply, "[PASS]") {
			// 自查修复（学生实测复测 2026-10-28）：FU 语境下模型受历史惯性输出裸 [PASS]——
			// 语义唯一（追问轮没有别的通过标记），按 fu-pass 收尾；否则静默吞掉、AskWhy 清了却不收尾，卡死收节
			kind = "fu-pass"
		}
		if kind != "" {
			// 第八轮-P0-1：只有出了 FU 判定才摘下追问挂起并留诊断——模型忘吐标记时
			// 下一条继续判，追问不会静默丢失（此前 AskWhy 一律清空，闭环会无声卡死）
			s.AskWhy = false
			s.addDiag(Diag{At: t.now(), Channel: "marker", Verdict: kind})
		}
		t.save(s)
		return Verdict{Kind: kind, Reply: reply, Guide: t.guideLevel(), Channel: "marker"}, nil
	}
	sys := `这个知识点已经学习收尾。学习者还在继续输入——可能是在回答你收尾前的加深追问，也可能是新问题。
正常回应：如果他在回答你的追问，判断对错并点评；如果是新问题，直接解答。≤250 字，保持助教口吻。
不要再输出 [PASS] 或 [PASS:partial] 标记（履历已记录，不重复）。`
	s.Messages = append(s.Messages, llm.Message{Role: "user", Content: userText})
	reply, err := llm.ChatWith(ctx, t.Client, sys, s.Messages)
	if err != nil {
		return Verdict{}, err
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant", Content: reply})
	t.save(s)
	return Verdict{Kind: "", Reply: reply, Guide: t.guideLevel()}, nil
}

// AskRealUse 首次通过后的「真会度」追问：追加一条助教消息「你为什么会这么写？」并挂起判定。
// 履历与收尾动作由调用方在 [FU-PASS] 到达后执行（延迟入账，符合「履历不撤回」宪法）。
func (t *Tutor) AskRealUse(nodeID string) {
	s := t.Load(nodeID)
	if s == nil {
		return
	}
	s.AskWhy = true
	if s.Verdict == "" {
		s.Verdict = "pass" // 收尾在途：会话不再走主判定流，后续输入进追问分支
	}
	s.Messages = append(s.Messages, llm.Message{Role: "assistant",
		Content: "判定通过。不过收尾前只差一步——**你为什么会这么写？一句话。**"})
	t.save(s)
}

// MarkLessonDone 课程状态推进（履历由调用方写）。
func (t *Tutor) MarkLessonDone(nodeID string) {
	cur := t.Curriculum()
	hasActive := false
	for i := range cur.Lessons {
		if cur.Lessons[i].ID == nodeID {
			cur.Lessons[i].Status = "done"
		}
		if cur.Lessons[i].Status == "active" {
			hasActive = true
		}
	}
	// 学生实测④（2026-10-28）：收尾后若无 active，按顺序提升下一节 pending——
	// 不变式：任意时刻恰好一节 active（TodayLesson「第一个 active」依赖它）
	if !hasActive {
		for i := range cur.Lessons {
			if cur.Lessons[i].Status == "pending" {
				cur.Lessons[i].Status = "active"
				break
			}
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
