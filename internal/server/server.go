// Package server — 前哨 2.0 应用壳：本地 Web 服务（宪法 §二「全托管学习机」的日用形态）。
//
// 用户的界面只剩两个动作：完成今天丢来的内容、看自己的履历。
// 视觉纪律（用户审美锚点）：纸白浅场、墨蓝单 ramp、粉 #FB7299 唯一点缀 ≤8%；
// 无 streak、无百分比表演、无 hover 浮起、无三色霓虹（负清单全部生效）。
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"slices"
	"sort"

	"outpost2/fsrs"
	"outpost2/internal/ability"
	"outpost2/internal/bandit"
	"outpost2/internal/evidence"
	"outpost2/internal/exercise"
	"outpost2/internal/ghfetch"
	"outpost2/internal/graduation"
	"outpost2/internal/iterate"
	"outpost2/internal/llm"
	"outpost2/internal/notes"
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

// Opportunity 机会条目（P1 去处）：手工维护 opportunities.json——真实 issue/实习/接单入口。
type Opportunity struct {
	Type  string `json:"type"` // issue|intern|gig
	Title string `json:"title"`
	Link  string `json:"link"`
	Note  string `json:"note,omitempty"`
}

// LastStop 接续锚点（小穗闭环 P0-1）：跨天/冷启动时一句话把人接回来。
type LastStop struct {
	NodeID string    `json:"nodeId"`
	Title  string    `json:"title"`
	Step   string    `json:"step"` // 学/练/副本 + 进度描述
	At     time.Time `json:"at"`
}

// Stats 最小埋点（小穗收敛轮⑧）：只本地统计，不联网。
type Stats struct {
	DaySteps    map[string][2]int `json:"daySteps"` // date -> [已完成步,总步=3]
	StreakLast  string            `json:"streakLast,omitempty"`
	StreakCount int               `json:"streakCount"`
	StepMins    []int             `json:"stepMins"` // 每步耗时样本（分钟，cap 50）
	Verdicts    map[string]int    `json:"verdicts"` // pass/partial 判定分布
}

// Trophy 纪念卡：通关揭晓时生成的战利品（收藏墙 + 沉淀连锚的数据源）。
type Trophy struct {
	Name      string    `json:"name"`
	Repo      string    `json:"repo"`
	Impact    string    `json:"impact"`
	Nodes     []string  `json:"nodes"` // 参与知识点（连锚边来源）
	Stages    int       `json:"stages"`
	Date      string    `json:"date"`  // 2026-10-27
	Short     string    `json:"short"` // 10·27
	At        time.Time `json:"at"`
	DungeonID string    `json:"dungeonId"`
	// 小穗二轮问题1：证据必须挂用户自己的产物（他的仓库/commit/文件），不是被复现的原仓库
	UserEvidence string `json:"userEvidence,omitempty"`
}

// Reading 孵化池条目：进货抓来的候选项目——助教孵化（Shadow）后可开门进副本。
type Reading struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	URL         string            `json:"url"`
	Content     string            `json:"content"`               // 正文（中文整理/翻译，Markdown）
	Original    string            `json:"original,omitempty"`    // 英文原文（如有）
	Translation string            `json:"translation,omitempty"` // Original 的中文翻译（按需生成缓存）
	Brief       string            `json:"brief,omitempty"`       // 助教导读（深入项目源码后的结构化消化）
	Shadow      *tutor.ShadowMeta `json:"shadow,omitempty"`      // 影子元数据（孵化器产出；nil=未孵化）
	Impact      string            `json:"impact,omitempty"`      // 影响力叙事（揭晓 payload 专供，候选期不下发）
	Done        bool              `json:"done"`
	ReadAt      string            `json:"readAt,omitempty"`
}

// Server 应用壳。
type Server struct {
	Dir       string               `json:"dir"`
	Goals     []Goal               `json:"goals"`
	Plan      *scheduler.TodayPlan `json:"plan,omitempty"`
	Done      Completed            `json:"done"`
	Ledger    *evidence.Ledger     `json:"-"` // 履历单一真相源=ledger.json（Record 时落盘）；state.json 不存副本——此前双写不一致导致重启即丢履历（事故 2026-10-20）
	Bandit    *bandit.Bandit       `json:"bandit"`
	Ability   *ability.Tracker     `json:"ability"`
	Exercises []exercise.Exercise  `json:"exercises"`          // 今天的实操练习（助教按实时进度出）
	ExerDate  string               `json:"exerDate,omitempty"` // 练习生成日期（跨日作废重出——评审 P0-1：原实现永不轮换）
	// FSRS 复习（评审 P1-5：fsrs 包此前零接入，产品对遗忘无防御）
	FSCards    map[string]*fsrs.Card `json:"fsCards"`    // 知识点 → 记忆卡（收尾时建卡）
	FSRS       *fsrs.Scheduler       `json:"-"`          // 调度器（内存态）
	Readings   []Reading             `json:"readings"`   // 孵化池（副本候选；json 字段名保留 readings 兼容旧数据）
	PracBias   map[string]int        `json:"pracBias"`   // 科目→难度偏置（楼梯式 DDA：±80 内）
	PracStreak map[string]int        `json:"pracStreak"` // 科目→连对计数（bias 上探的依据）
	// 真实知识连接："核心ID->语境ID"→语境式练习真实调用次数——关联图的强边来源
	ContextEdges map[string]int `json:"contextEdges"`
	// 错题回归（Magpie 思想）：答错的知识点记入错题本，下次出题优先出换场景变式——自适应让人看得见
	WrongHits map[string]*WrongHit `json:"wrongHits"`
	// 副本学习（用户 2026-10-26 设计）：用已收尾知识复现真实项目，通关揭晓恍然大悟
	Duns []*tutor.Dungeon `json:"dungeons"`
	// 纪念卡（副本 v2 悬念化）：通关揭晓时生成，收藏墙 + 沉淀连锚的数据源
	Trophies []*Trophy `json:"trophies"`
	// 实验开关（小穗收敛轮④）：Elo 能力估计 / bandit 题目选择——保留代码，默认关闭
	ExpElo    bool `json:"expElo,omitempty"`
	ExpBandit bool `json:"expBandit,omitempty"`
	// P0-1 接续（小穗闭环）：跨天记住用户停在哪一步
	LastStop *LastStop `json:"lastStop,omitempty"`
	// P0-3 体面暂停：暂停期不算断签、不推提醒、不显示落后
	PausedUntil *time.Time `json:"pausedUntil,omitempty"`
	// P1 去处：机会页（手工维护 opportunities.json）
	Opportunities []Opportunity `json:"opportunities,omitempty"`
	// 最小埋点（小穗收敛轮⑧）：只本地统计，不联网
	Stats      *Stats               `json:"stats,omitempty"`
	StepStarts map[string]time.Time `json:"-"` // 步开始时间（耗时统计用，内存态）
	// 孵化器存量补齐的去重状态（进程内）
	hatchOnce sync.Once
	hatchDay  atomic.Value // string 日期
	// 模型配置口：多 Profile 热切换（key 只落本地 llm.json，0600，不入仓库）
	LLMStore    *llm.Store       `json:"-"`
	AgentsPath  string           `json:"-"` // AGENTS.md 约束文件（每次 LLM 调用热读）
	ProfilePath string           `json:"-"` // 学习者画像 memory/profile.md（热读注入 + 方向同步时自动沉淀）
	RefDir      string           `json:"-"` // 教学参考库 kb/（教务抓取的原文，教学注入清单）
	NotesDir    string           `json:"-"` // 学习者笔记 notes/（助教每日巡检对象，只读）
	Tutor       *tutor.Tutor     `json:"-"`
	Exer        *exercise.Engine `json:"-"`

	// 内容进货（用户 2026-10-25 决策）：助教带 GitHub 抓取工具，定时+按钮触发
	FetchConf      *FetchConfig    `json:"-"` // fetch.json：代理/时刻/上次报告
	Gh             *ghfetch.Client `json:"-"` // GitHub 抓取客户端（代理从 FetchConf）
	fetchMu        sync.Mutex      `json:"-"`
	fetchRunning   bool            `json:"-"`
	routineRunning bool            `json:"-"`

	// 助教自迭代（用户 2026-10-25 纠正：内置助教自己在低峰时段迭代项目，非外部 agent 代劳）
	Obs       []tutor.Observation `json:"-"` // backlog.json：改进观察（open/done）
	RepoDir   string              `json:"-"` // 仓库根（迭代工作区；dataDir 的上一级）
	iterMu    sync.Mutex          `json:"-"`
	iterating bool                `json:"-"`
}

// Backlog backlog.json：助教的改进观察清单。
type Backlog struct {
	Updated      string              `json:"updated,omitempty"`
	Observations []tutor.Observation `json:"observations"`
}

// FetchConfig fetch.json：内容进货的配置与运行状态。
type FetchConfig struct {
	Proxy      string `json:"proxy"`                // HTTP 代理（如 http://127.0.0.1:7897；空=直连）
	Hour       int    `json:"hour"`                 // 每日抓取时刻（0-23，默认 7）
	LastRun    string `json:"lastRun,omitempty"`    // 上次运行日期 2006-01-02
	LastReport string `json:"lastReport,omitempty"` // 上次进货报告（助教输出）
	// 笔记巡检（AI 主导：助教每天对照教学内容查笔记，诊断主动可见）
	NoteRun    string `json:"noteRun,omitempty"`    // 上次笔记巡检日期
	NoteReport string `json:"noteReport,omitempty"` // 上次笔记诊断报告
	// 教学反思（每日例行第三步：发现项目缺漏 → backlog）
	ReflectRun string `json:"reflectRun,omitempty"` // 上次反思日期
	// 助教自迭代（低峰时段独立时刻；默认凌晨 3 点，可关）
	IterEnabled bool   `json:"iterEnabled,omitempty"` // 启用自迭代
	IterHour    int    `json:"iterHour,omitempty"`    // 迭代时刻 0-23（默认 3）
	IterRun     string `json:"iterRun,omitempty"`     // 上次迭代日期
	IterReport  string `json:"iterReport,omitempty"`  // 上次迭代报告
}

func (s *Server) loadFetchConf() {
	conf := &FetchConfig{Hour: 7}
	_ = s.load("fetch.json", conf)
	if conf.Hour < 0 || conf.Hour > 23 {
		conf.Hour = 7
	}
	s.FetchConf = conf
	s.Gh = ghfetch.New(conf.Proxy)
}

// truncateNote 进货提示截断（开场注入的体量控制：报告全文在设置页，教学只带摘要）。
func truncateNote(report string) string {
	report = strings.TrimSpace(report)
	if report == "" {
		return ""
	}
	if len(report) > 500 {
		report = report[:500] + "…（全文见设置页·内容进货）"
	}
	return report
}

// runIn 在 dir 里跑一条固定命令（自迭代工具用；不接受模型给的任意命令）。
func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// tailLines 取输出末 N 行（测试输出可能很长）。
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return s
	}
	return "…\n" + strings.Join(lines[len(lines)-n:], "\n")
}

func (s *Server) saveFetchConf() {
	if s.FetchConf != nil {
		s.save("fetch.json", s.FetchConf)
	}
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
	s := &Server{Dir: dir, Ledger: evidence.New(), Bandit: bandit.New(time.Now().UnixNano()), Ability: ability.New(), AgentsPath: filepath.Join(dir, "AGENTS.md"), ProfilePath: profile.Ensure(dir), RefDir: reflib.Ensure(dir), NotesDir: notes.Ensure(dir)}
	s.loadFetchConf()
	s.FSRS = fsrs.NewScheduler()
	// 助教改进观察 backlog + 仓库根（自迭代工作区）
	_ = s.load("backlog.json", &s.Obs)
	_ = s.load("dungeons.json", &s.Duns)
	_ = s.load("trophies.json", &s.Trophies)
	if abs, err := filepath.Abs(dir); err == nil {
		s.RepoDir = filepath.Dir(abs)
	}
	// 模型配置口：llm.json 多 Profile（兼容旧单配置自动迁移）；AGENTS.md 用户约束（缺失写默认模板）
	if st, err := llm.LoadStore(filepath.Join(dir, "llm.json")); err == nil {
		s.LLMStore = st
	}
	if _, err := os.Stat(s.AgentsPath); os.IsNotExist(err) {
		_ = os.WriteFile(s.AgentsPath, []byte(defaultAgentsMD), 0o644)
	}
	s.rebuildLLM()
	// 今日进货提示 → 助教开场带一次（AI 主导：收尾时主动推荐延伸材料）
	if s.Tutor != nil && s.FetchConf != nil && s.FetchConf.LastRun == time.Now().Format("2006-01-02") {
		s.Tutor.FetchNote = truncateNote(s.FetchConf.LastReport)
	}
	_ = s.load("opportunities.json", &s.Opportunities)
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
	s.Tutor = &tutor.Tutor{Dir: s.Dir, Client: client, AgentsPath: s.AgentsPath, ProfilePath: s.ProfilePath, RefDir: s.RefDir, Tools: s.buildFetchTools(), NotesDir: s.NotesDir}
	s.Exer = &exercise.Engine{Client: client, AgentsPath: s.AgentsPath, ProfilePath: s.ProfilePath}
}

// buildReadTools 只读抓取工具（trending/search/browse/fetch_file）——导读等只读任务专用，不给写入口。
func (s *Server) buildReadTools() []tutor.Tool {
	all := s.buildFetchTools()
	read := all[:0] // 复用底层数组前 4 个（trending/search/browse/fetch_file 顺序固定）
	for _, t := range all {
		if t.Name == "gh_trending" || t.Name == "gh_search" || t.Name == "gh_browse" || t.Name == "gh_fetch_file" {
			read = append(read, t)
		}
	}
	return read
}

// buildFetchTools 助教的 GitHub 进货工具组（Gh 未配置=无工具，教学自动降级为纯对话）。
func (s *Server) buildFetchTools() []tutor.Tool {
	if s.Gh == nil {
		return nil
	}
	trunc := func(tag, out string) string {
		if len(out) > 20000 {
			return out[:20000] + "\n…（" + tag + "已截断）"
		}
		return out
	}
	repoList := func(repos []ghfetch.Repo) string {
		var b strings.Builder
		for i, r := range repos {
			fmt.Fprintf(&b, "%d. %s ★%d", i+1, r.FullName, r.Stars)
			if r.Lang != "" {
				b.WriteString(" [" + r.Lang + "]")
			}
			if r.Desc != "" {
				b.WriteString(" " + r.Desc)
			}
			b.WriteString("\n   " + r.URL + "\n")
		}
		return b.String()
	}
	return []tutor.Tool{
		{
			Name: "gh_trending", Description: "抓 GitHub 今日热门仓库榜（可选语言过滤，如 go/python）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"language": map[string]any{"type": "string", "description": "语言过滤，可留空"}},
				"required": []string{}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Language string `json:"language"`
				}
				_ = json.Unmarshal(args, &req)
				repos, err := s.Gh.Trending(req.Language)
				if err != nil {
					return "", err
				}
				return trunc("列表", repoList(repos)), nil
			},
		},
		{
			Name: "gh_search", Description: "按关键词定向搜索 GitHub 仓库（支持 GitHub 搜索语法，如「机器学习 tutorial language:Python」）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"n":     map[string]any{"type": "integer", "description": "条数 1-20，默认 10"}},
				"required": []string{"query"}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Query string `json:"query"`
					N     int    `json:"n"`
				}
				_ = json.Unmarshal(args, &req)
				if strings.TrimSpace(req.Query) == "" {
					return "", fmt.Errorf("query 不能为空")
				}
				repos, err := s.Gh.Search(req.Query, req.N)
				if err != nil {
					return "", err
				}
				return trunc("列表", repoList(repos)), nil
			},
		},
		{
			Name: "gh_browse", Description: "看一个仓库的概览与文件树（判断 README 之外还有什么可学的源文件）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"repo": map[string]any{"type": "string", "description": "owner/repo"}},
				"required": []string{"repo"}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Repo string `json:"repo"`
				}
				_ = json.Unmarshal(args, &req)
				d, err := s.Gh.Detail(req.Repo)
				if err != nil {
					return "", err
				}
				tree, err := s.Gh.Tree(req.Repo)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%s ★%d\n%s\n默认分支：%s\n\n文件树（%d 个文件）：\n", req.Repo, d.Stars, d.Desc, d.DefaultBranch, len(tree))
				for _, p := range tree {
					b.WriteString("  " + p + "\n")
				}
				return trunc("文件树", b.String()), nil
			},
		},
		{
			Name: "gh_fetch_file", Description: "抓仓库里的一个文本文件全文（README/示例代码/教程文档；branch 留空=默认分支；二进制拒收、64KB 截断）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"repo":   map[string]any{"type": "string"},
				"path":   map[string]any{"type": "string"},
				"branch": map[string]any{"type": "string", "description": "可留空"}},
				"required": []string{"repo", "path"}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Repo, Path, Branch string
				}
				_ = json.Unmarshal(args, &req)
				branch := req.Branch
				if branch == "" {
					d, err := s.Gh.Detail(req.Repo)
					if err != nil {
						return "", err
					}
					branch = d.DefaultBranch
				}
				out, err := s.Gh.Raw(req.Repo, branch, req.Path)
				if err != nil {
					return "", err
				}
				return trunc("文件", out), nil
			},
		},
		{
			Name: "kb_save", Description: "把一份学习材料存入教学参考库（content 传全文=库内文档；source_path 传本地路径=外部引用；两者留一）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"title":       map[string]any{"type": "string", "description": "人读标题"},
				"digest":      map[string]any{"type": "string", "description": "一句话摘要≤60字：学完能做什么"},
				"content":     map[string]any{"type": "string", "description": "全文（库内文档）"},
				"source_path": map[string]any{"type": "string", "description": "本地路径（外部引用）"},
				"url":         map[string]any{"type": "string"},
				"domain":      map[string]any{"type": "string", "description": "六域 A-F，可留空"}},
				"required": []string{"title", "digest"}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Title, Digest, Content, SourcePath, URL, Domain string
				}
				_ = json.Unmarshal(args, &req)
				if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Digest) == "" {
					return "", fmt.Errorf("title 与 digest 必填")
				}
				d := reflib.Doc{
					ID:    "gh-" + time.Now().Format("0102") + "-" + reflib.SanitizeID(req.Title),
					Title: req.Title, URL: req.URL, Domain: req.Domain,
					Date: time.Now().Format("2006-01-02"), Digest: req.Digest,
					Path: req.SourcePath,
				}
				if err := reflib.Add(s.RefDir, d, req.Content); err != nil {
					return "", err
				}
				return "已入库教学参考库：" + d.ID + "（导航已更新）", nil
			},
		},
		{
			Name: "add_reading", Description: "把值得复现的项目加进学习者的副本孵化池（候选池，助教导读后可开门）",
			Schema: map[string]any{"type": "object", "properties": map[string]any{
				"title": map[string]any{"type": "string"},
				"url":   map[string]any{"type": "string"},
				"why":   map[string]any{"type": "string", "description": "为什么值得读（1-2 句）"}},
				"required": []string{"title", "why"}},
			Fn: func(args json.RawMessage) (string, error) {
				var req struct {
					Title, URL, Why string
				}
				_ = json.Unmarshal(args, &req)
				if strings.TrimSpace(req.Title) == "" {
					return "", fmt.Errorf("title 必填")
				}
				r := Reading{ID: "gh-" + time.Now().Format("0102-150405"), Title: req.Title, URL: req.URL}
				if req.Why != "" {
					r.Content = "**为什么值得读：**" + req.Why
				}
				s.Readings = append(s.Readings, r)
				s.save("readings.json", s.Readings) // fetch 增量写回（教务覆盖式更新时以文件为准，Done 状态由 state 合并保留）
				s.saveState()
				return "已加入阅读清单：" + req.Title, nil
			},
		},
	}
}

// noteContext 笔记巡检的教学上下文（科目/各知识点状态与简介）。
func (s *Server) noteContext() string {
	var b strings.Builder
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		if len(cur.Subjects) > 0 {
			fmt.Fprintf(&b, "- 在学科目：%s\n", strings.Join(cur.Subjects, "、"))
		}
		for _, l := range cur.Lessons {
			tag := "已收尾"
			if l.Status == "active" {
				tag = "进行中"
			}
			fmt.Fprintf(&b, "- 《%s》（%s）：%s\n", l.Title, tag, l.Brief)
		}
	}
	if b.Len() == 0 {
		return "（还没有开课——笔记检查仅按通用学习笔记标准）"
	}
	return b.String()
}

// RunNoteReview 触发一次笔记巡检（异步；与进货共用互斥锁）。结果落 FetchConfig 并主动可见。
// noteReviewCore 笔记巡检同步核（日期已做则跳过）。
func (s *Server) noteReviewCore() error {
	today := time.Now().Format("2006-01-02")
	if s.FetchConf != nil && s.FetchConf.NoteRun == today {
		return nil
	}
	if !s.beginStep() {
		return nil
	}
	defer s.endStep()
	report, err := s.Tutor.ReviewNotes(s.noteContext())
	if s.FetchConf != nil {
		s.FetchConf.NoteRun = today
		if err != nil {
			s.FetchConf.NoteReport = "巡检失败: " + err.Error()
		} else {
			s.FetchConf.NoteReport = report
		}
	}
	s.saveFetchConf()
	return err
}

// RunNoteReview 触发一次笔记巡检（异步）。
func (s *Server) RunNoteReview() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	go s.noteReviewCore()
	return nil
}

// curateContext 进货员的学习者上下文（科目/进度/库里已有——防重复进货）。
// ---------- 助教自迭代（反思收集 → 低峰自主实现） ----------

// RunReflect 每日教学反思：今天的教学数据 → 结构化观察 → backlog（防重复、封顶 20 条）。
// reflectCore 教学反思同步核（日期已做则跳过）。
func (s *Server) reflectCore() error {
	today := time.Now().Format("2006-01-02")
	if s.FetchConf != nil && s.FetchConf.ReflectRun == today {
		return nil
	}
	if !s.beginStep() {
		return nil
	}
	defer s.endStep()
	obs, err := s.Tutor.Reflect(s.todayCareer(), s.existingObservations())
	if err == nil && len(obs) > 0 {
		s.fetchMu.Lock()
		for i, o := range obs {
			o.ID = fmt.Sprintf("obs-%s-%d", time.Now().Format("0102"), i+1)
			o.At = today
			o.Status = "open"
			obs[i] = o
		}
		s.Obs = append(s.Obs, obs...)
		if len(s.Obs) > 20 { // backlog 封顶：最旧的 done/failed 先清，仍超则丢最旧
			var keep []tutor.Observation
			for _, o := range s.Obs {
				if o.Status == "open" || len(keep) < 20 {
					keep = append(keep, o)
				}
			}
			s.Obs = keep
		}
		s.saveBacklog()
		s.fetchMu.Unlock()
	}
	if s.FetchConf != nil {
		s.FetchConf.ReflectRun = today
		s.saveFetchConf()
	}
	return err
}

// RunReflect 触发一次教学反思（异步）。
func (s *Server) RunReflect() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	go s.reflectCore()
	return nil
}

// RunDailyRoutine 每日例行串行流水：进货 → 笔记巡检 → 反思（评审 P0-3：原实现同刻争锁+跨时刻静默跳过）。
// 各核自带日期幂等与步骤互斥——机器不在线跨过时刻，下次上线也会补跑（loop 的 >= 语义）。
func (s *Server) RunDailyRoutine() {
	s.fetchMu.Lock()
	if s.routineRunning {
		s.fetchMu.Unlock()
		return
	}
	s.routineRunning = true
	s.fetchMu.Unlock()
	go func() {
		if s.Tutor == nil { // 无 LLM 例行无从谈起
			s.fetchMu.Lock()
			s.routineRunning = false
			s.fetchMu.Unlock()
			return
		}
		if err := s.curateCore(); err != nil {
			log.Printf("例行·进货: %v", err)
		}
		if err := s.noteReviewCore(); err != nil {
			log.Printf("例行·笔记巡检: %v", err)
		}
		if err := s.reflectCore(); err != nil {
			log.Printf("例行·反思: %v", err)
		}
		s.fetchMu.Lock()
		s.routineRunning = false
		s.fetchMu.Unlock()
	}()
}

// todayCareer 反思输入：今天的教学生涯数据（会话/练习/进货/巡检）。
func (s *Server) todayCareer() string {
	var b strings.Builder
	today := time.Now().Format("2006-01-02")
	fmt.Fprintf(&b, "- 日期：%s\n", today)
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		var done, active []string
		for _, l := range cur.Lessons {
			if l.Status == "done" {
				done = append(done, l.Title)
			} else {
				active = append(active, l.Title)
			}
			if sess := s.Tutor.Load(l.ID); sess != nil && sess.UpdatedAt.Format("2006-01-02") == today {
				fmt.Fprintf(&b, "- 今日教学会话：《%s》共 %d 轮（判定 %q）\n", l.Title, len(sess.Messages)/2, sess.Verdict)
			}
		}
		if len(done) > 0 {
			fmt.Fprintf(&b, "- 已收尾：%s\n", strings.Join(done, "、"))
		}
		if len(active) > 0 {
			fmt.Fprintf(&b, "- 进行中：%s\n", strings.Join(active, "、"))
		}
	}
	for i := range s.Exercises {
		e := &s.Exercises[i]
		if e.Status == "done" || len(e.Messages) > 2 {
			fmt.Fprintf(&b, "- 练习《%s》（%s）：%d 轮作答，状态 %s\n", e.Title, e.Topic, (len(e.Messages)-1)/2, e.Status)
		}
	}
	if s.FetchConf != nil {
		if s.FetchConf.LastRun == today && s.FetchConf.LastReport != "" {
			fmt.Fprintf(&b, "- 今日进货报告：%.150s\n", s.FetchConf.LastReport)
		}
		if s.FetchConf.NoteRun == today && s.FetchConf.NoteReport != "" {
			fmt.Fprintf(&b, "- 今日笔记巡检：%.150s\n", s.FetchConf.NoteReport)
		}
	}
	if b.Len() == 0 {
		return "（今天没有教学活动——可输出空观察列表）"
	}
	return b.String()
}

func (s *Server) existingObservations() string {
	var lines []string
	for _, o := range s.Obs {
		if o.Status == "open" {
			lines = append(lines, "- "+o.Observation)
		}
	}
	if len(lines) == 0 {
		return "（空）"
	}
	return strings.Join(lines, "\n")
}

func (s *Server) saveBacklog() {
	s.save("backlog.json", Backlog{Updated: time.Now().Format("2006-01-02"), Observations: s.Obs})
}

func (s *Server) saveDuns() {
	s.save("dungeons.json", s.Duns)
}

// statsEnsure 埋点结构惰性初始化。
func (s *Server) statsEnsure() *Stats {
	if s.Stats == nil {
		s.Stats = &Stats{DaySteps: map[string][2]int{}, Verdicts: map[string]int{}}
	}
	if s.Stats.DaySteps == nil {
		s.Stats.DaySteps = map[string][2]int{}
	}
	if s.Stats.Verdicts == nil {
		s.Stats.Verdicts = map[string]int{}
	}
	return s.Stats
}

// recordVerdict 判定埋点：分布计数 + 连续天数 + 步耗时（open→verdict）。
func (s *Server) recordVerdict(verdict string, nodeId string) {
	if verdict == "" {
		return
	}
	st := s.statsEnsure()
	today := time.Now().Format("2006-01-02")
	if verdict == "partial" {
		st.Verdicts["partial"]++
	} else {
		st.Verdicts["pass"]++
	}
	if s.PausedUntil != nil && time.Now().Before(*s.PausedUntil) {
		// 暂停期：断签冻结——回来那天接着算（不施压）
	} else if st.StreakLast != today {
		yest := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
		if st.StreakLast == yest {
			st.StreakCount++
		} else {
			st.StreakCount = 1
		}
		st.StreakLast = today
	}
	if s.StepStarts == nil {
		s.StepStarts = map[string]time.Time{}
	}
	if t0, ok := s.StepStarts[nodeId]; ok {
		mins := int(time.Since(t0).Minutes())
		if mins >= 1 && mins <= 180 {
			st.StepMins = append(st.StepMins, mins)
			if len(st.StepMins) > 50 {
				st.StepMins = st.StepMins[len(st.StepMins)-50:]
			}
		}
		delete(s.StepStarts, nodeId)
	}
	// 今日步数：判定=完成了一个「讲」步
	ds := st.DaySteps[today]
	ds[0]++
	ds[1] = 3
	st.DaySteps[today] = ds
	s.saveState()
}

// recordStepOpen 步打开埋点（耗时起点）。
func (s *Server) recordStepOpen(nodeId string) {
	if s.StepStarts == nil {
		s.StepStarts = map[string]time.Time{}
	}
	s.StepStarts[nodeId] = time.Now()
}

func (s *Server) saveTrophies() {
	s.save("trophies.json", s.Trophies)
}

// hatchMark 把孵化结果写进孵化池条目（miss/tier 由后端对照已收尾清单判定）。
func hatchMark(s *Server, rd *Reading, m tutor.ShadowMeta, impact string) {
	learned := map[string]bool{}
	for _, p := range s.Ledger.List() {
		if p.Depth >= 1 { // 收尾及以上才算已掌握
			learned[p.Title] = true
		}
	}
	for i := range m.Req {
		m.Req[i].Miss = !learned[m.Req[i].N]
	}
	m.Tier = tutor.TierOf(m.Req)
	rd.Shadow = &m
	rd.Impact = impact
}

// hatchBacklog 孵化存量：已导读但缺影子元数据的条目（惰性补齐，一次一条防烧钱）。
func (s *Server) hatchBacklog() {
	if s.Tutor == nil || s.Tutor.Client == nil {
		return
	}
	for i := range s.Readings {
		rd := &s.Readings[i]
		if rd.Brief == "" || rd.Shadow != nil || githubRepoFromURL(rd.URL) == "" {
			continue
		}
		if m, impact, err := s.Tutor.Hatch(hatchCtx(s), rd.Brief); err == nil {
			hatchMark(s, rd, m, impact)
			s.save("readings.json", s.Readings)
			s.saveState()
		}
		return // 每次调用只补一条——大厅多次刷新自然补完
	}
}

// exportResume P0-4 作品 → 能力条目 → 简历：按技能聚合纪念卡，每条挂可点开的证据链接；
// 本地生成 resume.md 并用资源管理器打开（不做云端）。
func (s *Server) exportResume() (string, error) {
	if len(s.Trophies) == 0 {
		return "", fmt.Errorf("还没有作品——先通关一个影子副本")
	}
	skill := map[string][]*Trophy{}
	for _, tr := range s.Trophies {
		for _, n := range tr.Nodes {
			skill[n] = append(skill[n], tr)
		}
	}
	var b strings.Builder
	b.WriteString("# 我的实践简历\n\n")
	b.WriteString("> 由前哨 2.0 生成于 " + time.Now().Format("2006-01-02") + " —— 每一条都有可点开的证据。\n\n")
	b.WriteString("## 能力（按技能聚合）\n\n")
	skills := make([]string, 0, len(skill))
	for k := range skill {
		skills = append(skills, k)
	}
	sort.Strings(skills)
	for _, sk := range skills {
		b.WriteString("### " + sk + "\n")
		for _, tr := range skill[sk] {
			b.WriteString("- **" + tr.Name + "**：复现于 " + tr.Date + "\n")
			b.WriteString("  - " + tr.Impact + "\n")
			if tr.UserEvidence != "" {
				b.WriteString("  - 证据（我的产物）：" + tr.UserEvidence + "\n")
			} else {
				b.WriteString("  - 证据：待补——把你的仓库/文件路径写进这张纪念卡，简历才有力气\n")
			}
			b.WriteString("  - 参考来源（被复现的项目）：" + tr.Repo + "\n")
		}
	}
	b.WriteString("\n## 作品清单\n\n")
	for _, tr := range s.Trophies {
		b.WriteString("- " + tr.Name + "（" + tr.Repo + "）· " + fmt.Sprint(tr.Stages) + " 关 · 复现于 " + tr.Date + "\n")
	}
	path := filepath.Join(s.Dir, "resume.md")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// extractUserEvidence 从副本对话里找用户自己的产物：本地文件路径 / 他的仓库链接 / commit。
// 找不到就留空——简历里挂「待补」，绝不用被复现的原仓库冒充证据。
func extractUserEvidence(d *tutor.Dungeon) string {
	for i := len(d.Messages) - 1; i >= 0; i-- {
		if d.Messages[i].Role != "user" {
			continue
		}
		for _, p := range strings.Fields(d.Messages[i].Content) {
			p = strings.Trim(p, "\t\r\n,，。;；")
			if strings.HasPrefix(p, "http") && strings.Contains(p, "github.com") && !strings.Contains(p, d.Repo) {
				return p // 他自己的仓库/commit 链接
			}
			if (strings.Contains(p, `:\`) || strings.HasPrefix(p, "/")) && strings.ContainsAny(p, ".py.go.md.js") {
				return p // 本地产物路径
			}
		}
	}
	return ""
}

// mintTrophy 通关揭晓时铸造纪念卡（一锤揭晓的揭晓 payload 数据源）。
func (s *Server) mintTrophy(d *tutor.Dungeon) *Trophy {
	nodes := []string{}
	for _, r := range d.Req {
		nodes = append(nodes, r.N)
	}
	now := time.Now()
	tr := &Trophy{
		Name: d.Project, Repo: d.Repo, Impact: d.Impact, Nodes: nodes,
		Stages: len(d.Stages),
		Date:   now.Format("2006-01-02"), Short: now.Format("01·02"),
		At: now, DungeonID: d.ID,
		UserEvidence: extractUserEvidence(d),
	}
	if tr.Impact == "" { // 旧数据兜底：孵化叙事缺失时给朴素回顾
		tr.Impact = "一个真实开源项目的核心复现——它只用了你已经学会的东西。"
	}
	s.Trophies = append([]*Trophy{tr}, s.Trophies...) // 新卡置顶
	// P0-4 作品 → 能力条目：进履历（「做过」级），证据=仓库链接——每条都可点开验证
	s.Ledger.Record("trophy:"+d.ID, "作品复现："+tr.Name, evidence.Done, time.Now(), "trophy:"+tr.Repo, tr.Impact)
	s.save("ledger.json", s.Ledger)
	return tr
}

// activeDungeon 当前进行中的副本（同时只跑一个——专注）。
func (s *Server) activeDungeon() *tutor.Dungeon {
	for _, d := range s.Duns {
		if d.Status == "active" {
			return d
		}
	}
	return nil
}

func (s *Server) hasOpenObservation() bool {
	for _, o := range s.Obs {
		if o.Status == "open" {
			return true
		}
	}
	return false
}

// buildIterateTools 自迭代工具组：白名单读写 + 固定的测试/构建/看 diff（无任意命令执行）。
func (s *Server) buildIterateTools() []tutor.Tool {
	readTool := tutor.Tool{
		Name: "read_file", Description: "读仓库源码文件（白名单：internal/ 与根 .go；数据目录与 cmd/ 禁读——密钥与入口不进模型）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}}, "required": []string{"path"}},
		Fn: func(args json.RawMessage) (string, error) {
			var req struct{ Path string }
			_ = json.Unmarshal(args, &req)
			p, err := iterate.SafePath(s.RepoDir, req.Path)
			if err != nil {
				return "", err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			if len(b) > 48<<10 {
				b = b[:48<<10]
			}
			return string(b), nil
		},
	}
	listTool := tutor.Tool{
		Name: "list_dir", Description: "列目录（白名单同上）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}}, "required": []string{"path"}},
		Fn: func(args json.RawMessage) (string, error) {
			var req struct{ Path string }
			_ = json.Unmarshal(args, &req)
			p, err := iterate.SafePath(s.RepoDir, req.Path)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(p)
			if err != nil {
				return "", err
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			return strings.Join(names, "\n"), nil
		},
	}
	writeTool := tutor.Tool{
		Name: "write_file", Description: "写仓库源码文件（白名单：internal/ 与根 .go；单文件 ≤128KB；这是你唯一的写入口）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}},
			"required": []string{"path", "content"}},
		Fn: func(args json.RawMessage) (string, error) {
			var req struct{ Path, Content string }
			_ = json.Unmarshal(args, &req)
			p, err := iterate.SafePath(s.RepoDir, req.Path)
			if err != nil {
				return "", err
			}
			if len(req.Content) > iterate.MaxFileBytes {
				return "", fmt.Errorf("内容超过 %dKB 上限——需要大改请放弃并在报告里说明", iterate.MaxFileBytes>>10)
			}
			if err := os.WriteFile(p, []byte(req.Content), 0o644); err != nil {
				return "", err
			}
			return "已写入 " + req.Path, nil
		},
	}
	testTool := tutor.Tool{
		Name: "run_tests", Description: "跑全量 go test ./...（改完必须跑，红了修到绿）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
		Fn: func(args json.RawMessage) (string, error) {
			out, err := iterate.RunTests(s.RepoDir)
			if err != nil {
				return "", err
			}
			return "测试全绿\n" + tailLines(out, 10), nil
		},
	}
	buildTool := tutor.Tool{
		Name: "go_build", Description: "构建到 outpost2-next.exe（不碰运行中的服务）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
		Fn: func(args json.RawMessage) (string, error) {
			if _, err := iterate.Build(s.RepoDir); err != nil {
				return "", err
			}
			return "构建成功", nil
		},
	}
	diffTool := tutor.Tool{
		Name: "git_diff", Description: "看自己改动的内容（git diff internal/）",
		Schema: map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
		Fn: func(args json.RawMessage) (string, error) {
			out, err := runIn(s.RepoDir, "git", "diff", "--stat", "internal/")
			if err != nil {
				return "", err
			}
			return out, nil
		},
	}
	return []tutor.Tool{readTool, listTool, writeTool, testTool, buildTool, diffTool}
}

// RunIterate 低峰自迭代：取最旧的一条 open 观察 → 助教自主实现 → 确定性闸门（测试+构建，不过即还原）→ 提交。
// 一次只做一条；失败不重试（报告 failed，观察留给人工评估）。
func (s *Server) RunIterate() error {
	s.iterMu.Lock()
	defer s.iterMu.Unlock()
	if s.iterating {
		return fmt.Errorf("迭代进行中")
	}
	var obs *tutor.Observation
	for i := range s.Obs {
		if s.Obs[i].Status == "open" {
			obs = &s.Obs[i]
			break
		}
	}
	if obs == nil {
		return fmt.Errorf("观察清单为空——等每日反思")
	}
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	target := *obs
	s.iterating = true
	go func() {
		report, err := s.doIterate(target)
		s.iterMu.Lock()
		s.iterating = false
		// 回写 backlog 状态
		for i := range s.Obs {
			if s.Obs[i].ID == target.ID {
				if err != nil {
					s.Obs[i].Status = "failed"
					s.Obs[i].Report = "失败: " + err.Error()
				} else {
					s.Obs[i].Status = "done"
					s.Obs[i].Report = report
				}
			}
		}
		s.saveBacklog()
		if s.FetchConf != nil {
			s.FetchConf.IterRun = time.Now().Format("2006-01-02")
			s.FetchConf.IterReport = report
			if err != nil {
				s.FetchConf.IterReport = "迭代失败: " + err.Error()
			}
			s.saveFetchConf()
		}
		s.iterMu.Unlock()
	}()
	return nil
}

func (s *Server) doIterate(obs tutor.Observation) (string, error) {
	tools := s.buildIterateTools()
	report, _, err := s.Tutor.SelfIterate(obs, tools)
	if err != nil {
		iterate.Restore(s.RepoDir)
		return "", err
	}
	// 确定性闸门（不信任 AI）：全量测试 + 构建，任一失败 → 还原
	if _, err := iterate.RunTests(s.RepoDir); err != nil {
		iterate.Restore(s.RepoDir)
		return "", err
	}
	if _, err := iterate.Build(s.RepoDir); err != nil {
		iterate.Restore(s.RepoDir)
		return "", err
	}
	msg := fmt.Sprintf("iter(%s): %s —— 助教低峰自迭代\n\n观察：%s\n缺口：%s\n\nCo-authored-by: ZCode (GLM) <zcode@z.ai>", obs.Priority, obs.Suggestion, obs.Observation, obs.Gap)
	if err := iterate.Commit(s.RepoDir, msg); err != nil {
		iterate.Restore(s.RepoDir)
		return "", err
	}
	return report + "\n（闸门全绿，已提交——重启换版后生效）", nil
}

func (s *Server) curateContext() string {
	var b strings.Builder
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		if len(cur.Subjects) > 0 {
			fmt.Fprintf(&b, "- 当前在学科目：%s\n", strings.Join(cur.Subjects, "、"))
		}
		var done []string
		for _, l := range cur.Lessons {
			if l.Status == "done" {
				done = append(done, l.Title)
			}
		}
		if len(done) > 0 {
			fmt.Fprintf(&b, "- 已收尾的知识点：%s\n", strings.Join(done, "、"))
		}
	}
	var titles []string
	for _, d := range reflib.LoadIndex(s.RefDir).Docs {
		titles = append(titles, d.Title)
	}
	if len(titles) > 0 {
		fmt.Fprintf(&b, "- 参考库已有（勿重复入库）：%s\n", strings.Join(titles, "；"))
	}
	if b.Len() == 0 {
		return "（新用户：还没有在学科目与库存——从 Trending 通用好东西里挑）"
	}
	return b.String()
}

// beginStep/endStep 每日例行步骤互斥（进货/巡检/反思串行化的基础）。
func (s *Server) beginStep() bool {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	if s.fetchRunning {
		return false
	}
	s.fetchRunning = true
	return true
}

func (s *Server) endStep() {
	s.fetchMu.Lock()
	s.fetchRunning = false
	s.fetchMu.Unlock()
}

// curateCore 进货同步核（日期已做则跳过；返回 nil=跳过或成功）。
func (s *Server) curateCore() error {
	today := time.Now().Format("2006-01-02")
	if s.FetchConf != nil && s.FetchConf.LastRun == today {
		return nil // 今日已进货
	}
	if !s.beginStep() {
		return nil
	}
	defer s.endStep()
	report, err := s.doCurate()
	if s.FetchConf != nil {
		s.FetchConf.LastRun = today
		if err != nil {
			s.FetchConf.LastReport = "进货失败: " + err.Error()
		} else {
			s.FetchConf.LastReport = report
			if s.Tutor != nil {
				s.Tutor.FetchNote = truncateNote(report) // 进货完成 → 下一节开场由助教主动推荐
			}
		}
	}
	s.saveFetchConf()
	return err
}

// RunCurate 触发一次内容进货（异步；同一时刻只跑一份）。
func (s *Server) RunCurate() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	if len(s.buildFetchTools()) == 0 {
		return fmt.Errorf("抓取工具未就绪")
	}
	if s.FetchConf != nil && s.FetchConf.LastRun == time.Now().Format("2006-01-02") {
		return fmt.Errorf("今天已经进过货了")
	}
	go s.curateCore()
	return nil
}

func (s *Server) doCurate() (string, error) {
	return s.Tutor.CurateOnce(s.curateContext(), s.buildFetchTools())
}

// StartFetchLoop 每日例行调度：启动即查一次（补跑），此后每 15 分钟一查。
// 时刻语义是 >=（跨过配置时刻后只要当天没跑就补跑——机器关机跳过不再静默丢）。
func (s *Server) StartFetchLoop() {
	go func() {
		for i := 0; ; i++ {
			if i > 0 {
				time.Sleep(15 * time.Minute)
			}
			if s.FetchConf == nil {
				continue
			}
			now := time.Now()
			today := now.Format("2006-01-02")
			// 每日例行流水（进货→巡检→反思串行）：任一步当天没做且已到/过时刻 → 整条流水跑一遍（核内幂等）
			stepsDone := s.FetchConf.LastRun == today && s.FetchConf.NoteRun == today && s.FetchConf.ReflectRun == today
			if !stepsDone && now.Hour() >= s.FetchConf.Hour {
				s.RunDailyRoutine()
			}
			// 低峰自迭代：独立时刻（默认凌晨 3 点，可关），观察清单里有 open 才动手
			if s.FetchConf.IterEnabled && now.Hour() >= s.iterHour() && s.FetchConf.IterRun != today && s.hasOpenObservation() {
				_ = s.RunIterate()
			}
		}
	}()
}

// iterHour 迭代时刻（未配置=默认凌晨 3 点）。
func (s *Server) iterHour() int {
	if s.FetchConf.IterHour >= 0 && s.FetchConf.IterHour <= 23 {
		return s.FetchConf.IterHour
	}
	return 3
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
	if !s.ExpBandit {
		return "standard" // 实验默认关：固定标准节奏（确定性）
	}
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

// dueCards FSRS 驱动：到期知识点的复习卡（评审 P1-5：替换 reviews.json 占位）。
func (s *Server) dueCards() []scheduler.ReviewCard {
	now := time.Now()
	titles := map[string]string{}
	if s.Tutor != nil {
		for _, l := range s.Tutor.Curriculum().Lessons {
			titles[l.ID] = l.Title
		}
	}
	var due []scheduler.ReviewCard
	for id, c := range s.FSCards {
		if c == nil || c.Due.After(now) {
			continue
		}
		due = append(due, scheduler.ReviewCard{ID: id, Due: c.Due, Title: titles[id]})
	}
	return due
}

// reviewDuePayload /api/review/due 的载荷：到期卡 + 四档评分的下次间隔预览。
type reviewDuePayload struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	Due      time.Time         `json:"due"`
	Previews map[string]string `json:"previews"` // "1".."4" → 文案
}

// reviewDueAll 当前到期复习清单（含评分预览，按到期先后）。
func (s *Server) reviewDueAll() []reviewDuePayload {
	now := time.Now()
	titles := map[string]string{}
	if s.Tutor != nil {
		for _, l := range s.Tutor.Curriculum().Lessons {
			titles[l.ID] = l.Title
		}
	}
	out := []reviewDuePayload{}
	for id, c := range s.FSCards {
		if c == nil || c.Due.After(now) {
			continue
		}
		pv := s.FSRS.Preview(*c, now)
		previews := map[string]string{}
		for r, txt := range pv {
			previews[strconv.Itoa(int(r))] = txt
		}
		out = append(out, reviewDuePayload{ID: id, Title: titles[id], Due: c.Due, Previews: previews})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out
}

// CompleteMain 完成一片主推进：消化产出物的首个剩余动作 + 履历记 Used。
func (s *Server) CompleteMain(title string) {
	s.Done.Main = append(s.Done.Main, title)
	s.Ledger.Record("goal-"+title, title, evidence.Done, time.Now(), "goal", "")
	s.save("ledger.json", s.Ledger)
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
	if s.ExpBandit {
		s.Bandit.Settle("local", shape, reward)
	}
	s.Done.Mood = mood
	s.saveState()
}

// Handlers 装配路由。
func (s *Server) Handlers() http.Handler {
	mux := http.NewServeMux()
	// embed 静态资源随版本更新——客户端禁止缓存（否则发版后浏览器拿旧 JS，表现为「改了没生效」）
	noCache := func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		if s.Plan == nil {
			s.RefreshPlan()
		}
		noCache(w)
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
		// 孵化池影子化下发（ADR 0001）：title/repo/impact 不出服务端——前端只见影子画像
		shadowOut := []map[string]any{}
		for _, rd := range s.Readings {
			so := map[string]any{"id": rd.ID, "done": rd.Done, "brief": rd.Brief != ""}
			if githubRepoFromURL(rd.URL) != "" {
				so["project"] = true
			}
			if rd.Shadow != nil {
				so["tier"] = rd.Shadow.Tier
				so["stars"] = rd.Shadow.Stars
				so["req"] = rd.Shadow.Req
			}
			shadowOut = append(shadowOut, so)
		}
		writeJSON(w, map[string]any{
			"plan": s.Plan, "done": s.Done, "goals": s.Goals,
			"exercises": s.Exercises, "readings": shadowOut, "trophies": s.Trophies, "contextEdges": s.ContextEdges,
			"wrongHits": s.WrongHits,
			"ledger":    s.Ledger.List(), "counts": map[string]int{"known": k, "used": u, "done": d},
			"graduation": map[string]any{"state": string(g), "reason": gReason},
			"curriculum": curriculum, "today": today, "sessions": sessions,
		})
	})
	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.RefreshPlan())
	})
	mux.HandleFunc("/api/opportunities", func(w http.ResponseWriter, _ *http.Request) { // P1 去处：机会页
		writeJSON(w, map[string]any{"list": s.Opportunities})
	})
	mux.HandleFunc("/api/resume/doc", func(w http.ResponseWriter, _ *http.Request) { // 小穗二轮建议3：纯预览（GET 无副作用）
		md, err := s.exportResume()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"markdown": md})
	})
	mux.HandleFunc("/api/trophy/evidence", func(w http.ResponseWriter, r *http.Request) { // 小穗二轮问题1：证据补填
		var req struct {
			DungeonID string `json:"dungeonId"`
			Evidence  string `json:"evidence"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, tr := range s.Trophies {
			if tr.DungeonID == req.DungeonID {
				tr.UserEvidence = strings.TrimSpace(req.Evidence)
				s.saveTrophies()
				writeJSON(w, map[string]any{"ok": true})
				return
			}
		}
		http.Error(w, "纪念卡不存在", 404)
	})
	mux.HandleFunc("/api/resume/export", func(w http.ResponseWriter, r *http.Request) { // 落盘+打开（POST——避免预取弹窗）
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		path, err := s.exportResume()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if c := exec.Command("explorer", "/select,"+path); c.Run() != nil {
			_ = exec.Command("cmd", "/c", "start", path).Run()
		}
		writeJSON(w, map[string]any{"ok": true, "path": path})
	})
	mux.HandleFunc("/api/pause", func(w http.ResponseWriter, r *http.Request) { // P0-3 体面暂停
		var req struct {
			Days int `json:"days"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Days <= 0 || req.Days > 90 {
			http.Error(w, "暂停天数限 1-90", 400)
			return
		}
		t := time.Now().AddDate(0, 0, req.Days)
		s.PausedUntil = &t
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "pausedUntil": t.Format("2006-01-02")})
	})
	// 小穗二轮问题2：旧 /api/resume（恢复学习）与简历命名撞车且 GET 带副作用——拆为 /api/pause/end（POST）
	mux.HandleFunc("/api/pause/end", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		s.PausedUntil = nil
		s.saveState()
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/config/exp", func(w http.ResponseWriter, r *http.Request) { // 小穗收敛轮④：实验开关
		var req struct {
			Elo    *bool `json:"elo"`
			Bandit *bool `json:"bandit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Elo != nil {
			s.ExpElo = *req.Elo
		}
		if req.Bandit != nil {
			s.ExpBandit = *req.Bandit
		}
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "expElo": s.ExpElo, "expBandit": s.ExpBandit})
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
			ID     string `json:"id"`
			Rating int    `json:"rating"` // 1=忘了 2=想不起来 3=想起来 4=轻松
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		card, ok := s.FSCards[req.ID]
		if !ok || card == nil {
			http.Error(w, "复习卡不存在", 404)
			return
		}
		if req.Rating < 1 || req.Rating > 4 {
			http.Error(w, "rating 取值 1-4", 400)
			return
		}
		next, _ := s.FSRS.Review(*card, fsrs.Rating(req.Rating), time.Now(), 0)
		s.FSCards[req.ID] = &next
		s.Done.Reviews = append(s.Done.Reviews, req.ID)
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "nextDue": next.Due.Format("2006-01-02 15:04")})
	})
	mux.HandleFunc("/api/review/due", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"cards": s.reviewDueAll()})
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
		if isNew { // ⑧埋点：步开始（耗时统计起点）
			s.recordStepOpen(lesson.ID)
		}
		if s.LastStop == nil || s.LastStop.NodeID != lesson.ID {
			s.LastStop = &LastStop{NodeID: lesson.ID, Title: lesson.Title, Step: "讲到一半", At: time.Now()}
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
			// FSRS 建卡（评审 P1-5）：收尾即入遗忘防御——到期由复习闭环召回
			if s.FSCards == nil {
				s.FSCards = map[string]*fsrs.Card{}
			}
			if _, ok := s.FSCards[lesson.ID]; !ok {
				c := fsrs.NewCard(time.Now())
				s.FSCards[lesson.ID] = &c
			}
			s.Tutor.MarkLessonDone(lesson.ID)
			s.save("ledger.json", s.Ledger)
			verdict = v.Kind
		}
		s.recordVerdict(verdict, lesson.ID)
		s.LastStop = &LastStop{NodeID: lesson.ID, Title: lesson.Title,
			Step: "判定结束", At: time.Now()}
		writeJSON(w, map[string]any{"reply": v.Reply, "verdict": verdict})
	})
	mux.HandleFunc("/api/exercise/generate", func(w http.ResponseWriter, _ *http.Request) {
		today := time.Now().Format("2006-01-02")
		if len(s.Exercises) > 0 && s.ExerDate == today { // 幂等：今天的题已出过
			writeJSON(w, map[string]any{"exercises": s.Exercises})
			return
		}
		if len(s.Exercises) > 0 { // 跨日作废重出（错题已在 WrongHits，不丢学习信号）
			s.Exercises = nil
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
		// 策略层：实验开=Thompson 采样选题（利用+探索）；默认关=按收尾顺序取前 3（确定性）
		picked := arms
		if len(picked) > 3 {
			picked = picked[:3]
		}
		if s.ExpBandit {
			picked = s.Bandit.PickN("practice", arms, 3)
		}
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
			if s.ExpElo {
				diffNote = fmt.Sprintf("\n\n学习者当前能力：Elo≈%.0f。所有题面难度必须落在 Elo %.0f–%.0f 区间（对应预期正确率约 50%%–85%%），%s：不出明显超纲的题，也不出送分题。", s.Ability.Rating("practice"), lo, hi, tone)
			}
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
		s.ExerDate = today
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
			if first && s.Ability != nil && s.ExpElo {
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
				if s.ExpBandit {
					s.Bandit.Settle("practice", s.Exercises[i].LessonID, rw)
				}
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
				// 知识点升级（评审 P1-4）：核心知识点随练习通过升「用过」——履历深度不再停在「知道」
				if lid := s.Exercises[i].LessonID; lid != "" && s.Tutor != nil {
					for _, l := range s.Tutor.Curriculum().Lessons {
						if l.ID == lid {
							s.Ledger.Record(lid, l.Title, evidence.Used, time.Now(), "practice", s.Exercises[i].ID)
							break
						}
					}
				}
				s.save("ledger.json", s.Ledger)
			}
			s.saveState()
			if done {
				st := s.statsEnsure()
				today := time.Now().Format("2006-01-02")
				ds := st.DaySteps[today]
				ds[0]++
				ds[1] = 3
				st.DaySteps[today] = ds
				s.saveState()
			}
			writeJSON(w, map[string]any{"reply": reply, "verdict": done})
			return
		}
		http.Error(w, "练习不存在", 404)
	})
	// 助教导读：深入 GitHub 项目源码（browse→fetch 多文件）→ 结构化消化（用户 2026-10-26 指令）
	// ---------- 副本学习（用户 2026-10-26 设计：用已收尾知识复现真实项目，通关揭晓恍然大悟） ----------
	mux.HandleFunc("/api/dungeon/start", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ReadingID string `json:"readingId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Tutor == nil {
			http.Error(w, "LLM 未配置", 400)
			return
		}
		if s.activeDungeon() != nil {
			http.Error(w, "已有进行中的副本——一次只打一个", 400)
			return
		}
		for i := range s.Readings {
			if s.Readings[i].ID != req.ReadingID {
				continue
			}
			rd := &s.Readings[i]
			repo := githubRepoFromURL(rd.URL)
			if repo == "" {
				http.Error(w, "该条目不是 GitHub 项目", 400)
				return
			}
			if rd.Brief == "" || rd.Shadow == nil {
				http.Error(w, "先孵化这个影子（助教导读+定档），再开门", 400)
				return
			}
			d, missing, err := s.Tutor.DungeonStart(briefContext(s, rd), repo, rd.Brief)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if d == nil {
				http.Error(w, missing, 400) // 重合度不足——缺什么如实告知
				return
			}
			d.ID = fmt.Sprintf("dn-%s-%d", time.Now().Format("0102"), len(s.Duns)+1)
			d.ReadingID = rd.ID
			d.Impact = rd.Impact
			if rd.Shadow != nil { // 影子画像开门即冻结（tier/stars/req 不再随学习漂移）
				d.ShadowMeta = *rd.Shadow
			}
			s.Duns = append(s.Duns, d)
			s.saveDuns()
			writeJSON(w, map[string]any{"dungeon": d})
			return
		}
		http.Error(w, "阅读条目不存在", 404)
	})
	mux.HandleFunc("/api/dungeon/send", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		d := s.activeDungeon()
		if d == nil || d.ID != req.ID {
			http.Error(w, "副本不在进行中", 400)
			return
		}
		reply, err := s.Tutor.DungeonSend(d, req.Text, s.buildFetchTools())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var reveal map[string]any
		if strings.Contains(reply, "[CLEAR]") { // 一锤揭晓：生成纪念卡 + 揭晓 payload（ADR 0001：此刻才下发项目身份）
			tr := s.mintTrophy(d)
			reveal = map[string]any{
				"name": tr.Name, "repo": tr.Repo, "impact": tr.Impact,
				"nodes": tr.Nodes, "date": tr.Date, "short": tr.Short,
			}
			s.saveTrophies()
		}
		s.saveDuns()
		writeJSON(w, map[string]any{"reply": reply, "dungeon": d, "reveal": reveal})
	})
	mux.HandleFunc("/api/dungeon/list", func(w http.ResponseWriter, _ *http.Request) {
		// 列表脱敏：active 副本不回传 project 名（Sealed 机制）
		// ADR 0001：Sealed 副本只下发影子画像（repo/project/impact 全部不出现）
		s.hatchOnce.Do(func() { // 存量补齐：当日一次、后台跑（LLM 慢不阻塞列表）
			if time.Now().Format("2006-01-02") != s.hatchDay.Load() {
				s.hatchDay.Store(time.Now().Format("2006-01-02"))
				go s.hatchBacklog()
			}
		})
		type dunView struct {
			ID        string          `json:"id"`
			ReadingID string          `json:"readingId"`
			Title     string          `json:"title"`
			Status    string          `json:"status"`
			Stages    []tutor.DStage  `json:"stages"`
			Cur       int             `json:"cur"`
			Messages  []llm.Message   `json:"messages,omitempty"`
			Tier      int             `json:"tier"`
			Stars     int             `json:"stars"`
			Req       []tutor.ReqNode `json:"req"`
		}
		out := []dunView{}
		for _, d := range s.Duns {
			v := dunView{ID: d.ID, ReadingID: d.ReadingID, Title: d.Project, Status: d.Status, Stages: d.Stages, Cur: d.Cur,
				Messages: d.Messages, Tier: d.Tier, Stars: d.Stars, Req: d.Req}
			if d.Sealed() {
				v.Title = "？？？"
				v.Req = d.Req // 影子画像保持可见（需求知识点+难度是候选期的承诺）
			}
			out = append(out, v)
		}
		writeJSON(w, map[string]any{"dungeons": out})
	})
	mux.HandleFunc("/api/dungeon/reopen", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.activeDungeon() != nil {
			http.Error(w, "已有进行中的副本——一次只打一个", 400)
			return
		}
		for _, d := range s.Duns {
			if d.ID == req.ID && d.Status == "abandoned" {
				d.Status = "active"
				s.saveDuns()
				writeJSON(w, map[string]any{"ok": true, "dungeon": d})
				return
			}
		}
		http.Error(w, "副本不存在或非放弃态", 404)
	})
	mux.HandleFunc("/api/dungeon/abandon", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, d := range s.Duns {
			if d.ID == req.ID && d.Status == "active" {
				d.Status = "abandoned"
				s.saveDuns()
				writeJSON(w, map[string]any{"ok": true})
				return
			}
		}
		http.Error(w, "副本不存在或已结束", 404)
	})

	mux.HandleFunc("/api/reading/brief", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Tutor == nil {
			http.Error(w, "LLM 未配置", 400)
			return
		}
		for i := range s.Readings {
			if s.Readings[i].ID != req.ID {
				continue
			}
			rd := &s.Readings[i]
			// 用户点击即重新生成（覆盖旧导读——网络失败/换一次生成的重试入口）
			repo := githubRepoFromURL(rd.URL)
			if repo == "" {
				http.Error(w, "该条目不是 GitHub 项目，无法深入源码导读", 400)
				return
			}
			brief, err := s.Tutor.ProjectBrief(briefContext(s, rd), repo, s.buildReadTools())
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			rd.Brief = brief
			// 孵化影子（导读完成后顺带定档——用户点一次得到完整影子）
			if m, impact, herr := s.Tutor.Hatch(hatchCtx(s), brief); herr == nil {
				hatchMark(s, rd, m, impact)
			}
			s.save("readings.json", s.Readings) // 教务覆盖式更新时 Brief 以 readings.json 为准
			s.saveState()
			writeJSON(w, map[string]any{"brief": brief, "shadow": rd.Shadow})
			return
		}
		http.Error(w, "阅读条目不存在", 404)
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
	// 版本自检：前端定期比对，服务重启（=发版）后自动 reload——桌面驻留窗口永不过期
	startedAt := time.Now()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"started": startedAt.Unix()})
	})
	mux.HandleFunc("/api/fetch", func(w http.ResponseWriter, _ *http.Request) {
		s.fetchMu.Lock()
		running := s.fetchRunning
		s.fetchMu.Unlock()
		resp := map[string]any{"running": running, "toolsReady": len(s.buildFetchTools()) > 0}
		if s.FetchConf != nil {
			resp["proxy"], resp["hour"], resp["lastRun"], resp["lastReport"] =
				s.FetchConf.Proxy, s.FetchConf.Hour, s.FetchConf.LastRun, s.FetchConf.LastReport
		}
		// 今日进货统计（方向卡「今日进货」行的数据源）
		today := time.Now().Format("2006-01-02")
		var titles []string
		for _, d := range reflib.LoadIndex(s.RefDir).Docs {
			if strings.HasPrefix(d.ID, "gh-") && d.Date == today && strings.TrimSpace(d.Title) != "" {
				titles = append(titles, d.Title)
			}
		}
		resp["todayItems"], resp["todayTitles"] = len(titles), titles
		resp["failed"] = s.FetchConf != nil && strings.HasPrefix(s.FetchConf.LastReport, "进货失败")
		writeJSON(w, resp)
	})
	mux.HandleFunc("/api/fetch/config", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Proxy       string `json:"proxy"`
			Hour        *int   `json:"hour"`
			IterEnabled *bool  `json:"iterEnabled"`
			IterHour    *int   `json:"iterHour"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.FetchConf == nil {
			s.FetchConf = &FetchConfig{Hour: 7, IterHour: 3}
		}
		s.FetchConf.Proxy = strings.TrimSpace(req.Proxy)
		if req.Hour != nil && *req.Hour >= 0 && *req.Hour <= 23 {
			s.FetchConf.Hour = *req.Hour
		}
		if req.IterEnabled != nil {
			s.FetchConf.IterEnabled = *req.IterEnabled
		}
		if req.IterHour != nil && *req.IterHour >= 0 && *req.IterHour <= 23 {
			s.FetchConf.IterHour = *req.IterHour
		}
		s.Gh = ghfetch.New(s.FetchConf.Proxy)
		s.saveFetchConf()
		if s.Tutor != nil { // 工具组随新客户端重建
			s.Tutor.Tools = s.buildFetchTools()
		}
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/notes", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{"list": notes.List(s.NotesDir)}
		if s.FetchConf != nil {
			resp["noteRun"], resp["noteReport"] = s.FetchConf.NoteRun, s.FetchConf.NoteReport
		}
		writeJSON(w, resp)
	})
	mux.HandleFunc("/api/notes/review", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.RunNoteReview(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "助教正在巡检笔记，报告稍后出现在这里"})
	})
	mux.HandleFunc("/api/backlog", func(w http.ResponseWriter, _ *http.Request) {
		s.iterMu.Lock()
		obs := s.Obs
		iterating := s.iterating
		s.iterMu.Unlock()
		var iterConf map[string]any
		if s.FetchConf != nil {
			iterConf = map[string]any{"enabled": s.FetchConf.IterEnabled, "hour": s.iterHour(),
				"lastRun": s.FetchConf.IterRun, "lastReport": s.FetchConf.IterReport}
		}
		writeJSON(w, map[string]any{"observations": obs, "iterating": iterating, "iter": iterConf})
	})
	mux.HandleFunc("/api/iterate/run", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.RunIterate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "助教开始自主迭代：实现→测试→构建→提交，报告稍后出现在这里"})
	})
	mux.HandleFunc("/api/reflect/run", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.RunReflect(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "助教开始今日反思"})
	})
	mux.HandleFunc("/api/fetch/run", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.RunCurate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "进货已开始——助教正在抓取，报告稍后出现在这里"})
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

// githubRepoFromURL 从 URL 提取 owner/repo（非 GitHub 返回空）。
func githubRepoFromURL(raw string) string {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "/")
	i := strings.Index(raw, "github.com/")
	if i < 0 {
		return ""
	}
	rest := raw[i+len("github.com/"):]
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// briefContext 导读生成的学习者上下文（贴合个人：在学科目+已收尾）。
// hatchCtx 孵化器专用上下文：只有已收尾知识点清单（req 词汇表的唯一来源，防 LLM 把条目标题当知识点）。
// 数据源=ledger（履历单一真相源），与 hatchMark 的 miss 判定同源——两处口径必须一致。
func hatchCtx(s *Server) string {
	var done []string
	for _, p := range s.Ledger.List() {
		if p.Depth >= 1 {
			done = append(done, p.Title)
		}
	}
	if len(done) == 0 {
		return "（暂无已收尾知识点——req 将全部标记为缺失，属正常状态）"
	}
	return "已收尾知识点（req 词汇表的唯一来源，逐字取用）：" + strings.Join(done, "、")
}

func briefContext(s *Server, rd *Reading) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- 阅读条目：%s（%s）\n", rd.Title, rd.URL)
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		if len(cur.Subjects) > 0 {
			fmt.Fprintf(&b, "- 在学科目：%s\n", strings.Join(cur.Subjects, "、"))
		}
		var done []string
		for _, l := range cur.Lessons {
			if l.Status == "done" {
				done = append(done, l.Title)
			}
		}
		if len(done) > 0 {
			fmt.Fprintf(&b, "- 已收尾知识点：%s\n", strings.Join(done, "、"))
		}
	}
	return b.String()
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
	s.StartFetchLoop() // 内容进货定时器（命中 fetch.json.hour 且当日未跑则触发）
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
