// Package bandit — Thompson Sampling Beta-Bernoulli 多臂老虎机（前哨 2.0 策略学习层）。
//
// 与 1.x 的 TunerBandit/PlanBandit/AdviceBandit 同构（map[contextKey|arm]→{α,β}），
// 已训练后验可直接迁移。用途边界（宪法约束）：只在少数离散决策上学习（计划形态/
// 复习容量乘数/讲法选择），奖励函数=「学会了且想继续学」的可计算近似（完成×意愿），
// 永不优化短期打开率。透明度：均值与样本数全量可导出，用户可见。
package bandit

import (
	"math"
	"math/rand"
	"sort"
)

// Bandit 持有全部 (上下文|臂) → Beta 后验。JSON 兼容 1.x 的 map[string][2]float64。
type Bandit struct {
	Posterior map[string][2]float64 `json:"posterior"`
	Rng       *rand.Rand            `json:"-"`
}

func New(seed int64) *Bandit {
	return &Bandit{Posterior: map[string][2]float64{}, Rng: rand.New(rand.NewSource(seed))}
}

func (b *Bandit) ensure(key string) [2]float64 {
	ab, ok := b.Posterior[key]
	if !ok || ab[0] <= 0 || ab[1] <= 0 {
		ab = [2]float64{1, 1} // 均匀先验
	}
	return ab
}

// Pick Thompson 采样选臂（探索与利用自动平衡）。返回选中的臂下标。
func (b *Bandit) Pick(contextKey string, arms []string) int {
	best, bestSample := 0, -1.0
	for i, arm := range arms {
		ab := b.ensure(contextKey + "|" + arm)
		s := betaSample(b.Rng, ab[0], ab[1])
		if s > bestSample {
			best, bestSample = i, s
		}
	}
	return best
}

// PickN Thompson 采样排序取前 n 个臂（不重复）——用于「一次出多道题，覆盖多个知识点」。
// 低后验的臂也会按概率被采到（探索），高奖励的臂大概率排前（利用），自动平衡。
func (b *Bandit) PickN(contextKey string, arms []string, n int) []string {
	type row struct {
		arm string
		s   float64
	}
	rows := make([]row, 0, len(arms))
	for _, a := range arms {
		ab := b.ensure(contextKey + "|" + a)
		rows = append(rows, row{a, betaSample(b.Rng, ab[0], ab[1])})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].s > rows[j].s })
	if n > len(rows) {
		n = len(rows)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, rows[i].arm)
	}
	return out
}

// Settle 结算某次选择的奖励（0-1）。未选过的臂不结算。
func (b *Bandit) Settle(contextKey, arm string, reward float64) {
	if reward < 0 {
		reward = 0
	}
	if reward > 1 {
		reward = 1
	}
	key := contextKey + "|" + arm
	ab := b.ensure(key)
	ab[0] += reward
	ab[1] += 1 - reward
	b.Posterior[key] = ab
}

// ArmStat 臂的透明度数据（宪法：用户可见可覆盖）。
type ArmStat struct {
	Mean   float64 `json:"mean"`   // 后验均值
	Sample float64 `json:"sample"` // 样本数（α+β-2）
}

// Stats 导出某上下文全部臂的均值与样本数（数据页展示/审计用）。
func (b *Bandit) Stats(contextKey string, arms []string) []ArmStatRow {
	out := []ArmStatRow{}
	for _, arm := range arms {
		ab := b.ensure(contextKey + "|" + arm)
		n := ab[0] + ab[1] - 2
		if n < 0 {
			n = 0
		}
		out = append(out, ArmStatRow{Arm: arm, Mean: ab[0] / (ab[0] + ab[1]), Samples: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mean > out[j].Mean })
	return out
}

type ArmStatRow struct {
	Arm     string  `json:"arm"`
	Mean    float64 `json:"mean"`
	Samples float64 `json:"samples"`
}

// ---------- Beta 采样（Gamma 比值法，Marsaglia-Tsang） ----------

func betaSample(rng *rand.Rand, a, b float64) float64 {
	x := gammaSample(rng, a)
	y := gammaSample(rng, b)
	if x+y == 0 {
		return 0.5
	}
	return x / (x + y)
}

func gammaSample(rng *rand.Rand, shape float64) float64 {
	if shape < 1 {
		// shape<1：Boost 变换 G(α)=G(α+1)·U^(1/α)
		return gammaSample(rng, shape+1) * math.Pow(rng.Float64(), 1/shape)
	}
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9*d)
	for {
		var x, v float64
		for {
			x = rng.NormFloat64()
			v = 1 + c*x
			if v > 0 {
				break
			}
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x {
			return d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
