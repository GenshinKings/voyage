// Package fsrs — FSRS-5 间隔重复调度算法的教学版 Go 手写实现。
//
// 19 个默认参数与全部公式来自 open-spaced-repetition 项目（py-fsrs v5.1.3，
// FSRS-5），学术来源：
//   - KDD 2022: "A Stochastic Shortest Path Algorithm for Optimizing Spaced
//     Repetition Scheduling" (DOI: 10.1145/3534678.3539081)
//   - TKDE 2023: "Optimizing Spaced Repetition Schedule by Capturing the
//     Dynamics of Memory" (DOI: 10.1109/TKDE.2023.3251721)
//
// 与官方实现的差异（教学版，见 docs/gaps/BACKLOG.md）：
//   - fuzz 采用规格 §4 的 ±5% 固定比例（官方 FSRS-5 是分段 15%/10%/5%），
//     仅对 ≥2.5 天的间隔生效，可用 SetFuzzRand 注入确定性随机源做性质测试。
package fsrs

import "math"

// DefaultParameters 是 FSRS-5 的 19 个默认权重（py-fsrs v5.1.3 DEFAULT_PARAMETERS）。
// 索引含义：
//
//	w[0..3]   Again/Hard/Good/Easy 的初始稳定性 S0
//	w[4..5]   初始难度 D0 参数
//	w[6..7]   难度更新（线性阻尼）与均值回归参数
//	w[8..10]  成功后稳定性增长参数
//	w[11..14] 遗忘后稳定性参数
//	w[15]     Hard 惩罚系数
//	w[16]     Easy 奖励系数
//	w[17..18] 同日短时稳定性参数
var DefaultParameters = []float64{
	0.40255, 1.18385, 3.173, 15.69105,
	7.1949, 0.5345, 1.4604, 0.0046,
	1.54575, 0.1192, 1.01925, 1.9395,
	0.11, 0.29605, 2.2698,
	0.2315, 2.9898,
	0.51655, 0.6621,
}

// 遗忘曲线常数（FSRS-5 默认 decay = -0.5，幂函数遗忘曲线）。
const Decay = -0.5

// Factor = 0.9^(1/Decay) - 1 = 19/81 ≈ 0.2345679。
// 使 nextInterval(S) 在目标保持率 0.9 时恰好等于 S：R(S, S) = 0.9。
var Factor = math.Pow(0.9, 1/Decay) - 1

// 目标保持率上下限（设置页可调范围，SPEC §4）。
const (
	MinRetention = 0.8
	MaxRetention = 0.95
)

// 难度夹取边界（官方实现 _clamp_difficulty）。
const (
	MinDifficulty = 1.0
	MaxDifficulty = 10.0
)

// MaximumInterval 最大间隔（天），官方默认 36500。
const MaximumInterval = 36500

// LeechThreshold 卡点卡判定阈值：累计遗忘达到该次数即标记（SPEC §4 默认 8）。
const LeechThreshold = 8

// RelearnStepMinutes 学习/重学步长（分钟）：Same-day relearn 不推明天，当日内重排。
const RelearnStepMinutes = 10

// FuzzRatio 间隔抖动比例（教学版 ±5%），仅对 ≥ MinFuzzInterval 天的间隔生效。
const (
	FuzzRatio       = 0.05
	MinFuzzInterval = 2.5
)
