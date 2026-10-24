// Package server — 前哨 2.0 应用壳：本地 Web 服务（宪法 §二「全托管学习机」的日用形态）。
//
// 用户的界面只剩两个动作：完成今天丢来的内容、看自己的履历。
// 视觉纪律（用户审美锚点）：纸白浅场、墨蓝单 ramp、粉 #FB7299 唯一点缀 ≤8%；
// 无 streak、无百分比表演、无 hover 浮起、无三色霓虹（负清单全部生效）。
package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"slices"

	"outpost2/internal/ability"
	"outpost2/internal/bandit"
	"outpost2/internal/evidence"
	"outpost2/internal/exercise"
	"outpost2/internal/graduation"
	"outpost2/internal/llm"
	"outpost2/internal/profile"
	"outpost2/internal/reflib"
	"outpost2/internal/sandbox"
	"outpost2/internal/scheduler"
	"outpost2/internal/tutor"
)

//go:embed static/index.html
var indexHTML []byte

// Store 应用壳的文件持久化（引擎层的 JSON 落盘装配）。
type Store struct {
	Dir string
}

func (s *Store) load(path string, v any) bool {
	b, err := os.ReadFile(filepath.Join(s.Dir, path))
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (s *Store) save(path string, v any) {
	_ = os.MkdirAll(s.Dir, 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.Dir, path), b, 0o644)
}

// Goal 产出物（持久化形态：验收清单可消化）。
type Goal struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Deadline string   `json:"deadline"`
	Progress float64  `json:"progress"`
	Actions  []string `json:"actions"` // 剩余验收动作（完成即消化）
}

// Completed 今日已完成项（内存+落盘）。
type Completed struct {
	Date    string   `json:"date"`
	Main    []string `json:"main"`    // 已完成切片（DeliverableID|actionIndex 语义由前端组装标题）
	Reviews []string `json:"reviews"` // 已完成复习卡 id
	Extras  []string `json:"extras"`
	Mood    string   `json:"mood,omitempty"`
	Shape   string   `json:"shape,omitempty"`
}

// Reading 阅读材料：教务策展产出的原文（或中文整理/翻译）。读完记「知道」级履历。
type Reading struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Content     string `json:"content"`               // 正文（中文整理/翻译，Markdown）
	Original    string `json:"original,omitempty"`    // 英文原文（如有）
	Translation string `json:"translation,omitempty"` // Original 的中文翻译（按需生成缓存）
	Done        bool   `json:"done"`
	ReadAt      string `json:"readAt,omitempty"`
}

// Server 应用壳。
type Server struct {
	Dir        string               `json:"dir"`
	Goals      []Goal               `json:"goals"`
	Plan       *scheduler.TodayPlan `json:"plan,omitempty"`
	Done       Completed            `json:"done"`
	Ledger     *evidence.Ledger     `json:"-"` // 履历单一真相源=ledger.json（Record 时落盘）；state.json 不存副本——此前双写不一致导致重启即丢履历（事故 2026-10-20）
	Bandit     *bandit.Bandit       `json:"bandit"`
	Ability    *ability.Tracker     `json:"ability"`
	Exercises  []exercise.Exercise  `json:"exercises"`  // 今天的实操练习（助教按实时进度出）
	Readings   []Reading            `json:"readings"`   // 阅读材料（阅读管理器主体）
	PracBias   map[string]int       `json:"pracBias"`   // 科目→难度偏置（楼梯式 DDA：±80 内）
	PracStreak map[string]int       `json:"pracStreak"` // 科目→连对计数（bias 上探的依据）
	// 真实知识连接："核心ID->语境ID"→语境式练习真实调用次数——关联图的强边来源
	ContextEdges map[string]int `json:"contextEdges"`
	// 错题回归（Magpie 思想）：答错的知识点记入错题本，下次出题优先出换场景变式——自适应让人看得见
	WrongHits map[string]*WrongHit `json:"wrongHits"`
	// 模型配置口：多 Profile 热切换（key 只落本地 llm.json，0600，不入仓库）
	LLMStore    *llm.Store       `json:"-"`
	AgentsPath  string           `json:"-"` // AGENTS.md 约束文件（每次 LLM 调用热读）
	ProfilePath string           `json:"-"` // 学习者画像 memory/profile.md（热读注入 + 方向同步时自动沉淀）
	RefDir      string           `json:"-"` // 教学参考库 kb/（教务抓取的原文，教学注入清单）
	Tutor       *tutor.Tutor     `json:"-"`
	Exer        *exercise.Engine `json:"-"`
}

func (s *Server) load(path string, v any) bool {
	b, err := os.ReadFile(filepath.Join(s.Dir, path))
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func (s *Server) save(path string, v any) {
	_ = os.MkdirAll(s.Dir, 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(s.Dir, path), b, 0o644)
}

// NewServer 装配：缺 goals 时写入种子（90 天冲刺三产出物）。
func NewServer(dir string) *Server {
	s := &Server{Dir: dir, Ledger: evidence.New(), Bandit: bandit.New(time.Now().UnixNano()), Ability: ability.New(), AgentsPath: filepath.Join(dir, "AGENTS.md"), ProfilePath: profile.Ensure(dir), RefDir: reflib.Ensure(dir)}
	// 模型配置口：llm.json 多 Profile（兼容旧单配置自动迁移）；AGENTS.md 用户约束（缺失写默认模板）
	if st, err := llm.LoadStore(filepath.Join(dir, "llm.json")); err == nil {
		s.LLMStore = st
	}
	if _, err := os.Stat(s.AgentsPath); os.IsNotExist(err) {
		_ = os.WriteFile(s.AgentsPath, []byte(defaultAgentsMD), 0o644)
	}
	s.rebuildLLM()
	loaded := s.load("state.json", s)
	// 履历从单一真相源恢复（此前只信 state.json 的过期副本，重启即丢——事故 2026-10-20）
	s.load("ledger.json", &s.Ledger)
	// 阅读材料合并：内容以 readings.json（教务策展，覆盖式）为准，Done/翻译缓存以 state.json 为准
	persisted := s.Readings
	s.load("readings.json", &s.Readings)
	byID := map[string]Reading{}
	for _, p := range persisted {
		byID[p.ID] = p
	}
	for i := range s.Readings {
		if p, ok := byID[s.Readings[i].ID]; ok {
			s.Readings[i].Done = p.Done
			s.Readings[i].ReadAt = p.ReadAt
			s.Readings[i].Translation = p.Translation
		}
	}
	if loaded {
		return s
	}
	// 种子：90 天冲刺（90天FDE证据冲刺-v1草案.md 的三产出物）
	now := time.Now()
	s.Goals = []Goal{
		{ID: "P1", Title: "部署品：有真实用户的小工具", Deadline: now.AddDate(0, 0, 21).Format("2006-01-02"), Progress: 0,
			Actions: []string{"定选题（解决自己真实痛点）", "最小可用版跑通", "部署到公网", "找 ≥3 个真实用户", "录 2 分钟演示视频"}},
		{ID: "P2", Title: "Agent 作品：开源 + 实践长文", Deadline: now.AddDate(0, 0, 45).Format("2006-01-02"), Progress: 0,
			Actions: []string{"选型（多 agent 协作/调度引擎/工具链）", "仓库+README+QuickStart", "发布实践长文", "收获 ≥5 star/fork"}},
		{ID: "P3", Title: "FDE 证据页 + ≥30 份投递", Deadline: now.AddDate(0, 0, 90).Format("2006-01-02"), Progress: 0,
			Actions: []string{"证据页上线", "简历按 FDE 改写", "累计 30 份投递", "≥3 场真实面试"}},
	}
	s.saveState()
	return s
}

func (s *Server) saveState() { s.save("state.json", s) }

// WrongHit 一条错题记录：练习首次作答失败时记入；之后任一题通过该知识点即转已巩固。
type WrongHit struct {
	Topic   string    `json:"topic"`
	Subject string    `json:"subject,omitempty"`
	Count   int       `json:"count"`
	Pending bool      `json:"pending"` // 尚未做对过——出题时优先变式重出
	LastAt  time.Time `json:"lastAt"`
}

func wrongKey(subject, topic string) string {
	return subject + "|" + strings.TrimSpace(topic)
}

// recordWrongHit 练习首次作答失败 → 记入错题本。
func (s *Server) recordWrongHit(subject, topic string) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return
	}
	if s.WrongHits == nil {
		s.WrongHits = map[string]*WrongHit{}
	}
	k := wrongKey(subject, topic)
	h := s.WrongHits[k]
	if h == nil {
		h = &WrongHit{Topic: topic, Subject: subject}
		s.WrongHits[k] = h
	}
	h.Count++
	h.Pending = true
	h.LastAt = time.Now()
}

// pendingWrongTopics 科目下待巩固的错题知识点（空科目=全部）。
func (s *Server) pendingWrongTopics(subject string) []string {
	out := []string{}
	for _, h := range s.WrongHits {
		if h.Pending && (subject == "" || h.Subject == subject) {
			out = append(out, h.Topic)
		}
	}
	return out
}

func sessMessages(sess *tutor.DirectionSession) []llm.Message {
	if sess == nil {
		return []llm.Message{}
	}
	return sess.Messages
}

func sessProposal(sess *tutor.DirectionSession) *tutor.Proposal {
	if sess == nil {
		return nil
	}
	return sess.Proposal
}

// rebuildLLM 按当前激活 Profile 重建 LLM 客户端（切换/新增模型后热生效）。
func (s *Server) rebuildLLM() {
	if s.LLMStore == nil {
		return
	}
	p, err := s.LLMStore.ActiveProfile()
	if err != nil {
		return
	}
	client := llm.NewClient(p.Config())
	s.Tutor = &tutor.Tutor{Dir: s.Dir, Client: client, AgentsPath: s.AgentsPath, ProfilePath: s.ProfilePath, RefDir: s.RefDir}
	s.Exer = &exercise.Engine{Client: client, AgentsPath: s.AgentsPath, ProfilePath: s.ProfilePath}
}

func (s *Server) readAgents() string {
	b, err := os.ReadFile(s.AgentsPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// defaultAgentsMD 首启写入的用户可编辑约束模板（每次 LLM 调用热读，改完即生效）。
const defaultAgentsMD = `# AGENTS.md — AI 助教行为约束

> 本文件在**每次** AI 调用时注入，改完保存即生效（无需重启）。
> 与系统默认纪律冲突时，以本文件为准。删掉本文件则回到默认纪律。

## 教学边界
- 只教当前知识点课上讲过的内容；对话没教到的概念不出题、不默认学习者会。
- 每条回复 ≤250 字，永远以让学习者动一次脑/动一次手收尾。
- 语气温和不评判，不用考试腔，多用生活化类比。

## 收尾规则
- [PASS] 与 [PASS:partial] 必须单独成行。
- 加深追问必须单独成条并等学习者回答；不在同一条消息里既提问又收尾。
- 收尾自查：还有没讲到也没被问到的要点 → 先输出【还有没讲的：…要继续学吗？】。

## 出题与批改
- 只出实操题（动手算 / 写代码 / 场景决策），不出背诵默写题。
- 语境式：旧概念在新题路径中被自然调用，不在题面重新讲解。
- 批改先说对错再给一步提示，不直接报答案。

## 我的自定义约束
（在下面添加你自己的要求，例如：讲解尽量引用我的行业背景；少用数学符号；全程中文……）
`

// RefreshPlan 重新生成今日计划（bandit 学形态，规则引擎给骨架）。
func (s *Server) RefreshPlan() scheduler.TodayPlan {
	now := time.Now()
	dels := make([]scheduler.Deliverable, 0, len(s.Goals))
	for _, g := range s.Goals {
		deadline, _ := time.Parse("2006-01-02", g.Deadline)
		dels = append(dels, scheduler.Deliverable{ID: g.ID, Title: g.Title, Deadline: deadline, Progress: g.Progress, Actions: g.Actions})
	}
	shape := ResolveShapeCurrent(s, now)
	plan := scheduler.Plan(now, dels, s.dueCards(), energyFor(shape), nil)
	plan.Note = "形态:" + string(shape) + " | " + plan.Note
	s.Plan = &plan
	s.Done = Completed{Date: now.Format("2006-01-02"), Shape: string(shape)}
	s.saveState()
	return plan
}

// energyFor 形态 → 配额（与 policy.go 同构）。
func energyFor(shape string) scheduler.Energy {
	switch scheduler.PlanShape(shape) {
	case scheduler.ShapeSprint:
		return scheduler.EnergyHigh
	case scheduler.ShapeMaintain:
		return scheduler.EnergyLow
	}
	return scheduler.EnergyNormal
}

// sessionDigest 助教实际讲过的内容摘要——练习题只能考讲过的东西。
func sessionDigest(s *tutor.Session) string {
	var b strings.Builder
	for _, m := range s.Messages {
		if m.Role != "assistant" {
			continue
		}
		b.WriteString(m.Content)
		b.WriteString(" ")
		if b.Len() > 400 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 400 {
		out = out[:400] + "…"
	}
	return out
}

// ResolveShapeCurrent 当前策略（v1：bandit 后验从 state 持久化恢复）。
func ResolveShapeCurrent(s *Server, now time.Time) string {
	arms := []string{"standard", "sprint", "maintain"}
	best, bestSample := "standard", -1.0
	for _, arm := range arms {
		ab := s.Bandit.Posterior["local|"+arm]
		if ab[0] <= 0 || ab[1] <= 0 {
			ab = [2]float64{1, 1}
		}
		sample := ab[0] / (ab[0] + ab[1]) // v1 server 均值策略（确定性，测试友好）
		if sample > bestSample {
			best, bestSample = arm, sample
		}
	}
	_ = now
	return best
}

// dueCards v0：无 FSRS 实时对接时从 state 里读手动维护的复习项（占位）。
func (s *Server) dueCards() []scheduler.ReviewCard {
	var due []scheduler.ReviewCard
	s.load("reviews.json", &due)
	return due
}

// CompleteMain 完成一片主推进：消化产出物的首个剩余动作 + 履历记 Used。
func (s *Server) CompleteMain(title string) {
	s.Done.Main = append(s.Done.Main, title)
	for gi := range s.Goals {
		g := &s.Goals[gi]
		if len(g.Actions) > 0 && g.Actions[0] == title {
			g.Actions = g.Actions[1:]
			total := float64(len(g.Actions)) + g.Progress*10
			if total > 0 {
				g.Progress = 1 - float64(len(g.Actions))/total
			}
			break
		}
	}
	s.saveState()
}

// SettleMood 日终自报 → bandit 奖励（学会了且想继续学的可计算近似）。
func (s *Server) SettleMood(mood string, completed bool) {
	shape := "standard"
	if s.Done.Shape != "" {
		shape = s.Done.Shape
	}
	reward := 0.1
	switch {
	case completed && mood == "还想来":
		reward = 1.0
	case completed:
		reward = 0.6
	}
	s.Bandit.Settle("local", shape, reward)
	s.Done.Mood = mood
	s.saveState()
}

// Handlers 装配路由。
func (s *Server) Handlers() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		if s.Plan == nil {
			s.RefreshPlan()
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		if s.Plan == nil {
			s.RefreshPlan()
		}
		k, u, d := s.Ledger.Counts()
		g, gReason := graduation.Evaluate(graduation.DomainStats{DoneCount: d, MaintainDays: 0}, graduation.DefaultConfig(), time.Now())
		today := (*tutor.Lesson)(nil)
		curriculum := map[string]any{}
		sessions := map[string]*tutor.Session{}
		if s.Tutor != nil {
			cur := s.Tutor.Curriculum()
			today = s.Tutor.TodayLesson()
			curriculum = map[string]any{"subjects": cur.Subjects, "lessons": cur.Lessons}
			// 全部已有会话打包进 state——前端打开已学知识点=纯本地渲染（用户要求 ≤2ms 级响应）
			for _, l := range cur.Lessons {
				if sess := s.Tutor.Load(l.ID); sess != nil {
					sessions[l.ID] = sess
				}
			}
			// 换科目后被移出课程表的历史会话：履历里出现过的 id 也打包，沉淀仍可回看
			for _, p := range s.Ledger.List() {
				if _, ok := sessions[p.ID]; !ok {
					if sess := s.Tutor.Load(p.ID); sess != nil {
						sessions[p.ID] = sess
					}
				}
			}
		}
		writeJSON(w, map[string]any{
			"plan": s.Plan, "done": s.Done, "goals": s.Goals,
			"exercises": s.Exercises, "readings": s.Readings, "contextEdges": s.ContextEdges,
			"wrongHits": s.WrongHits,
			"ledger":    s.Ledger.List(), "counts": map[string]int{"known": k, "used": u, "done": d},
			"graduation": map[string]any{"state": string(g), "reason": gReason},
			"curriculum": curriculum, "today": today, "sessions": sessions,
		})
	})
	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.RefreshPlan())
	})
	mux.HandleFunc("/api/complete-main", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.CompleteMain(req.Title)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/complete-review", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.Done.Reviews = append(s.Done.Reviews, req.ID)
		s.saveState()
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/mood", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Mood      string `json:"mood"`
			Completed bool   `json:"completed"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.SettleMood(req.Mood, req.Completed)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/subject", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.Name) == "" {
			http.Error(w, "科目名不能为空", 400)
			return
		}
		if s.Tutor == nil {
			http.Error(w, "LLM 未配置（outpost2-data/llm.json）", 400)
			return
		}
		cur, err := s.Tutor.StartSubject(req.Name)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// 换/开科目=今日练习作废重出（题目永远跟着当下在学的东西走）
		s.Exercises = nil
		s.saveState()
		writeJSON(w, cur)
	})
	mux.HandleFunc("/api/tutor/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			NodeID string `json:"nodeId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lesson := findLesson(s, req.NodeID)
		if lesson == nil {
			http.Error(w, "知识点不存在", 404)
			return
		}
		sess, isNew, err := s.Tutor.StartOrResume(*lesson)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"session": sess, "isNew": isNew})
	})
	mux.HandleFunc("/api/tutor/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			NodeID string `json:"nodeId"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lesson := findLesson(s, req.NodeID)
		if lesson == nil {
			http.Error(w, "知识点不存在", 404)
			return
		}
		v, err := s.Tutor.Send(*lesson, req.Text)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		verdict := ""
		if v.Kind == "pass" || v.Kind == "partial" {
			s.Ledger.Record(lesson.ID, lesson.Title, evidence.Known, time.Now(), "tutor:"+lesson.Subject, "")
			s.Tutor.MarkLessonDone(lesson.ID)
			s.save("ledger.json", s.Ledger)
			verdict = v.Kind
		}
		writeJSON(w, map[string]any{"reply": v.Reply, "verdict": verdict})
	})
	mux.HandleFunc("/api/exercise/generate", func(w http.ResponseWriter, _ *http.Request) {
		if len(s.Exercises) > 0 { // 幂等：今天的题已出过
			writeJSON(w, map[string]any{"exercises": s.Exercises})
			return
		}
		if s.Exer == nil {
			http.Error(w, "LLM 未配置（outpost2-data/llm.json）", 400)
			return
		}
		arms := []string{}
		digests := map[string]string{}
		curSub := ""
		if s.Tutor != nil {
			for _, l := range s.Tutor.Curriculum().Lessons {
				// 练习=收尾之后的巩固：只从收尾过（done）的知识点出题，与关联图资格一致
				if l.Status != "done" {
					continue
				}
				sess := s.Tutor.Load(l.ID)
				if sess == nil {
					continue
				}
				arms = append(arms, l.ID)
				digests[l.ID] = sessionDigest(sess)
				curSub = l.Subject
			}
		}
		if len(arms) == 0 {
			http.Error(w, "先把今天的课收尾——收尾过的知识点才有练习", 400)
			return
		}
		// 策略层 v1：Thompson 采样选题——高奖励的知识点多出（利用），低后验的也会被采到（探索）
		picked := s.Bandit.PickN("practice", arms, 3)
		byID := map[string]tutor.Lesson{}
		for _, l := range s.Tutor.Curriculum().Lessons {
			byID[l.ID] = l
		}
		// 语境配对（i+1 语境式练习）：核心之外的更早收尾知识点作为语境——
		// 旧概念在新题的解题路径中被自然调用（最早学的优先），不做背诵式复习
		contextPool := []string{}
		for _, id := range arms {
			if !slices.Contains(picked, id) {
				contextPool = append(contextPool, id)
			}
		}
		// 真实知识连接累积：每道语境式练习 = 核心与语境之间的一次真实调用（关联图强边）
		if s.ContextEdges == nil {
			s.ContextEdges = map[string]int{}
		}
		for i, id := range picked {
			if len(contextPool) > 0 {
				s.ContextEdges[id+"->"+contextPool[i%len(contextPool)]]++
			}
		}
		ordered := make([]string, 0, len(picked))
		for i, id := range picked {
			l := byID[id]
			entry := "核心知识点：" + l.Subject + " / " + l.Title + "\n  课上讲了：" + digests[id]
			if len(contextPool) > 0 {
				ctxID := contextPool[i%len(contextPool)]
				cl := byID[ctxID]
				entry += "\n语境知识点（解题路径须自然经过它，不要重新讲解）：" + cl.Subject + " / " + cl.Title + "\n  课上讲了：" + digests[ctxID]
			}
			ordered = append(ordered, entry)
		}
		// 难度自适应 v1（分层架构：能力模型→策略）：按当前 Elo 难度带约束题面难度
		diffNote := ""
		if s.Ability != nil {
			lo, hi := s.Ability.DifficultyBand("practice")
			tone := "按区间正常出题"
			if s.PracBias != nil {
				if s.PracBias[curSub] >= 40 {
					tone = "学习者近期连对——在区间内上探偏难（贴上沿）"
				} else if s.PracBias[curSub] <= -40 {
					tone = "学习者近期受挫——压到区间下沿，给足提示"
				}
			}
			diffNote = fmt.Sprintf("\n\n学习者当前能力：Elo≈%.0f。所有题面难度必须落在 Elo %.0f–%.0f 区间（对应预期正确率约 50%%–85%%），%s：不出明显超纲的题，也不出送分题。", s.Ability.Rating("practice"), lo, hi, tone)
		}
		// 错题回归（Magpie 思想）：曾做错的知识点优先出换场景变式——它记得你错过的题
		wrongList := s.pendingWrongTopics(curSub)
		if len(wrongList) > 0 {
			diffNote += "\n\n【错题回归】以下知识点学习者曾做错——优先以其中 1-2 个为核心知识点出变式题：换一个新场景、考同一个点，难度不升：\n- " + strings.Join(wrongList, "\n- ")
		}
		list, err := s.Exer.Generate(ordered, diffNote)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for i := range list { // 回填臂归属与语境：批改结果将作为该知识点的奖励回写
			if i < len(picked) {
				list[i].LessonID = picked[i]
				if l, ok := byID[picked[i]]; ok {
					list[i].Subject = l.Subject
				}
			}
			if len(contextPool) > 0 {
				ctxID := contextPool[i%len(contextPool)]
				if l, ok := byID[ctxID]; ok {
					list[i].Context = l.Title
				}
			}
			for _, w := range wrongList { // 错题回归题打标（前端「巩固」标签）
				if strings.TrimSpace(list[i].Topic) == w {
					list[i].Review = true
					break
				}
			}
		}
		s.Exercises = list
		s.saveState()
		writeJSON(w, map[string]any{"exercises": s.Exercises})
	})
	mux.HandleFunc("/api/exercise/answer", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Exer == nil {
			http.Error(w, "LLM 未配置（outpost2-data/llm.json）", 400)
			return
		}
		for i := range s.Exercises {
			if s.Exercises[i].ID != req.ID {
				continue
			}
			reply, done, err := s.Exer.Answer(&s.Exercises[i], req.Text)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			// 能力模型更新（RL 分层第一层）：每题首次作答=一次能力观测，Elo 随对错浮动
			first := true
			for _, m := range s.Exercises[i].Messages {
				if m.Role == "user" {
					first = false
					break
				}
			}
			if first && s.Ability != nil {
				s.Ability.Answer("practice", s.Ability.Rating("practice"), done)
			}
			// 错题回归：首次作答失败记入错题本（下次出题变式重出）；通过即转已巩固
			sub, topic := s.Exercises[i].Subject, s.Exercises[i].Topic
			if done {
				if h := s.WrongHits[wrongKey(sub, topic)]; h != nil {
					h.Pending = false
				}
			} else if first {
				s.recordWrongHit(sub, topic)
			}
			// 策略更新（在线训练）：批改结果即奖励；楼梯式难度调整（连对上探/受挫回落）
			rw := 0.1
			if done {
				rw = 1.0
			}
			if s.Exercises[i].LessonID != "" {
				s.Bandit.Settle("practice", s.Exercises[i].LessonID, rw)
			}
			if sub := s.Exercises[i].Subject; sub != "" {
				if s.PracBias == nil {
					s.PracBias = map[string]int{}
				}
				if s.PracStreak == nil {
					s.PracStreak = map[string]int{}
				}
				if done {
					s.PracStreak[sub]++
					if s.PracStreak[sub] >= 3 {
						if s.PracBias[sub] < 80 {
							s.PracBias[sub] += 40
						}
						s.PracStreak[sub] = 0
					}
				} else {
					if s.PracBias[sub] > -80 {
						s.PracBias[sub] -= 40
					}
					s.PracStreak[sub] = 0
				}
			}
			if done { // 实操完成=「用过」级履历
				s.Ledger.Record(s.Exercises[i].ID, s.Exercises[i].Title, evidence.Used, time.Now(), "practice", "")
				s.save("ledger.json", s.Ledger)
			}
			s.saveState()
			writeJSON(w, map[string]any{"reply": reply, "verdict": done})
			return
		}
		http.Error(w, "练习不存在", 404)
	})
	mux.HandleFunc("/api/reading/done", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for i := range s.Readings {
			if s.Readings[i].ID != req.ID || s.Readings[i].Done {
				continue
			}
			s.Readings[i].Done = true
			s.Readings[i].ReadAt = time.Now().Format(time.RFC3339)
			s.Ledger.Record(s.Readings[i].ID, s.Readings[i].Title, evidence.Known, time.Now(), "reading", s.Readings[i].URL)
			s.save("ledger.json", s.Ledger)
			s.saveState()
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		http.Error(w, "阅读材料不存在或已读完", 404)
	})
	mux.HandleFunc("/api/reading/translate", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for i := range s.Readings {
			rd := &s.Readings[i]
			if rd.ID != req.ID {
				continue
			}
			if rd.Original == "" {
				http.Error(w, "该材料没有英文原文可翻译", 400)
				return
			}
			if rd.Translation == "" {
				if s.Exer == nil || s.Exer.Client == nil {
					http.Error(w, "LLM 未配置（outpost2-data/llm.json）", 400)
					return
				}
				tr, err := s.Exer.Client.Chat(
					`你是技术文档翻译。把英文技术文档翻译成中文：保留 Markdown 结构与代码块（代码与代码注释不翻译），术语首次出现时括注英文原词。直接输出译文 Markdown，不要解释。`,
					[]llm.Message{{Role: "user", Content: rd.Original}})
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				rd.Translation = strings.TrimSpace(tr)
				s.saveState()
			}
			writeJSON(w, map[string]any{"translation": rd.Translation})
			return
		}
		http.Error(w, "阅读材料不存在", 404)
	})
	mux.HandleFunc("/api/direction/open", func(w http.ResponseWriter, r *http.Request) {
		sess := s.Tutor.LoadDirection()
		writeJSON(w, map[string]any{"messages": sessMessages(sess), "proposal": sessProposal(sess)})
	})
	mux.HandleFunc("/api/direction/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		reply, proposal, err := s.Tutor.DirectionSend(req.Text)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"reply": reply, "proposal": proposal})
	})
	mux.HandleFunc("/api/direction/commit", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Subject string         `json:"subject"`
			Lessons []tutor.Lesson `json:"lessons"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Subject == "" || len(req.Lessons) == 0 {
			http.Error(w, "提案不完整", 400)
			return
		}
		// 蒸馏输入要在 ResetDirection 删文件前快照
		sess := s.Tutor.LoadDirection()
		cur := s.Tutor.SetSubjectWithLessons(req.Subject, req.Lessons)
		s.Exercises = nil // 新方向 = 今日练习作废重出
		s.Tutor.ResetDirection()
		s.saveState()
		// 方向讨论里的背景/时间/目标沉淀为学习者画像（异步，失败不影响开课）
		if t := s.Tutor; sess != nil && len(sess.Messages) > 0 {
			go func() {
				if err := t.DistillProfile(sess); err != nil {
					log.Printf("学习者画像沉淀失败（不影响开课）: %v", err)
				}
			}()
		}
		writeJSON(w, map[string]any{"ok": true, "curriculum": map[string]any{"subjects": cur.Subjects, "lessons": cur.Lessons}})
	})
	mux.HandleFunc("/api/llm/profiles", func(w http.ResponseWriter, _ *http.Request) {
		profiles := []map[string]any{}
		active := ""
		if s.LLMStore != nil {
			active = s.LLMStore.Active
			for name, pr := range s.LLMStore.Profiles {
				profiles = append(profiles, map[string]any{
					"name": name, "baseUrl": pr.BaseURL, "model": pr.Model,
					"keyMasked": pr.MaskKey(), "active": name == s.LLMStore.Active,
				})
			}
		}
		writeJSON(w, map[string]any{"active": active, "profiles": profiles, "agents": s.readAgents()})
	})
	mux.HandleFunc("/api/llm/profile", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name    string `json:"name"`
			BaseURL string `json:"baseUrl"`
			Key     string `json:"key"`
			Model   string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" || req.BaseURL == "" || req.Model == "" {
			http.Error(w, "name / baseUrl / model 必填", 400)
			return
		}
		if s.LLMStore == nil {
			s.LLMStore = &llm.Store{Profiles: map[string]llm.Profile{}}
		}
		key := strings.TrimSpace(req.Key)
		if key == "" { // key 留空=保留该 profile 已存 key
			if old, ok := s.LLMStore.Profiles[req.Name]; ok {
				key = old.Key
			}
		}
		if key == "" {
			http.Error(w, "key 不能为空（首次配置该模型时必填）", 400)
			return
		}
		s.LLMStore.Profiles[req.Name] = llm.Profile{Name: req.Name, BaseURL: req.BaseURL, Key: key, Model: req.Model}
		s.LLMStore.Active = req.Name
		if err := s.LLMStore.Save(filepath.Join(s.Dir, "llm.json")); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.rebuildLLM()
		writeJSON(w, map[string]any{"ok": true, "active": req.Name})
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if err := os.WriteFile(s.AgentsPath, []byte(req.Content), 0o644); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		writeJSON(w, map[string]any{"content": s.readAgents()})
	})
	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if err := profile.Save(s.ProfilePath, req.Content); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		writeJSON(w, map[string]any{"content": profile.Load(s.ProfilePath)})
	})
	mux.HandleFunc("/api/reflib", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, reflib.LoadIndex(s.RefDir))
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, _ *http.Request) {
		nodes := []map[string]any{}
		edges := []map[string]any{}
		if s.Tutor != nil {
			cur := s.Tutor.Curriculum()
			for i, l := range cur.Lessons {
				nodes = append(nodes, map[string]any{"id": l.ID, "title": l.Title, "depth": 1, "subject": l.Subject, "status": l.Status})
				if i > 0 && cur.Lessons[i-1].Subject == l.Subject {
					// 弱边：课程先后顺序（教学序列，非知识本身的连接）
					edges = append(edges, map[string]any{"from": cur.Lessons[i-1].ID, "to": l.ID, "type": "sequence"})
				}
			}
		}
		// 强边：语境式练习真实发生过的知识调用（次数=权重）——图从目录可视化变成大脑使用记录
		for pair, n := range s.ContextEdges {
			parts := strings.SplitN(pair, "->", 2)
			if len(parts) != 2 {
				continue
			}
			edges = append(edges, map[string]any{"from": parts[0], "to": parts[1], "type": "context", "weight": n})
		}
		for _, p := range s.Ledger.List() {
			dup := false
			for _, nd := range nodes {
				if nd["id"] == p.ID {
					dup = true
					break
				}
			}
			if !dup { // 履历条目若与课程知识点同 id（收尾记录），不重复画节点
				nodes = append(nodes, map[string]any{"id": p.ID, "title": p.Title, "depth": int(p.Depth), "subject": "evidence", "status": "done"})
			}
		}
		writeJSON(w, map[string]any{"nodes": nodes, "edges": edges})
	})
	mux.HandleFunc("/api/sandbox/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CaseID string `json:"caseId"`
			Dir    string `json:"dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lib := sandbox.Library()
		c, ok := lib[req.CaseID]
		if !ok {
			http.Error(w, "未知案例", 400)
			return
		}
		if _, err := sandbox.Create(req.Dir, c); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "dir": req.Dir, "hint": "用编辑器修 " + firstFile(c) + "，修完回来点验收"})
	})
	mux.HandleFunc("/api/sandbox/verify", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CaseID string `json:"caseId"`
			Dir    string `json:"dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lib := sandbox.Library()
		c, ok := lib[req.CaseID]
		if !ok {
			http.Error(w, "未知案例", 400)
			return
		}
		sess := &sandbox.Session{Case: c, Root: req.Dir}
		res, err := sess.Verify()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if res.Passed {
			s.Ledger.Record(c.KnowledgePoint, c.Title, evidence.Used, time.Now(), "sandbox:"+c.ID, "")
			s.save("ledger.json", s.Ledger)
		}
		writeJSON(w, res)
	})
	mux.HandleFunc("/evidence.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.Ledger.WriteHTML(w, "学习者", time.Now())
	})
	return mux
}

func graduationReason(done int) string {
	if done == 0 {
		return "还没做过东西——能力要有实证才谈毕业"
	}
	return "做过 " + fmt.Sprint(done) + " 件——继续"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func firstFile(c *sandbox.BreakCase) string {
	for k := range c.Files {
		return k
	}
	return "main.go"
}

// Serve 启动本地服务并返回实际地址。
func Serve(addr string) (*Server, error) {
	s := NewServer(filepath.Join(".", "outpost2-data"))
	go func() { _ = http.ListenAndServe(addr, s.Handlers()) }()
	time.Sleep(200 * time.Millisecond)
	return s, nil
}

var _ = template.HTMLEscapeString // 保留模板引用

// findLesson 从课程中取知识点。
func findLesson(s *Server, nodeID string) *tutor.Lesson {
	if s.Tutor == nil {
		return nil
	}
	for _, l := range s.Tutor.Curriculum().Lessons {
		if l.ID == nodeID {
			l2 := l
			return &l2
		}
	}
	return nil
}
