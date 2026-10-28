// handlers_shop.go — 进货与机会路由：fetch/opportunities/backlog/iterate/reflect/记忆巩固（第八轮-P1-2 自 server.go 按域拆出，只搬移不改逻辑）

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"voyage/internal/ghfetch"
	"voyage/internal/iterate"
	"voyage/internal/reflib"
	"voyage/internal/tutor"
)

// registerShopHandlers 装配本域路由（Handlers 调用一次）。
func (s *Server) registerShopHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/opportunities", func(w http.ResponseWriter, r *http.Request) { // P1 去处：GET 无副作用（六轮P0-4）
		if r.Method == http.MethodPost { // 显式刷新（前端「换一批」/首访提示点击）
			s.LiveIssuesAt = time.Time{} // 置零=强制重拉
			go s.fetchLiveIssues()
			writeJSON(w, map[string]any{"ok": true, "hint": "正在后台拉取真实 issue——几秒后刷新可见"})
			return
		}
		if s.LiveIssues == nil && s.LiveIssuesAt.IsZero() { // 从未拉过：返回空+提示，不偷偷触发
			list := append([]Opportunity{}, s.Opportunities...)
			writeJSON(w, map[string]any{"list": list, "hint": "点「换一批真实 issue」拉取当前可啃的 open issue"})
			return
		}
		if time.Since(s.LiveIssuesAt) > 24*time.Hour { // 已有缓存且过期：返回旧值+后台静默更新
			s.LiveIssuesAt = time.Now()
			go s.fetchLiveIssues()
		}
		list := append([]Opportunity{}, s.LiveIssues...)
		list = append(list, s.Opportunities...)
		writeJSON(w, map[string]any{"list": list})
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
		// 第七轮：记忆巩固状态随进货面板可见
		resp["memRun"], resp["memReport"] = s.FetchConf.MemRun, s.FetchConf.MemReport
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

	mux.HandleFunc("/api/memory/consolidate", func(w http.ResponseWriter, _ *http.Request) { // 第七轮：手动巩固入口
		if err := s.RunConsolidate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "记忆巩固已开始——报告稍后出现在这里"})
	})

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

func (s *Server) loadFetchConf() {
	conf := &FetchConfig{Hour: 7}
	_ = s.load("fetch.json", conf)
	if conf.Hour < 0 || conf.Hour > 23 {
		conf.Hour = 7
	}
	s.FetchConf = conf
	s.Gh = ghfetch.New(conf.Proxy)
}

func (s *Server) saveFetchConf() {
	if s.FetchConf != nil {
		s.save("fetch.json", s.FetchConf)
	}
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
		// 第七轮：巩固放最后（前面步骤的对话今天也能被巩固进去；失败只记日志）
		if s.Tutor != nil {
			if err := s.consolidateCore(); err != nil {
				log.Printf("例行·记忆巩固: %v", err)
			}
		}
		s.fetchMu.Lock()
		s.routineRunning = false
		s.fetchMu.Unlock()
	}()
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
			// 每日例行流水（进货→巡检→反思→巩固串行）：任一步当天没做且已到/过时刻 → 整条流水跑一遍（核内幂等）
			stepsDone := s.FetchConf.LastRun == today && s.FetchConf.NoteRun == today && s.FetchConf.ReflectRun == today && s.FetchConf.MemRun == today
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
		Name: "go_build", Description: "构建到 voyage-next.exe（不碰运行中的服务）",
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

// RunReflect 触发一次教学反思（异步）。
func (s *Server) RunReflect() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	go s.reflectCore()
	return nil
}

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

// fetchLiveIssues 小穗三轮问题3：按已学语言拉真实 good first issue（缓存 24h）。
// ghfetch 走 FetchConf 代理；失败静默保留旧缓存——去处页永远有东西看，但不硬凑。
func (s *Server) fetchLiveIssues() {
	if s.Gh == nil {
		return
	}
	lang := "python"
	for _, sub := range s.Tutor.Curriculum().Subjects {
		sl := strings.ToLower(sub)
		if strings.Contains(sl, "go") && !strings.Contains(sl, "python") {
			lang = "go"
		}
	}
	issues, err := s.Gh.SearchIssues(lang, 4)
	now := time.Now()
	s.LiveIssuesAt = now
	if err != nil {
		s.saveState()
		return
	}
	out := []Opportunity{}
	for _, it := range issues {
		out = append(out, Opportunity{Type: "live-issue", Title: it.Title,
			Link: it.HTMLURL, Note: "真实 good first issue（" + it.Repo + "）——按你已学的 " + lang + " 拉，现在 open。助教可以陪你啃。"})
	}
	s.LiveIssues = out
	s.saveState()
}
