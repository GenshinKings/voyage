// memory.go — 第七轮：记忆巩固的装配与调度（数据在 internal/memory，这里只做编排）。
//
// 触发：每日例行第四步（进货→笔记巡检→反思→巩固）+ 手动 /api/memory/consolidate。
// 失败纪律：巩固挂了只记日志与报告，绝不阻塞教学主流程（第七轮验收 5）。
package server

import (
	"fmt"
	"log"
	"strings"
	"time"

	"voyage/internal/memory"
	"voyage/internal/tutor"
)

// memEnsure 记忆库惰性装配（NewServer 已载入；测试直构 Server 时兜底）。
func (s *Server) memEnsure() *memory.Store {
	if s.Memory == nil {
		s.Memory = memory.Load(s.Dir)
	}
	return s.Memory
}

// memoryContext 巩固输入：今天有活动的教学会话（用户侧原话 + 助教带判定标记的确认句）。
func (s *Server) memoryContext() string {
	var b strings.Builder
	today := time.Now().Format("2006-01-02")
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		for _, l := range cur.Lessons {
			sess := s.Tutor.Load(l.ID)
			if sess == nil || sess.UpdatedAt.Format("2006-01-02") != today {
				continue
			}
			fmt.Fprintf(&b, "会话 session:%s@%s《%s》：\n", sess.NodeID, today, l.Title)
			for _, m := range sess.Messages {
				if m.Role == "user" {
					fmt.Fprintf(&b, "- 学习者：%s\n", truncateRunes(m.Content, 160))
				} else if m.Role == "assistant" && strings.Contains(m.Content, "[PASS") {
					fmt.Fprintf(&b, "- 助教确认：%s\n", truncateRunes(m.Content, 160))
				}
			}
		}
	}
	if b.Len() == 0 {
		return "（今天没有教学对话——只输出 NOOP，禁止编造）"
	}
	return b.String()
}

// existingMemories 巩固输入：现有 active 条目（id+text——UPDATE/NOOP 判断的依据）。
func (s *Server) existingMemories() string {
	lines := []string{}
	for i := range s.memEnsure().Entries {
		e := &s.Memory.Entries[i]
		if e.Status == memory.StatusActive {
			lines = append(lines, fmt.Sprintf("- %s：%s", e.ID, e.Text))
		}
	}
	if len(lines) == 0 {
		return "（空库）"
	}
	return strings.Join(lines, "\n")
}

// applyMemOps 逐条过校验后应用操作。校验失败/目标不存在 → 丢弃该条并计数（不允许半条记忆）。
func (s *Server) applyMemOps(ops []tutor.MemOp, now time.Time) (added, updated, deleted, noop, dropped int) {
	st := s.memEnsure()
	for _, op := range ops {
		switch strings.ToUpper(strings.TrimSpace(op.Op)) {
		case "ADD":
			e, err := st.Add(memory.Entry{
				Text: op.Text, Kind: op.Kind, Source: op.Source, Confidence: op.Confidence,
				Ts: now,
			}, now)
			if err != nil {
				dropped++
				log.Printf("记忆巩固·丢弃 ADD（%v）: %.80s", err, op.Text)
				continue
			}
			added++
			_ = e
		case "UPDATE":
			e := st.Get(op.ID)
			if e == nil || e.Status != memory.StatusActive || strings.TrimSpace(op.Text) == "" {
				dropped++
				log.Printf("记忆巩固·丢弃 UPDATE（目标 %q 不存在或文本为空）", op.ID)
				continue
			}
			e.History = append(e.History, memory.History{Text: e.Text, Ts: e.Ts, Source: e.Source})
			e.Text = strings.TrimSpace(op.Text)
			e.Ts = now
			if strings.TrimSpace(op.Source) != "" {
				e.Source = strings.TrimSpace(op.Source)
			}
			if op.Confidence > 0 && op.Confidence <= 1 {
				e.Confidence = op.Confidence
			}
			updated++
		case "DELETE":
			if st.Get(op.ID) == nil {
				dropped++
				log.Printf("记忆巩固·丢弃 DELETE（目标 %q 不存在）", op.ID)
				continue
			}
			st.Delete(op.ID)
			deleted++
		case "NOOP":
			noop++
		default:
			dropped++
			log.Printf("记忆巩固·未知操作 %q 已丢弃", op.Op)
		}
	}
	return
}

// consolidateCore 巩固同步核（当天已做则跳过；步骤互斥复用 beginStep）。
func (s *Server) consolidateCore() error {
	today := time.Now().Format("2006-01-02")
	if s.FetchConf != nil && s.FetchConf.MemRun == today {
		return nil
	}
	if !s.beginStep() {
		return nil
	}
	defer s.endStep()
	ops, err := s.Tutor.Consolidate(s.memoryContext(), s.existingMemories())
	report := ""
	if err == nil {
		added, updated, deleted, noop, dropped := s.applyMemOps(ops, time.Now())
		s.memEnsure().Save(s.Dir)
		switch {
		case added == 0:
			report = "今天没什么值得记的"
		case added < 5:
			report = fmt.Sprintf("巩固完成：新增 %d 条（不足 5 条——宁缺毋滥不凑数）、更新 %d、删除 %d、去重 %d；校验丢弃 %d", added, updated, deleted, noop, dropped)
		default:
			report = fmt.Sprintf("巩固完成：新增 %d 条、更新 %d、删除 %d、去重 %d；校验丢弃 %d", added, updated, deleted, noop, dropped)
		}
	}
	if s.FetchConf != nil {
		s.FetchConf.MemRun = today
		if err != nil {
			s.FetchConf.MemReport = "巩固失败: " + err.Error()
		} else {
			s.FetchConf.MemReport = report
		}
	}
	s.saveFetchConf()
	return err
}

// RunConsolidate 手动触发一次记忆巩固（异步）。
func (s *Server) RunConsolidate() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	go func() {
		if err := s.consolidateCore(); err != nil {
			log.Printf("记忆巩固: %v", err)
		}
	}()
	return nil
}

// truncateRunes 按字符数截断（记忆上下文的体量控制）。
func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
