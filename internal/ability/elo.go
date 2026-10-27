// Package ability — 能力评估器 v0（宪法 §3.5 辅助件）：Elo per 技能域。
//
// 用途：给 AI 助教的「能力评估」一个可解释的数字底座（调研结论：给用户看的参数
// 用 Elo/θ，黑箱 KT 只可驱动调度）。Elo 抗冷启动（几道题收敛）且天然输出「难度带」
// ——调度层据此维持 50-90% 正确带（desirable difficulties）。
//
// 事件驱动：Answer(Domain, Difficulty, Correct) 更新该域 rating。
// 出题难度 d 与学习者 r 的期望正确率 = 1/(1+10^((d-r)/400))（标准 Elo）。
package ability

import (
	"math"
	"sort"
)

type Domain struct {
	Name   string  `json:"name"`
	Rating float64 `json:"rating"` // 初始 1200
	Games  int     `json:"games"`  // 已结算事件数（冷启动透明度）
}

type Tracker struct {
	Domains map[string]*Domain `json:"domains"`
	K       int                // 每事件最大变动（v0 固定 64：快收敛期；games>10 后降 24）
}

func New() *Tracker { return &Tracker{Domains: map[string]*Domain{}, K: 64} }

// Expected 学习者对难度 d 的期望正确率（0-1）。
func (t *Tracker) Expected(domain string, difficulty float64) float64 {
	r := t.ensure(domain).Rating
	return 1.0 / (1.0 + pow10((difficulty-r)/400))
}

// Answer 结算一次答题/修复事件：difficulty=题目 Elo，correct=结果。
// 同时上调/下调题目难度（题也走 Elo——难度校准是双向的）。
func (t *Tracker) Answer(domain string, difficulty float64, correct bool) (newRating float64) {
	dm := t.ensure(domain)
	expected := 1.0 / (1.0 + pow10((difficulty-dm.Rating)/400))
	actual := 0.0
	if correct {
		actual = 1
	}
	k := float64(t.K)
	if dm.Games > 10 {
		k = 24
	}
	dm.Rating += k * (actual - expected)
	dm.Games++
	return dm.Rating
}

// DifficultyBand 调度层用：返回该域当前的目标难度带（目标正确率 50-90% 带的两端）。
// 50% 对应难度=rating；90% 对应难度=rating-361（logistics 反解）。
func (t *Tracker) DifficultyBand(domain string) (low, high float64) {
	r := t.ensure(domain).Rating
	return r - 361, r // 90%正确≈难度-361；50%≈同难度
}

// Rating 可解释输出（给助教引用：「你的 Go 域评级 1180，约对应中级」）。
func (t *Tracker) Rating(domain string) float64 { return t.ensure(domain).Rating }

// Report 全域快照（透明度导出）。
func (t *Tracker) Report() []Domain {
	out := make([]Domain, 0, len(t.Domains))
	for _, d := range t.Domains {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rating > out[j].Rating })
	return out
}

func (t *Tracker) ensure(domain string) *Domain {
	d, ok := t.Domains[domain]
	if !ok {
		d = &Domain{Name: domain, Rating: 1200}
		t.Domains[domain] = d
	}
	return d
}

func pow10(x float64) float64 { return math.Exp(x * math.Ln10) }
