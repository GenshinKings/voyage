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
	"time"

	"outpost2/internal/ability"
	"outpost2/internal/bandit"
	"outpost2/internal/evidence"
	"outpost2/internal/graduation"
	"outpost2/internal/sandbox"
	"outpost2/internal/scheduler"
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

// Server 应用壳。
type Server struct {
	Dir     string               `json:"dir"`
	Goals   []Goal               `json:"goals"`
	Plan    *scheduler.TodayPlan `json:"plan,omitempty"`
	Done    Completed            `json:"done"`
	Ledger  *evidence.Ledger     `json:"ledger"`
	Bandit  *bandit.Bandit       `json:"bandit"`
	Ability *ability.Tracker     `json:"ability"`
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
	s := &Server{Dir: dir, Ledger: evidence.New(), Bandit: bandit.New(time.Now().UnixNano()), Ability: ability.New()}
	if s.load("state.json", s) {
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
		writeJSON(w, map[string]any{
			"plan": s.Plan, "done": s.Done, "goals": s.Goals,
			"ledger": s.Ledger.List(), "counts": map[string]int{"known": k, "used": u, "done": d},
			"graduation": map[string]any{"state": string(g), "reason": gReason},
		})
	})
	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, s.RefreshPlan())
	})
	mux.HandleFunc("/api/complete-main", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Title string `json:"title"` }
		_ = json.NewDecoder(r.Body).Decode(&req)
		s.CompleteMain(req.Title)
		writeJSON(w, map[string]any{"ok": true})
	})
	mux.HandleFunc("/api/complete-review", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ ID string `json:"id"` }
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
