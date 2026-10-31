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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voyage/fsrs"
	"voyage/internal/ability"
	"voyage/internal/bandit"
	"voyage/internal/evidence"
	"voyage/internal/exercise"
	"voyage/internal/ghfetch"
	"voyage/internal/inbox"
	"voyage/internal/llm"
	"voyage/internal/memory"
	"voyage/internal/notes"
	"voyage/internal/profile"
	"voyage/internal/reflib"
	"voyage/internal/review"
	"voyage/internal/run"
	"voyage/internal/scheduler"
	"voyage/internal/store"
	"voyage/internal/tutor"
)

//go:embed static/index.html
var indexHTML []byte

//go:embed static/copy.json
var copyJSON []byte

// copyTable 第八轮-P1-1 白话文案映射表：代码里只引用 key，工程词不出现在用户界面。
// 随 /api/state 下发（STATE.copy）；单测保证每个 key 都有值。
var copyTable = func() map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(copyJSON, &m)
	return m
}()

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
	Date       string   `json:"date"`
	Main       []string `json:"main"`    // 已完成切片（DeliverableID|actionIndex 语义由前端组装标题）
	Reviews    []string `json:"reviews"` // 已完成复习卡 id
	Extras     []string `json:"extras"`
	Mood       string   `json:"mood,omitempty"`       // 旧字段（兼容读）；新写入拆到下面两个
	TimeBudget string   `json:"timeBudget,omitempty"` // 小穗三轮问题2：日初时间预算 10/30/90
	EndMood    string   `json:"endMood,omitempty"`    // 小穗三轮问题2：日终自报 还想来/今天够了
	Shape      string   `json:"shape,omitempty"`
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
	sc       stateCacheT  // 序列化缓存 + 去抖落盘（statecache.go），每实例一份
	Dir      string       `json:"dir"`
	st       *store.Store // 本实例的存储锁与原子写，不再用包级全局
	Goals    []Goal       `json:"goals"`
	RunState *run.Run     `json:"run,omitempty"` // 「从零做开源项目」当前闯关进度
	// 第三方判定待判队列（单人本地版：判卷人=用户自己）
	Reviews *review.Manager `json:"-" `
	Inbox   *inbox.Box      `json:"-"` // 麻烦收件箱（一句话→任务卡）
	// 弃学防线（comeback.go）：上次学习日期 与 召回招呼（只打一次）
	LastLearn string               `json:"lastLearn,omitempty"`
	Comeback  string               `json:"comeback,omitempty"`
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
	// 第六轮-P0-1 引导档位：lessonID → 样例(1)/半提示(2)/提问(3)——随人不随课，/api/state 可读
	GuideLevels map[string]int `json:"guideLevels"`
	// 第六轮-P0-2 误因分类：lessonID → 误因分布；WrongNotes 记最近一条证据（开场引用用）
	WrongReasons map[string]map[string]int `json:"wrongReasons"`
	WrongNotes   map[string]string         `json:"wrongNotes"`
	// 第八轮-P0-2 错因事件流：错因不只是计数——可回放，开场检索「该科目最近 3 条」的原料
	WrongLog []WrongEvent `json:"wrongLog,omitempty"`
	// 第六轮-P1-2 取材：学习者最近一次沙箱给的本机代码目录（出题读真实函数名；前端只给路径输入时也存）
	SandboxDir string `json:"sandboxDir,omitempty"`
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
	// 小穗三轮问题3：按已学语言拉真实 good first issue（ghfetch，24h 缓存）
	LiveIssues   []Opportunity `json:"liveIssues,omitempty"`
	LiveIssuesAt time.Time     `json:"liveIssuesAt,omitempty"`
	// 最小埋点（小穗收敛轮⑧）：只本地统计，不联网
	Stats      *Stats               `json:"stats,omitempty"`
	StepStarts map[string]time.Time `json:"-"` // 步开始时间（耗时统计用，内存态）
	// 孵化器存量补齐的去重状态（进程内）
	hatchOnce sync.Once
	hatchDay  atomic.Value // string 日期
	// 模型配置口：多 Profile 热切换（key 只落本地 llm.json，0600，不入仓库）
	LLMStore        *llm.Store       `json:"-"`
	AgentsPath      string           `json:"-"` // AGENTS.md 约束文件（每次 LLM 调用热读）
	ProfilePath     string           `json:"-"` // 学习者画像 memory/profile.md（热读注入 + 方向同步时自动沉淀）
	RefDir          string           `json:"-"` // 教学参考库 kb/（教务抓取的原文，教学注入清单）
	NotesDir        string           `json:"-"` // 学习者笔记 notes/（助教每日巡检对象，只读）
	defaultNotesDir string           `json:"-"` // 默认 notes/（切换仓库后回退用）
	vaultPath       string           `json:"-"` // 用户选的笔记仓库（空=默认 notes/）
	Tutor           *tutor.Tutor     `json:"-"`
	Exer            *exercise.Engine `json:"-"`

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

	// 第七轮：长期记忆（海马快写）——独立文件 memory/memories.json，不碰 state.json
	Memory *memory.Store `json:"-"`
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
	// 第七轮：记忆巩固（随每日例行跑，当天只一次）
	MemRun    string `json:"memRun,omitempty"`    // 上次巩固日期
	MemReport string `json:"memReport,omitempty"` // 上次巩固报告
}

func (s *Server) load(path string, v any) bool {
	return s.st.Load(s.Dir, path, v)
}

func (s *Server) save(path string, v any) {
	s.st.Save(s.Dir, path, v)
}

// NewServer 装配：缺 goals 时写入种子（90 天冲刺三产出物）。
func NewServer(dir string) *Server {
	s := &Server{Dir: dir, st: store.New(), Ledger: evidence.New(), Bandit: bandit.New(time.Now().UnixNano()), Ability: ability.New(), AgentsPath: filepath.Join(dir, "AGENTS.md"), ProfilePath: profile.Ensure(dir), RefDir: reflib.Ensure(dir), NotesDir: notes.Ensure(dir)}
	s.defaultNotesDir = s.NotesDir
	s.loadVaultConf()
	s.loadFetchConf()
	s.maybeComeback(time.Now()) // 弃学防线：隔了几天没来才打一次招呼
	s.FSRS = fsrs.NewScheduler()
	// 助教改进观察 backlog + 仓库根（自迭代工作区）
	_ = s.load("backlog.json", &s.Obs)
	_ = s.load("dungeons.json", &s.Duns)
	_ = s.load("trophies.json", &s.Trophies)
	// 第七轮：长期记忆（独立文件，损坏/缺失=空库不阻塞启动）
	s.Memory = memory.Load(dir)
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
	// 小穗三轮问题1：冷启动同步时间预算进助教（当日 mood 选择在 state.json 里）
	if s.Tutor != nil && s.Done.TimeBudget != "" {
		s.Tutor.TimeBudget = s.Done.TimeBudget
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

func (s *Server) saveState() { // 小穗四轮·性能1+3：默认入口=置脏+300ms 合并落盘（见 statecache.go）
	s.saveStateSoon()
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
	client.Name = p.Name
	s.Tutor = &tutor.Tutor{Dir: s.Dir, Client: client, AgentsPath: s.AgentsPath, ProfilePath: s.ProfilePath, RefDir: s.RefDir, Tools: s.buildFetchTools(), NotesDir: s.NotesDir}
	// 自查修复（2026-10-28）：档位挂 Tutor 内存态——重启不回填会静默滚回样例档。
	// 取 GuideLevels 最大值（人曾到达的最高档）；老数据无 guideLevels 字段时 map 为 nil，遍历零次=样例档，无 panic。
	for _, lv := range s.GuideLevels {
		if lv > s.Tutor.GuideLevel {
			s.Tutor.GuideLevel = lv
		}
	}
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

// curateContext 进货员的学习者上下文（科目/进度/库里已有——防重复进货）。
// ---------- 助教自迭代（反思收集 → 低峰自主实现） ----------

// setBudget 日初时间预算写入（进 tutor prompt + state 持久化）。
func (s *Server) setBudget(b string) {
	s.Done.TimeBudget = b
	s.saveState()
	if s.Tutor != nil {
		s.Tutor.TimeBudget = b
	}
}

// Handlers 装配路由。
func (s *Server) Handlers() http.Handler {
	mux := http.NewServeMux()

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerLearnHandlers(mux)
	s.registerRunHandlers(mux)
	s.registerTeachHandlers(mux)
	s.registerReviewHandlers(mux)
	s.registerInkpalHandlers(mux)
	s.registerInkpalVaultHandlers(mux)
	s.registerVaultHandlers(mux)
	s.registerNoteIndexHandlers(mux)
	s.registerInboxHandlers(mux)

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerKBHandlers(mux)

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerShopHandlers(mux)

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerSysHandlers(mux)

	return mux
}

// Serve 启动本地服务并返回实际地址。
func Serve(addr string) (*Server, error) {
	s := NewServer(DataDirName)
	s.StartFetchLoop() // 内容进货定时器（命中 fetch.json.hour 且当日未跑则触发）
	go func() { _ = http.ListenAndServe(addr, s.Handlers()) }()
	time.Sleep(200 * time.Millisecond)
	return s, nil
}

var _ = template.HTMLEscapeString // 保留模板引用
