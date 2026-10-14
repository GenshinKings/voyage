// Package graduation — 毕业仪式状态机（宪法 §1.5/§3.4）。
//
// 目标函数的终点：某领域「学会了且想继续学」走到头时，系统明确宣告毕业、
// 把调度权交还学习者。毕业不是流失，是产品唯一的交付物。
//
// 状态机：Learning（还在学：有到期/有坑/无实做）→ Maintaining（主线完成，
// 侧链保养中，持续 ≥14 天）→ Graduated（宣告毕业，调度权交还）。
// 任何一项未清（到期卡、未修坑）都会把状态拉回 Learning——毕业不靠感觉。
package graduation

import "time"

type State string

const (
	Learning    State = "learning"
	Maintaining State = "maintaining"
	Graduated   State = "graduated"
)

// DomainStats 一个领域的毕业输入（由应用层从 FSRS/沙盒/履历汇出）。
type DomainStats struct {
	Domain        string `json:"domain"`
	DueCount      int    `json:"dueCount"`      // FSRS 到期未清数
	OpenBreaks    int    `json:"openBreaks"`    // 未修复的坑数
	DoneCount     int    `json:"doneCount"`     // 履历「做过」数
	MaintainDays  int    `json:"maintainDays"`  // 连续保养天数（队列空且已复习）
	LastGraduated string `json:"lastGraduated,omitempty"` // 已毕业则锁定（防反复）
}

// Config 毕业阈值（默认 DefaultConfig；用户可覆盖——参数可见可调，宪法纪律）。
type Config struct {
	MinDone        int `json:"minDone"`        // 「做过」至少 N 件（默认 3：能力要有实证）
	MinMaintainDay int `json:"minMaintainDay"` // 保养期至少 N 天（默认 14：两周无到期=真记住了）
}

func DefaultConfig() Config { return Config{MinDone: 3, MinMaintainDay: 14} }

// Evaluate 派生状态。已毕业的领域锁定（graduated 是单向门——毕业了就是毕业了）。
func Evaluate(s DomainStats, cfg Config, now time.Time) (State, string) {
	if s.LastGraduated != "" {
		return Graduated, "已于 " + s.LastGraduated + " 毕业——侧链保养继续，主线不再需要本系统"
	}
	if s.DoneCount < cfg.MinDone {
		return Learning, "还没做过东西（做过 " + itoa(s.DoneCount) + "/" + itoa(cfg.MinDone) + "）——能力要有实证才谈毕业"
	}
	if s.DueCount > 0 || s.OpenBreaks > 0 {
		return Learning, "还有 " + itoa(s.DueCount) + " 张到期、" + itoa(s.OpenBreaks) + " 个未修的坑——先还账"
	}
	if s.MaintainDays < cfg.MinMaintainDay {
		return Maintaining, "主线已清，保养 " + itoa(s.MaintainDays) + "/" + itoa(cfg.MinMaintainDay) + " 天——两周无到期才算真记住了"
	}
	return Graduated, "毕业 ✓ 侧链保养继续，主线你不再需要本系统"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
