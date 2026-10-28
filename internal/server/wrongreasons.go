// wrongreasons.go — 第八轮-P0-2：「记住你错在哪」必须真的参与教学，而不是只落库。
//
// 三条参与路径：
//  1. 开场引用：检索该科目最近 3 条错因事件，与本节相关的点名一条（自然句）
//  2. 档位初始化：同一知识点累计错 ≥2 次 → 直接从半提示档（GuideHint）起步
//  3. 收尾预告：本节带着未消除错因收尾（partial）→ 回复带「下次会先考你这个」
package server

import (
	"fmt"
	"strings"
	"time"

	"voyage/internal/tutor"
)

// WrongEvent 一条错因事件（可回放的教学信号；分布计数之外的事件流）。
type WrongEvent struct {
	At       time.Time `json:"at"`
	LessonID string    `json:"lessonId"`
	Subject  string    `json:"subject,omitempty"`
	Reason   string    `json:"reason"`
	Evidence string    `json:"evidence,omitempty"`
}

// causeLabel 误因类别的中文短词（开场引用与预告用白话，不用工程标签）。
func causeLabel(reason string) string {
	switch reason {
	case tutor.CauseConcept:
		return "概念没懂"
	case tutor.CauseCondition:
		return "看漏条件"
	case tutor.CauseEnv:
		return "环境问题"
	case tutor.CauseSlip:
		return "手滑/拼写"
	case tutor.CauseUnfinished:
		return "没做完步骤"
	}
	return reason
}

// recordWrongCause 错因入学习者模型：分布计数（第六轮）+ 事件流（第八轮，开场检索用）。
func (s *Server) recordWrongCause(lesson *tutor.Lesson, reason, evid string) {
	if s.WrongReasons == nil {
		s.WrongReasons = map[string]map[string]int{}
	}
	if s.WrongReasons[lesson.ID] == nil {
		s.WrongReasons[lesson.ID] = map[string]int{}
	}
	s.WrongReasons[lesson.ID][reason]++
	if evid != "" {
		if s.WrongNotes == nil {
			s.WrongNotes = map[string]string{}
		}
		s.WrongNotes[lesson.ID] = evid
	}
	s.WrongLog = append(s.WrongLog, WrongEvent{At: time.Now(), LessonID: lesson.ID,
		Subject: lesson.Subject, Reason: reason, Evidence: evid})
	if len(s.WrongLog) > 100 { // 事件流封顶：只留近期
		s.WrongLog = s.WrongLog[len(s.WrongLog)-100:]
	}
	s.saveState()
}

// guideFromHistory 档位初始化（第八轮-P0-2 第 2 条）：同一知识点累计错 ≥2 次
// → 半提示档起步，不再给完整样例（全懂收尾会清错因，所以剩余分布 ≥2 就是连败信号）。
func (s *Server) guideFromHistory(lesson *tutor.Lesson) {
	total := 0
	for _, n := range s.WrongReasons[lesson.ID] {
		total += n
	}
	if total < tutor.GuideFailStreak {
		return
	}
	if s.GuideLevels == nil {
		s.GuideLevels = map[string]int{}
	}
	if s.GuideLevels[lesson.ID] < tutor.GuideHint {
		s.GuideLevels[lesson.ID] = tutor.GuideHint
	}
	if s.Tutor != nil && s.Tutor.GuideLevel < tutor.GuideHint {
		s.Tutor.GuideLevel = tutor.GuideHint // 人级运行时档位同步抬到半提示
	}
}

// subjectWrongNote 开场检索（第八轮-P0-2 第 1 条）：该科目最近 3 条错因事件里，
// 取第一条与本节相关的，排成自然句；无相关事件返回空——绝不出「无中生有的上次」。
func (s *Server) subjectWrongNote(lesson *tutor.Lesson) string {
	if lesson == nil || len(s.WrongLog) == 0 {
		return ""
	}
	var cands []WrongEvent
	for i := len(s.WrongLog) - 1; i >= 0 && len(cands) < 3; i-- {
		if s.WrongLog[i].Subject == lesson.Subject {
			cands = append(cands, s.WrongLog[i])
		}
	}
	for _, ev := range cands {
		if !wrongRelated(ev, lesson) {
			continue
		}
		note := fmt.Sprintf("上次你错在「%s」", causeLabel(ev.Reason))
		if ev.Evidence != "" {
			note += "——" + ev.Evidence
		}
		if title := s.lessonTitle(ev.LessonID); title != "" && title != lesson.Title {
			note += "（当时在学《" + title + "》）"
		}
		return note + "；这节开始前先看一眼。"
	}
	return ""
}

// wrongRelated 事件与本节是否相关：同一知识点直接算；不同知识点看证据与标题
// 是否有 ≥2 字交叠（中文关键词断言的最小可靠粒度）。
func wrongRelated(ev WrongEvent, lesson *tutor.Lesson) bool {
	if ev.LessonID == lesson.ID {
		return true
	}
	return containsOverlap(ev.Evidence, lesson.Title)
}

// containsOverlap 短串的任意 2 字窗口出现在长串里（任一串短于 2 字=不判相关）。
func containsOverlap(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	short, long := ra, rb
	if len(short) > len(long) {
		short, long = rb, ra
	}
	if len(short) < 2 {
		return false
	}
	for i := 0; i+2 <= len(short); i++ {
		if strings.Contains(string(long), string(short[i:i+2])) {
			return true
		}
	}
	return false
}

// lessonTitle 从课程目录取知识点标题（目录已换/不存在返回空）。
func (s *Server) lessonTitle(nodeID string) string {
	if s.Tutor == nil {
		return ""
	}
	for _, l := range s.Tutor.Curriculum().Lessons {
		if l.ID == nodeID {
			return l.Title
		}
	}
	return ""
}
