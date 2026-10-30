// handlers_sys.go — 系统与输出路由：首页/state/计划/导出/暂停/预算/模型配置/版本/图谱/证据页（第八轮-P1-2 自 server.go 按域拆出，只搬移不改逻辑）

package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"voyage/internal/evidence"
	"voyage/internal/graduation"
	"voyage/internal/llm"
	"voyage/internal/scheduler"
	"voyage/internal/tutor"
)

// registerSysHandlers 装配本域路由（Handlers 调用一次）。
func (s *Server) registerSysHandlers(mux *http.ServeMux) {
	// embed 静态资源随版本更新——客户端禁止缓存（否则发版后浏览器拿旧 JS，表现为「改了没生效」）
	noCache := func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.RefreshPlan())
	})

	mux.HandleFunc("/api/export/all", func(w http.ResponseWriter, r *http.Request) { // 六轮P0-5：导出全部数据（zip，排除 llm.json 含 key）
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

	mux.HandleFunc("/api/resume/doc", func(w http.ResponseWriter, r *http.Request) { // 小穗二轮建议3+六轮P0：200 空态（前端不处理非预期错误）
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
		writeErr(w, 404, "纪念卡不存在")
	})

	mux.HandleFunc("/api/resume/export", func(w http.ResponseWriter, r *http.Request) { // 落盘+打开（POST——避免预取弹窗）
		if r.Method != http.MethodPost {
			writeErr(w, 405, "POST only")
			return
		}
		path, err := s.exportResume()
		if err != nil {
			s.httpErr(w, r, err, 400)
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
			writeErr(w, 400, "暂停天数限 1-90")
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
			writeErr(w, 405, "POST only")
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
		s.st.State.Lock()
		defer s.st.State.Unlock()
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
			writeErr(w, 400, "预算限 2/10/30/90/空")
			return
		}
		s.setBudget(req.Budget)
		writeJSON(w, map[string]any{"ok": true, "timeBudget": req.Budget})
	})

	mux.HandleFunc("/api/llm/profiles", func(w http.ResponseWriter, r *http.Request) {
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
			writeErr(w, 400, "name / baseUrl / model 必填")
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
			writeErr(w, 400, "key 不能为空（首次配置该模型时必填）")
			return
		}
		s.LLMStore.Profiles[req.Name] = llm.Profile{Name: req.Name, BaseURL: req.BaseURL, Key: key, Model: req.Model}
		s.LLMStore.Active = req.Name
		if err := s.LLMStore.Save(filepath.Join(s.Dir, "llm.json")); err != nil {
			s.httpErr(w, r, err, 500)
			return
		}
		s.rebuildLLM()
		writeJSON(w, map[string]any{"ok": true, "active": req.Name})
	})

	// 版本自检：前端定期比对，服务重启（=发版）后自动 reload——桌面驻留窗口永不过期
	startedAt := time.Now()
	// 第九轮-P0-1：接口错误的技术现场（前端文案分层的另一半——细节只在这里与日志）
	mux.HandleFunc("/api/diagnostics", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"diagnostics": diagSnapshot()})
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"started": startedAt.Unix()})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("/evidence.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		s.Ledger.WriteHTML(w, "学习者", time.Now())
	})

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

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
