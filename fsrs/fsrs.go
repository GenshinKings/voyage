package fsrs

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

// State 是卡片的学习状态机位置（对齐 py-fsrs：New 为本实现补充，官方 Learning=1）。
type State int

const (
	New        State = 0
	Learning   State = 1
	Review     State = 2
	Relearning State = 3
)

// Rating 是复习评分四档。
type Rating int

const (
	Again Rating = 1
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

// Card 是 FSRS 调度所需的最小记忆状态（SPEC §4：D/S/R/due/state/lapses/totalReviews）。
type Card struct {
	Difficulty   float64    `json:"difficulty"`   // D
	Stability    float64    `json:"stability"`    // S
	R            float64    `json:"r"`            // 可提取性缓存（展示用），评分时重算
	Due          time.Time  `json:"due"`          // 下次到期时间
	State        State      `json:"state"`        // New/Learning/Review/Relearning
	Step         int        `json:"step"`         // learning/relearning 步指针
	LastReview   *time.Time `json:"lastReview"`   // 最近一次评分时间
	Lapses       int        `json:"lapses"`       // 累计遗忘
	TotalReviews int        `json:"totalReviews"` // 累计评分次数
}

// NewCard 返回一张未学习的新卡（due=now，立即可学）。
func NewCard(now time.Time) Card {
	return Card{State: New, Due: now}
}

// Scheduler 持有参数与目标保持率。并发安全靠调用方（应用是单 goroutine 事件循环）。
type Scheduler struct {
	Parameters       []float64
	DesiredRetention float64
	fuzzRand         *rand.Rand // 可注入的确定性随机源；nil 时用全局 rand
}

// NewScheduler 用默认参数与目标保持率 0.9 构造。
func NewScheduler() *Scheduler {
	return &Scheduler{Parameters: append([]float64(nil), DefaultParameters...), DesiredRetention: 0.9}
}

// SetFuzzRand 注入确定性随机源（测试用）。
func (s *Scheduler) SetFuzzRand(r *rand.Rand) { s.fuzzRand = r }

// Retrievability 计算卡片在 t 时刻的可提取性 R（幂函数遗忘曲线）。
// R(t) = (1 + Factor * elapsedDays / S) ^ Decay
func (c *Card) Retrievability(s *Scheduler, t time.Time) float64 {
	if c.LastReview == nil || c.Stability <= 0 {
		return 0
	}
	elapsed := t.Sub(*c.LastReview).Hours() / 24
	if elapsed < 0 {
		elapsed = 0
	}
	return math.Pow(1+Factor*elapsed/c.Stability, Decay)
}

// intervalDays 由稳定性算下次间隔天数：
// I = S / Factor * (r^(1/Decay) - 1)，r=0.9 时 I=S（性质测试断言）。
// 四舍五入到整天，最少 1 天，封顶 MaximumInterval。
func (s *Scheduler) intervalDays(stability float64) int {
	ivl := (stability / Factor) * (math.Pow(s.DesiredRetention, 1/Decay) - 1)
	days := int(math.Round(ivl))
	if days < 1 {
		days = 1
	}
	if days > MaximumInterval {
		days = MaximumInterval
	}
	return days
}

// PreviewInterval 对外暴露间隔计算（评分按钮实时预览下次间隔，Anki 机制）。
func (s *Scheduler) PreviewInterval(stability float64) int { return s.intervalDays(stability) }

// initialStability S0(r) = max(w[r-1], 0.1)
func (s *Scheduler) initialStability(r Rating) float64 {
	return math.Max(s.Parameters[int(r)-1], 0.1)
}

// initialDifficulty D0(r) = clamp(w4 - e^(w5*(r-1)) + 1, 1, 10)
func (s *Scheduler) initialDifficulty(r Rating) float64 {
	d := s.Parameters[4] - math.Exp(s.Parameters[5]*(float64(r)-1)) + 1
	return clampDifficulty(d)
}

func clampDifficulty(d float64) float64 { return math.Min(math.Max(d, MinDifficulty), MaxDifficulty) }

// nextDifficulty 难度更新：线性阻尼 + 均值回归（官方 _next_difficulty）。
//
//	delta = -(w6 * (r - 3))
//	arg2  = D + (10 - D) * delta / 9
//	D'    = clamp(w7 * D0(Easy) + (1 - w7) * arg2, 1, 10)
func (s *Scheduler) nextDifficulty(d float64, r Rating) float64 {
	delta := -(s.Parameters[6] * (float64(r) - 3))
	arg2 := d + (10.0-d)*delta/9.0
	meanReversion := s.Parameters[7]*s.initialDifficulty(Easy) + (1-s.Parameters[7])*arg2
	return clampDifficulty(meanReversion)
}

// nextRecallStability 成功后稳定性（Hard 惩罚 / Easy 奖励）：
//
//	S' = S * (1 + e^w8 * (11-D) * S^(-w9) * (e^((1-R)*w10) - 1) * hard * easy)
func (s *Scheduler) nextRecallStability(d, st, r float64, rating Rating) float64 {
	hardPenalty := 1.0
	if rating == Hard {
		hardPenalty = s.Parameters[15]
	}
	easyBonus := 1.0
	if rating == Easy {
		easyBonus = s.Parameters[16]
	}
	return st * (1 +
		math.Exp(s.Parameters[8])*
			(11-d)*
			math.Pow(st, -s.Parameters[9])*
			(math.Exp((1-r)*s.Parameters[10])-1)*
			hardPenalty*
			easyBonus)
}

// nextForgetStability 遗忘后稳定性，取长程式与短程式较小者（官方 _next_forget_stability）：
//
//	长程: w11 * D^(-w12) * ((S+1)^w13 - 1) * e^((1-R)*w14)
//	短程: S / e^(w17*w18)
func (s *Scheduler) nextForgetStability(d, st, r float64) float64 {
	longTerm := s.Parameters[11] *
		math.Pow(d, -s.Parameters[12]) *
		(math.Pow(st+1, s.Parameters[13]) - 1) *
		math.Exp((1-r)*s.Parameters[14])
	shortTerm := st / math.Exp(s.Parameters[17]*s.Parameters[18])
	return math.Min(longTerm, shortTerm)
}

// shortTermStability 同日复习的短时稳定性：S' = S * e^(w17 * (r - 3 + w18))。
func (s *Scheduler) shortTermStability(st float64, r Rating) float64 {
	return st * math.Exp(s.Parameters[17]*(float64(r)-3+s.Parameters[18]))
}

// fuzzInterval 教学版 ±5% fuzz：仅对 ≥2.5 天的整日间隔生效，结果夹在
// [ceil(0.95*ivl), floor(1.05*ivl)] 且 ≥2，防同日扎堆。
func (s *Scheduler) fuzzInterval(days int) int {
	f := float64(days)
	if f < MinFuzzInterval {
		return days
	}
	lo := int(math.Ceil(f * (1 - FuzzRatio)))
	hi := int(math.Floor(f * (1 + FuzzRatio)))
	if hi < lo {
		hi = lo
	}
	if hi < 2 {
		hi = 2
	}
	if lo < 2 {
		lo = 2
	}
	var n int
	if s.fuzzRand != nil {
		n = lo + s.fuzzRand.Intn(hi-lo+1)
	} else {
		n = lo + rand.Intn(hi-lo+1)
	}
	if n > MaximumInterval {
		n = MaximumInterval
	}
	return n
}

// Preview 返回四个评分档的下次间隔文案（先算状态演化，再取间隔）。
// 学习/重学步内返回分钟级文案（当日内），毕业返回天数。
func (s *Scheduler) Preview(c Card, now time.Time) map[Rating]string {
	out := map[Rating]string{}
	for r := Again; r <= Easy; r++ {
		next, sameDay := s.nextDue(c, r, now)
		if sameDay {
			out[r] = fmt.Sprintf("%d分钟", int(math.Round(next.Sub(now).Minutes())))
		} else {
			days := int(math.Round(next.Sub(now).Hours() / 24))
			if days < 1 {
				days = 1
			}
			out[r] = fmt.Sprintf("%d天", days)
		}
	}
	return out
}

// nextDue 计算评分后卡片的新 due。sameDay 表示当日内重排（分钟级，不加 fuzz）。
// 状态机忠实移植 py-fsrs v5.1.3 review_card（单学习步 10 分钟配置）。
func (s *Scheduler) nextDue(c Card, rating Rating, now time.Time) (due time.Time, sameDay bool) {
	stepMin := RelearnStepMinutes

	switch c.State {
	case New, Learning:
		// 初始评分建立 S0/D0；Again/Hard 留在当日内学习步，Good/Easy 毕业。
		var st float64
		if c.Stability > 0 {
			if daysSince(c.LastReview, now) < 1 {
				st = s.shortTermStability(c.Stability, rating)
			} else {
				r := c.Retrievability(s, now)
				st = s.nextStability(c.Difficulty, c.Stability, r, rating)
			}
		} else {
			st = s.initialStability(rating)
		}
		switch rating {
		case Again:
			return now.Add(time.Duration(stepMin) * time.Minute), true
		case Hard:
			// 单学习步：Hard 折半步长，仍当日内（官方 1.5x 单步规则）。
			return now.Add(time.Duration(stepMin*3/2) * time.Minute), true
		default: // Good, Easy：毕业进 Review
			days := s.intervalDays(st)
			return now.Add(time.Duration(s.fuzzInterval(days)) * 24 * time.Hour), false
		}
	case Review, Relearning:
		var st float64
		if daysSince(c.LastReview, now) < 1 {
			st = s.shortTermStability(c.Stability, rating)
		} else {
			r := c.Retrievability(s, now)
			st = s.nextStability(c.Difficulty, c.Stability, r, rating)
		}
		if rating == Again {
			// 进入当日内重学步：Same-day relearn 不推明天（SPEC §4）。
			return now.Add(time.Duration(stepMin) * time.Minute), true
		}
		days := s.intervalDays(st)
		return now.Add(time.Duration(s.fuzzInterval(days)) * 24 * time.Hour), false
	}
	return now.Add(time.Duration(stepMin) * time.Minute), true
}

// nextStability 按评分档分派到成功式/遗忘式。
func (s *Scheduler) nextStability(d, st, r float64, rating Rating) float64 {
	if rating == Again {
		return s.nextForgetStability(d, st, r)
	}
	return s.nextRecallStability(d, st, r, rating)
}

// Review 处理一次评分：返回新卡状态与本次调度间隔（天；同日步返回 0）。
// 不修改入参卡片（调用方负责写回与落盘）。
func (s *Scheduler) Review(c Card, rating Rating, now time.Time, elapsedMs int64) (Card, Result) {
	next := c
	due, sameDay := s.nextDue(c, rating, now)

	// 更新 D/S
	if c.State == New {
		next.Stability = s.initialStability(rating)
		next.Difficulty = s.initialDifficulty(rating)
	} else {
		if daysSince(c.LastReview, now) < 1 {
			next.Stability = s.shortTermStability(c.Stability, rating)
		} else {
			r := c.Retrievability(s, now)
			next.Stability = s.nextStability(c.Difficulty, c.Stability, r, rating)
		}
		next.Difficulty = s.nextDifficulty(c.Difficulty, rating)
	}

	// 状态机迁移
	switch c.State {
	case New, Learning:
		switch rating {
		case Again:
			next.State = Learning
			next.Step = 0
		case Hard:
			next.State = Learning
			if c.State == New {
				next.Step = 0
			}
		default: // Good, Easy 毕业
			next.State = Review
			next.Step = 0
		}
	case Review:
		if rating == Again {
			next.State = Relearning
			next.Step = 0
			next.Lapses++
		}
	case Relearning:
		if rating == Again {
			next.Step = 0
		} else {
			next.State = Review
		}
	}
	next.TotalReviews++
	if next.LastReview == nil {
		t := now
		next.LastReview = &t
	} else {
		t := now
		next.LastReview = &t
	}
	next.R = c.Retrievability(s, now)
	next.Due = due

	return next, Result{
		ScheduledDays: intervalOrZero(due, sameDay, now),
		SameDay:       sameDay,
		ElapsedMs:     elapsedMs,
	}
}

// Result 是一次评分的调度结果（写 review log 用）。
type Result struct {
	ScheduledDays int
	SameDay       bool
	ElapsedMs     int64
}

func intervalOrZero(due time.Time, sameDay bool, now time.Time) int {
	if sameDay {
		return 0
	}
	return int(math.Round(due.Sub(now).Hours() / 24))
}

func daysSince(last *time.Time, now time.Time) float64 {
	if last == nil {
		return 1e9 // 无历史视为跨天
	}
	d := now.Sub(*last).Hours() / 24
	if d < 0 {
		d = 0
	}
	return d
}
