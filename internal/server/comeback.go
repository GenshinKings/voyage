package server

import (
	"fmt"
	"time"
)

// 弃学（churn）防线 —— 教学产品流失大多死在这里：
// 学习者连着几天没来，产品什么也不做。规则（docs/teach/spec.md 反模式之外）：
//
//  1. 不催、不指责、不问「为什么不来」
//  2. 给一个 5 分钟就能完成的小目标，门槛比上次更低
//  3. 只说一次，不刷屏
const comebackGapDays = 3

// touchLearn 记录「今天学过」。
func (s *Server) touchLearn() {
	today := time.Now().Format("2006-01-02")
	if s.LastLearn != today {
		s.LastLearn = today
		s.Comeback = "" // 回来了就把招呼清掉
		s.saveState()
	}
}

// maybeComeback 检查是否该给一句「回来」的招呼。
// 返回是否有新招呼（便于调用方决定要不要推送）。
func (s *Server) maybeComeback(now time.Time) bool {
	if s.LastLearn == "" {
		return false // 从没学过：属于新手引导，不走召回
	}
	last, err := time.Parse("2006-01-02", s.LastLearn)
	if err != nil {
		return false
	}
	gap := int(now.Sub(last).Hours() / 24)
	if gap < comebackGapDays {
		return false
	}
	if s.Comeback != "" {
		return false // 已经打过招呼，不重复
	}
	s.Comeback = fmt.Sprintf("你上次来是 %d 天前。今天不用补课，就一个 5 分钟的小目标：把上一节那个检查题重答一遍，答完就收工。", gap)
	s.saveState()
	return true
}
