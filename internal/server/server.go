// Package server — 前哨 2.0 应用壳：本地 Web 服务（宪法 §二「全托管学习机」的日用形态）。
//
// 用户的界面只剩两个动作：完成今天丢来的内容、看自己的履历。
// 视觉纪律（用户审美锚点）：纸白浅场、墨蓝单 ramp、粉 #FB7299 唯一点缀 ≤8%；
// 无 streak、无百分比表演、无 hover 浮起、无三色霓虹（负清单全部生效）。
package server

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sort"

	"voyage/fsrs"
	"voyage/internal/ability"
	"voyage/internal/bandit"
	"voyage/internal/evidence"
	"voyage/internal/exercise"
	"voyage/internal/ghfetch"
	"voyage/internal/graduation"
	"voyage/internal/llm"
	"voyage/internal/memory"
	"voyage/internal/notes"
	"voyage/internal/profile"
	"voyage/internal/reflib"
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

func (s *Server) load(path string, v any) bool { // 架构 P1-1：原始逻辑收敛至 internal/store
	return store.Load(s.Dir, path, v)
}

// stateMutex 保住 state 的「读-改-写」临界区（小穗五轮①：并发保存互相覆盖的根因治理）
var stateMutex = &store.StateMutex

// saveMutex 串行化全部文件落盘（state 与各数据文件共用一把=够用且简单）
var saveMutex = &store.WriteMutex

func (s *Server) save(path string, v any) { // 架构 P1-1：原子写收敛至 internal/store
	store.Save(s.Dir, path, v)
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

// saveStateNow 关键路径（verdict/trophy/pause/budget/export）立即落盘并刷缓存。
func (s *Server) saveStateNow() { s.flushState() }

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

// mistakeNote 第六章-P0-2：把此节点误因分布+最近证据排成一句话（开场引用；无记录=空）。
func (s *Server) mistakeNote(nodeID string) string {
	dist := s.WrongReasons[nodeID]
	if len(dist) == 0 {
		return ""
	}
	parts := []string{}
	for _, k := range []string{"condition", "concept", "env", "slip", "unfinished"} {
		if n := dist[k]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s×%d", causeLabel(k), n))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	note := strings.Join(parts, "、")
	if ev := s.WrongNotes[nodeID]; ev != "" {
		note += "；最近一次证据：" + ev
	}
	return note
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
	s.Done.EndMood = mood // 小穗三轮问题2：日终自报与日初时间预算分字段
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
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) { // 性能1+3：缓存+ETag/304（statecache.go）
		s.serveStateCache(w, r, func() (any, error) {
			if s.Plan == nil {
				s.RefreshPlan()
			}
			k, u, d := s.Ledger.Counts()
			g, gReason := graduation.Evaluate(graduation.DomainStats{DoneCount: d, MaintainDays: 0}, graduation.DefaultConfig(), time.Now())
			today := (*tutor.Lesson)(nil)
			curriculum := map[string]any{}
			sessions := map[string]any{} // 性能2/5：值可为 *tutor.Session（最近5节全量）或元信息 map
			if s.Tutor != nil {
				cur := s.Tutor.Curriculum()
				today = s.Tutor.TodayLesson()
				curriculum = map[string]any{"subjects": cur.Subjects, "lessons": cur.Lessons}
				// 性能2/5：只下发「最近 5 节」的全量消息（零 HTTP 快路径保留），
				// 其余节只发元信息（verdict/updatedAt/nMessages）——打开时 /api/tutor/open 按需拉。
				all := map[string]*tutor.Session{}
				for _, l := range cur.Lessons {
					if sess := s.Tutor.Load(l.ID); sess != nil {
						all[l.ID] = sess
					}
				}
				for _, p := range s.Ledger.List() {
					if _, ok := all[p.ID]; !ok {
						if sess := s.Tutor.Load(p.ID); sess != nil {
							all[p.ID] = sess
						}
					}
				}
				type se struct {
					id string
					s  *tutor.Session
				}
				arr := make([]se, 0, len(all))
				for id, sess := range all {
					arr = append(arr, se{id, sess})
				}
				sort.Slice(arr, func(i, j int) bool { return arr[i].s.UpdatedAt.After(arr[j].s.UpdatedAt) })
				for i, e := range arr {
					if i < 5 { // 最近 5 节：全量（快路径）
						sessions[e.id] = e.s
						continue
					}
					sessions[e.id] = map[string]any{"verdict": e.s.Verdict,
						"updatedAt": e.s.UpdatedAt, "title": e.s.Title,
						"nMessages": len(e.s.Messages)} // 其余：元信息（懒加载）
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
			// P0-2 时间预算真减步：10 分钟档 = 不下发 exercises/dungeons——首页固只有一步
			budgetLight := s.Done.TimeBudget == "10"
			return map[string]any{
				"plan": s.Plan, "done": s.Done, "goals": s.Goals,
				"exercises": func() any {
					if budgetLight {
						return []any{}
					}
					return s.Exercises
				}(),
				"dungeons": func() any {
					if budgetLight {
						return []any{}
					}
					return s.Duns
				}(),
				"readings": shadowOut, "trophies": s.Trophies, "contextEdges": s.ContextEdges,
				"wrongHits":    s.WrongHits,
				"guideLevels":  s.GuideLevels,
				"wrongReasons": s.WrongReasons,
				"copy":         copyTable,
				"ledger":       s.Ledger.List(), "counts": map[string]int{"known": k, "used": u, "done": d},
				"graduation": map[string]any{"state": string(g), "reason": gReason},
				"curriculum": curriculum, "today": today, "sessions": sessions,
			}, nil
		})
	})
	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.RefreshPlan())
	})
	mux.HandleFunc("/api/export/all", func(w http.ResponseWriter, _ *http.Request) { // 六轮P0-5：导出全部数据（zip，排除 llm.json 含 key）
		buf := new(bytes.Buffer)
		zw := zip.NewWriter(buf)
		entries, _ := os.ReadDir(s.Dir)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if name == "llm.json" || strings.HasSuffix(name, ".bak") || strings.HasSuffix(name, ".tmp") {
				continue // key 与历史备份不入档
			}
			b, err := os.ReadFile(filepath.Join(s.Dir, name))
			if err != nil {
				continue
			}
			f, _ := zw.Create(name)
			_, _ = f.Write(b)
		}
		_ = zw.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="voyage-data-`+time.Now().Format("2006-01-02")+`.zip"`)
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("/api/resume/doc", func(w http.ResponseWriter, _ *http.Request) { // 小穗二轮建议3+六轮P0：200 空态（前端不处理非预期错误）
		md, err := s.exportResume()
		if err != nil {
			writeJSON(w, map[string]any{"markdown": "", "hint": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"markdown": md, "hint": ""})
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
		stateMutex.Lock()
		defer stateMutex.Unlock()
		var req struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.CompleteMain(req.Title)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/mood", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Mood      string `json:"mood"`
			Completed bool   `json:"completed"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		// 小穗三轮问题2+六轮P0-3：接口拆分——数字预算走 /api/budget；本端点只做日终自报（兼容数字继续接受）
		if req.Mood == "10" || req.Mood == "30" || req.Mood == "90" {
			s.setBudget(req.Mood)
			writeJSON(w, map[string]any{"ok": true, "timeBudget": req.Mood})
			return
		}
		s.SettleMood(req.Mood, req.Completed)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/budget", func(w http.ResponseWriter, r *http.Request) { // 六轮P0-3：日初时间预算（显式端点）
		var req struct {
			Budget string `json:"budget"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Budget != "2" && req.Budget != "10" && req.Budget != "30" && req.Budget != "90" && req.Budget != "" {
			http.Error(w, "预算限 2/10/30/90/空", 400)
			return
		}
		s.setBudget(req.Budget)
		writeJSON(w, map[string]any{"ok": true, "timeBudget": req.Budget})
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
	// 版本自检：前端定期比对，服务重启（=发版）后自动 reload——桌面驻留窗口永不过期
	startedAt := time.Now()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"started": startedAt.Unix()})
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
			edges = append(edges, map[string]any{"from": parts[0], "to": parts[1], "type": "usage", "weight": n,
				"origin": "语境式练习真实调用（练习一条被记一次）"}) // 小穗四轮P0-3：诚实化 A
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
	mux.HandleFunc("/evidence.html", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.Ledger.WriteHTML(w, "学习者", time.Now())
	})
	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerLearnHandlers(mux)

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerKBHandlers(mux)

	// 第八轮-P1-2：路由按域拆文件（learn/kb/shop/sys），此处只做装配
	s.registerShopHandlers(mux)

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

// Serve 启动本地服务并返回实际地址。
func Serve(addr string) (*Server, error) {
	s := NewServer(filepath.Join(".", "outpost2-data"))
	s.StartFetchLoop() // 内容进货定时器（命中 fetch.json.hour 且当日未跑则触发）
	go func() { _ = http.ListenAndServe(addr, s.Handlers()) }()
	time.Sleep(200 * time.Millisecond)
	return s, nil
}

var _ = template.HTMLEscapeString // 保留模板引用
