// handlers_learn.go — 学习主链路路由：tutor/exercise/verdict/副本/方向/复习/沙箱（第八轮-P1-2 自 server.go 按域拆出，只搬移不改逻辑）

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"log"
	"strconv"
	"voyage/internal/evidence"

	"voyage/fsrs"
	"voyage/internal/llm"
	"voyage/internal/sandbox"
	"voyage/internal/tutor"
)

// registerLearnHandlers 装配本域路由（Handlers 调用一次）。
func (s *Server) registerLearnHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/sandbox/list", func(w http.ResponseWriter, r *http.Request) { // 六轮P0-1：沙箱入口数据（此前孤儿）
		type caseInfo struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Note  string `json:"note"`
		}
		lib := sandbox.Library()
		out := []caseInfo{}
		for _, c := range lib {
			out = append(out, caseInfo{ID: c.ID, Title: c.Title})
		}
		writeJSON(w, map[string]any{"list": out})
	})

	mux.HandleFunc("/api/complete-review", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     string `json:"id"`
			Rating int    `json:"rating"` // 1=忘了 2=想不起来 3=想起来 4=轻松
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		card, ok := s.FSCards[req.ID]
		if !ok || card == nil {
			writeErr(w, 404, "复习卡不存在")
			return
		}
		if req.Rating < 1 || req.Rating > 4 {
			writeErr(w, 400, "rating 取值 1-4")
			return
		}
		next, _ := s.FSRS.Review(*card, fsrs.Rating(req.Rating), time.Now(), 0)
		s.FSCards[req.ID] = &next
		s.Done.Reviews = append(s.Done.Reviews, req.ID)
		s.saveState()
		writeJSON(w, map[string]any{"ok": true, "nextDue": next.Due.Format("2006-01-02 15:04")})
	})

	mux.HandleFunc("/api/review/due", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"cards": s.reviewDueAll()})
	})

	mux.HandleFunc("/api/subject", func(w http.ResponseWriter, r *http.Request) { // GET=科目列表（200 语义）；POST=开课
		if r.Method == http.MethodGet {
			subs := []string{}
			if s.Tutor != nil {
				subs = s.Tutor.Curriculum().Subjects
			}
			writeJSON(w, map[string]any{"subjects": subs})
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if strings.TrimSpace(req.Name) == "" {
			writeErr(w, 400, "科目名不能为空")
			return
		}
		if s.Tutor == nil {
			s.httpErr(w, r, llmMissingErr(), 400)
			return
		}
		cur, err := s.Tutor.StartSubject(req.Name)
		if err != nil {
			s.httpErr(w, r, err, 500)
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
			writeErr(w, 404, "知识点不存在")
			return
		}
		// 第六轮-P0-2：误因回看——此前在此记录过失败的，开场第一句先点它
		s.Tutor.MistakeNote = s.mistakeNote(lesson.ID)
		if s.Tutor.MistakeNote == "" {
			// 第八轮-P0-2：本科目最近错因参与开场——「上次你错在……」，相关才提，无中生有的「上次」不出现
			s.Tutor.MistakeNote = s.subjectWrongNote(lesson)
		}
		// 第八轮-P0-2：档位初始化由历史决定——同一知识点连错 ≥2 次直接半提示档起步
		s.guideFromHistory(lesson)
		// 第七轮-P0-3：记忆引用——最多一条相关长期记忆，「你上次说……」（引用即入账）
		s.Tutor.MemoryNote = s.memoryNote(lesson)
		s.touchLearn() // 弃学防线：记下今天来过
		sess, isNew, err := s.Tutor.StartOrResume(r.Context(), *lesson)
		if err != nil {
			s.httpErr(w, r, err, 500)
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
		s.st.State.Lock()
		defer s.st.State.Unlock()
		var req struct {
			NodeID string `json:"nodeId"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lesson := findLesson(s, req.NodeID)
		if lesson == nil {
			writeErr(w, 404, "知识点不存在")
			return
		}
		// 第六轮-P1-1「首次通过」判据：Send 前快照会话判定态
		pre := s.Tutor.Load(req.NodeID)
		firstPass := pre == nil || pre.Verdict == ""
		s.touchLearn() // 弃学防线：记下今天来过
		v, err := s.Tutor.Send(r.Context(), *lesson, req.Text)
		if err != nil {
			s.httpErr(w, r, err, 500)
			return
		}
		verdict := ""
		// settlePass 收尾入账（履历+FSRS 建卡+课程 done）——延迟记触发：
		// 首次通过先追问「为什么这么写」，答上（FU-pass 或非首次）才入账；履历不撤回原则=先不记，不事后删。
		// 自查修复②（学生实测 2026-10-28）：只有最终 verdict≠pass 的会话才保留误因记录——
		// 全懂收尾时清掉本课的 wrongReasons/wrongNotes（那些是「最终答对之前」的过程记录，
		// 留着会让下次开场引用一条其实已经答对的错因）。partial 半懂有洞，误因保留有用。
		clearMistakes := func() {
			if s.WrongReasons != nil {
				delete(s.WrongReasons, lesson.ID)
			}
			if s.WrongNotes != nil {
				delete(s.WrongNotes, lesson.ID)
			}
		}
		settlePass := func(cleanMistakes bool) {
			if cleanMistakes {
				clearMistakes()
			}
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
		}
		switch {
		case v.Kind == "pass":
			if firstPass {
				// 第六轮-P1-1：首次通过必追问，追问过了才真正收尾
				s.Tutor.AskRealUse(lesson.ID)
			} else {
				settlePass(true)
			}
			verdict = "pass"
		case v.Kind == "partial":
			settlePass(false)
			verdict = "partial"
		case v.Kind == "fu-pass":
			// 真会度追问守住了——此刻才收尾入账（全懂：清过程误因）
			settlePass(true)
			verdict = "pass"
			s.recordVerdict("fuzzy-pass", lesson.ID)
		case v.Kind == "fu-fuzzy":
			// 真会度未过：不收尾（履历从未写入）、不推进更小结；会话判定已在 tutor 侧降为 partial
			s.recordVerdict("fuzzy", lesson.ID)
			verdict = "partial"
		default:
			// 第六轮-P0-2：检查题判定失败——误因分布入学习者模型（开场引用用）
			// 第八轮-P0-1：结构化通道的误因同样落库（cause/evidence 来自判定 JSON）
			reason, evid := tutor.ParseCause(v.Reply)
			if v.Channel == "structured" && v.Cause != "" {
				reason, evid = v.Cause, v.Evidence
			}
			if tutor.ValidCause(reason) {
				s.recordWrongCause(lesson, reason, evid)
			}
		}
		// 第八轮-P0-2：半懂收尾带的 [CAUSE] 也是「未消除的错因」——照记，收尾预告才有料
		if v.Kind == "partial" {
			if reason, evid := tutor.ParseCause(v.Reply); tutor.ValidCause(reason) {
				s.recordWrongCause(lesson, reason, evid)
			}
		}
		s.recordVerdict(verdict, lesson.ID)
		// 第六轮-P0-1：档位随判定推进——state 可读（验收），前端「讲」卡小字显示
		if s.GuideLevels == nil {
			s.GuideLevels = map[string]int{}
		}
		if v.Guide > 0 {
			s.GuideLevels[lesson.ID] = v.Guide
		}
		s.saveState()
		s.LastStop = &LastStop{NodeID: lesson.ID, Title: lesson.Title,
			Step: "判定结束", At: time.Now()}
		// 结构判定把「错在哪类 / 卡在哪 / 下一步做什么」一并下发——前端只说「再想想」等于没反馈
		resp := map[string]any{"reply": v.Reply, "verdict": verdict, "guide": v.Guide,
			"channel": v.Channel, "cause": v.Cause, "where": v.Where, "action": v.Action}
		if verdict == "partial" {
			// 第八轮-P0-2：收尾「下节预告」——带着未消除错因半懂收尾，明说下次先考什么
			if note := s.mistakeNote(lesson.ID); note != "" {
				resp["preview"] = "下次会先考你这个：" + note
			}
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("/api/exercise/generate", func(w http.ResponseWriter, r *http.Request) {
		today := time.Now().Format("2006-01-02")
		if len(s.Exercises) > 0 && s.ExerDate == today { // 幂等：今天的题已出过
			writeJSON(w, map[string]any{"exercises": s.Exercises})
			return
		}
		if len(s.Exercises) > 0 { // 跨日作废重出（错题已在 WrongHits，不丢学习信号）
			s.Exercises = nil
		}
		if s.Exer == nil {
			s.httpErr(w, r, llmMissingErr(), 400)
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
			writeErr(w, 400, "先把今天的课收尾——收尾过的知识点才有练习")
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
		// 小穗四轮·P0-3（诚实化 A）：这条边就是图例承诺的 usage 边——
		// 计数来源 = 语境式练习真实发生过（非手动标注），type 与 /api/graph 输出对齐。
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
		// 第六轮-P1-2 取材：优先学习者的真实仓库（沙箱记忆目录）——读不到退回通用例子
		s.Exer.GenerationMaterial = s.exerciseMaterial()
		list, err := s.Exer.Generate(r.Context(), ordered, diffNote)
		if err != nil {
			s.httpErr(w, r, err, 500)
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
		s.st.State.Lock()
		defer s.st.State.Unlock()
		var req struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Exer == nil {
			s.httpErr(w, r, llmMissingErr(), 400)
			return
		}
		for i := range s.Exercises {
			if s.Exercises[i].ID != req.ID {
				continue
			}
			reply, done, err := s.Exer.Answer(r.Context(), &s.Exercises[i], req.Text)
			if err != nil {
				s.httpErr(w, r, err, 500)
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
		writeErr(w, 404, "练习不存在")
	})

	// 助教导读：深入 GitHub 项目源码（browse→fetch 多文件）→ 结构化消化（用户 2026-10-26 指令）
	// ---------- 副本学习（用户 2026-10-26 设计：用已收尾知识复现真实项目，通关揭晓恍然大悟） ----------
	mux.HandleFunc("/api/dungeon/start", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ReadingID string `json:"readingId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Tutor == nil {
			writeErr(w, 400, "LLM 未配置")
			return
		}
		if s.activeDungeon() != nil {
			writeErr(w, 400, "已有进行中的副本——一次只打一个")
			return
		}
		for i := range s.Readings {
			if s.Readings[i].ID != req.ReadingID {
				continue
			}
			rd := &s.Readings[i]
			repo := githubRepoFromURL(rd.URL)
			if repo == "" {
				writeErr(w, 400, "该条目不是 GitHub 项目")
				return
			}
			if rd.Brief == "" || rd.Shadow == nil {
				writeErr(w, 400, "先孵化这个影子（助教导读+定档），再开门")
				return
			}
			d, missing, err := s.Tutor.DungeonStart(briefContext(s, rd), repo, rd.Brief)
			if err != nil {
				s.httpErr(w, r, err, 500)
				return
			}
			if d == nil {
				writeErr(w, 400, missing) // 重合度不足——缺什么如实告知
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
		writeErr(w, 404, "阅读条目不存在")
	})

	mux.HandleFunc("/api/dungeon/send", func(w http.ResponseWriter, r *http.Request) {
		s.st.State.Lock()
		defer s.st.State.Unlock()
		var req struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		d := s.activeDungeon()
		if d == nil || d.ID != req.ID {
			writeErr(w, 400, "副本不在进行中")
			return
		}
		reply, err := s.Tutor.DungeonSend(d, req.Text, s.buildFetchTools())
		if err != nil {
			s.httpErr(w, r, err, 500)
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

	mux.HandleFunc("/api/dungeon/list", func(w http.ResponseWriter, r *http.Request) {
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
			writeErr(w, 400, "已有进行中的副本——一次只打一个")
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
		writeErr(w, 404, "副本不存在或非放弃态")
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
		writeErr(w, 404, "副本不存在或已结束")
	})

	mux.HandleFunc("/api/reading/brief", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if s.Tutor == nil {
			writeErr(w, 400, "LLM 未配置")
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
				writeErr(w, 400, "该条目不是 GitHub 项目，无法深入源码导读")
				return
			}
			brief, err := s.Tutor.ProjectBrief(briefContext(s, rd), repo, s.buildReadTools())
			if err != nil {
				s.httpErr(w, r, err, 500)
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
		writeErr(w, 404, "阅读条目不存在")
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
			s.httpErr(w, r, err, 500)
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
			writeErr(w, 400, "提案不完整")
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

	mux.HandleFunc("/api/sandbox/open", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CaseID string `json:"caseId"`
			Dir    string `json:"dir"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lib := sandbox.Library()
		c, ok := lib[req.CaseID]
		if !ok {
			writeErr(w, 400, "未知案例")
			return
		}
		if _, err := sandbox.Create(req.Dir, c); err != nil {
			s.httpErr(w, r, err, 400)
			return
		}
		// 第六轮-P1-2：记住这个真实代码目录——练习出题时优先取材他的文件与函数名
		if d := strings.TrimSpace(req.Dir); d != "" {
			s.SandboxDir = d
			s.saveState()
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
			writeErr(w, 400, "未知案例")
			return
		}
		sess := &sandbox.Session{Case: c, Root: req.Dir}
		res, err := sess.Verify()
		if err != nil {
			s.httpErr(w, r, err, 400)
			return
		}
		if res.Passed {
			s.Ledger.Record(c.KnowledgePoint, c.Title, evidence.Used, time.Now(), "sandbox:"+c.ID, "")
			s.save("ledger.json", s.Ledger)
		}
		writeJSON(w, res)
	})

}

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

func (s *Server) saveTrophies() {
	s.save("trophies.json", s.Trophies)
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

func (s *Server) saveDuns() {
	s.save("dungeons.json", s.Duns)
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

func firstFile(c *sandbox.BreakCase) string {
	for k := range c.Files {
		return k
	}
	return "main.go"
}
