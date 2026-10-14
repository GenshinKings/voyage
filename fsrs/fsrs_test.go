package fsrs

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

func baseTime() time.Time { return time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC) }

func approx(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// 性质 0：D0 锚定官方值。D0(r) = w4 − e^(w5·(r−1)) + 1（py-fsrs v5.1.3 同式），
// 四档初始难度必须落在这四个硬编码值上——防止公式系数漂移（如 r−1 被误改成 r−k）。
func TestInitialDifficultyMatchesOfficialValues(t *testing.T) {
	s := NewScheduler()
	want := map[Rating]float64{
		Again: 7.1949, // w4 − e^0 + 1
		Hard:  6.4884, // 8.1949 − e^0.5345
		Good:  5.2824, // 8.1949 − e^1.069
		Easy:  3.2245, // 8.1949 − e^1.6035
	}
	for r, w := range want {
		if got := s.initialDifficulty(r); !approx(got, w, 1e-3) {
			t.Fatalf("D0(%d) = %v, want %v", r, got, w)
		}
	}
}

// 性质 1：新卡 Good 评分后 I(1)≈S，且 R(S,S)=0.9（遗忘曲线在 interval=stability 处恰为目标保持率）。
func TestNewCardGoodIntervalApproxStabilityAndRetention(t *testing.T) {
	s := NewScheduler()
	card := NewCard(baseTime())
	got, res := s.Review(card, Good, baseTime(), 4200)

	if !approx(got.Stability, s.Parameters[2], 1e-9) {
		t.Fatalf("S0(Good) = %v, want %v", got.Stability, s.Parameters[2])
	}
	if res.SameDay {
		t.Fatalf("Good on new card should graduate to Review, not same-day")
	}
	if got.State != Review {
		t.Fatalf("state = %v, want Review", got.State)
	}
	// I(1) ≈ S：interval 由 round(S) 得来，偏差 < 1 天
	if math.Abs(float64(res.ScheduledDays)-got.Stability) >= 1 {
		t.Fatalf("I(1)=%d not ≈ S=%v", res.ScheduledDays, got.Stability)
	}
	// R(S, S) = 0.9：elapsed 恰为 S（连续时间）时，可提取性应为目标保持率 0.9。
	// 整日取整的 due 处允许 ±1% 偏差。
	rExact := got.Retrievability(s, baseTime().Add(time.Duration(got.Stability*24*float64(time.Hour))))
	if !approx(rExact, 0.9, 1e-9) {
		t.Fatalf("R(S,S) = %v, want 0.9", rExact)
	}
	rRounded := got.Retrievability(s, baseTime().Add(time.Duration(res.ScheduledDays)*24*time.Hour))
	if !approx(rRounded, 0.9, 0.01) {
		t.Fatalf("R at rounded interval = %v, want ≈0.9", rRounded)
	}
}

// 性质 1b：目标保持率可调（0.8-0.95），I 随保持率单调。
func TestRetentionKnobChangesInterval(t *testing.T) {
	s08 := NewScheduler()
	s08.DesiredRetention = 0.8
	s095 := NewScheduler()
	s095.DesiredRetention = 0.95
	i08 := s08.intervalDays(10)
	i095 := s095.intervalDays(10)
	if !(i08 > 10 && i095 < 10) {
		t.Fatalf("interval monotonicity broken: r=0.8 → %d, r=0.95 → %d (S=10)", i08, i095)
	}
}

// 性质 2：Review 卡 Again → lapse，S 衰减且 D 增加。
func TestAgainLapseDecayStabilityAndDifficultyGrows(t *testing.T) {
	s := NewScheduler()
	last := baseTime().Add(-30 * 24 * time.Hour)
	card := Card{
		State: Review, Stability: 10, Difficulty: 5,
		Due: last, LastReview: &last,
	}
	got, res := s.Review(card, Again, baseTime(), 3000)

	if got.Stability >= card.Stability {
		t.Fatalf("lapse must decay stability: S %v → %v", card.Stability, got.Stability)
	}
	if got.Difficulty <= card.Difficulty {
		t.Fatalf("lapse must increase difficulty: D %v → %v", card.Difficulty, got.Difficulty)
	}
	if got.Lapses != 1 {
		t.Fatalf("lapses = %d, want 1", got.Lapses)
	}
	if got.State != Relearning {
		t.Fatalf("state after lapse = %v, want Relearning", got.State)
	}
	if !res.SameDay {
		t.Fatalf("lapse from long-term review should schedule same-day relearn step")
	}
	if res.ScheduledDays != 0 {
		t.Fatalf("relearn step is same-day, scheduledDays = %d", res.ScheduledDays)
	}
}

// 性质 2b：成功评分下 Easy 增长最多，Hard 增长最少（惩罚/奖励生效）。
func TestRatingOrderingOnStabilityGrowth(t *testing.T) {
	s := NewScheduler()
	mk := func() Card {
		last := baseTime().Add(-10 * 24 * time.Hour)
		return Card{State: Review, Stability: 5, Difficulty: 5, LastReview: &last}
	}
	sHard, _ := s.Review(mk(), Hard, baseTime(), 1000)
	sGood, _ := s.Review(mk(), Good, baseTime(), 1000)
	sEasy, _ := s.Review(mk(), Easy, baseTime(), 1000)
	if !(sEasy.Stability > sGood.Stability && sGood.Stability > sHard.Stability) {
		t.Fatalf("growth ordering broken: Hard=%v Good=%v Easy=%v",
			sHard.Stability, sGood.Stability, sEasy.Stability)
	}
}

// 性质 3：fuzz 边界——≥2.5 天的间隔扰动 ±5%，<2.5 天不动。
func TestFuzzBounds(t *testing.T) {
	s := NewScheduler()
	s.SetFuzzRand(rand.New(rand.NewSource(42)))
	for _, ivl := range []int{3, 4, 7, 8, 20, 21, 50, 100, 365} {
		lo := int(math.Ceil(float64(ivl) * 0.95))
		hi := int(math.Floor(float64(ivl) * 1.05))
		for i := 0; i < 200; i++ {
			got := s.fuzzInterval(ivl)
			if got < lo || got > hi {
				t.Fatalf("fuzz(%d) = %d, out of [%d,%d]", ivl, got, lo, hi)
			}
		}
	}
	for _, ivl := range []int{1, 2} {
		if got := s.fuzzInterval(ivl); got != ivl {
			t.Fatalf("small interval %d must not fuzz, got %d", ivl, got)
		}
	}
}

// 性质 3b：同参数下 fuzz 覆盖多个取值（不是常数偏移）。
func TestFuzzVaries(t *testing.T) {
	s := NewScheduler()
	s.SetFuzzRand(rand.New(rand.NewSource(7)))
	seen := map[int]bool{}
	for i := 0; i < 100; i++ {
		seen[s.fuzzInterval(50)] = true
	}
	if len(seen) < 3 {
		t.Fatalf("fuzz should vary, got %d distinct values: %v", len(seen), seen)
	}
}

// 性质 4：Same-day relearn 不推明天——Review 卡 Again 后 due 在当日（分钟级），
// Learning 卡 Again/Hard 同理；同日再评走短时稳定性公式。
func TestSameDayRelearnStaysToday(t *testing.T) {
	s := NewScheduler()
	last := baseTime().Add(-30 * 24 * time.Hour)
	card := Card{State: Review, Stability: 10, Difficulty: 5, LastReview: &last}

	got, res := s.Review(card, Again, baseTime(), 2000)
	if !res.SameDay || got.Due.Sub(baseTime()) <= 0 || got.Due.Sub(baseTime()) >= time.Hour {
		t.Fatalf("Again must reschedule within the day, due = %v", got.Due)
	}

	// 同日再评（10 分钟后 Good）：走短时稳定性，状态回 Review，且不推到明天
	got2, res2 := s.Review(got, Good, baseTime().Add(10*time.Minute), 2000)
	if res2.SameDay {
		t.Fatalf("Good after relearn should graduate to day-level interval")
	}
	if got2.State != Review {
		t.Fatalf("state = %v, want Review", got2.State)
	}
	wantSt := got.Stability * math.Exp(s.Parameters[17]*(float64(Good)-3+s.Parameters[18]))
	if !approx(got2.Stability, wantSt, 1e-9) {
		t.Fatalf("short-term stability = %v, want %v", got2.Stability, wantSt)
	}

	// 新卡 Again：当日内 10 分钟学习步
	fresh := NewCard(baseTime())
	got3, res3 := s.Review(fresh, Again, baseTime(), 1500)
	if !res3.SameDay || got3.State != Learning {
		t.Fatalf("new card Again must stay in same-day learning step, got sameDay=%v state=%v", res3.SameDay, got3.State)
	}
	if got3.Due.Sub(baseTime()) != 10*time.Minute {
		t.Fatalf("learning step = %v, want 10m", got3.Due.Sub(baseTime()))
	}
}

// 性质 5：评分预览——四档预览互不相同且顺序合理（Easy ≥ Good ≥ Hard），
// 学习步内预览为分钟级文案。
func TestPreviewIntervals(t *testing.T) {
	s := NewScheduler()
	last := baseTime().Add(-10 * 24 * time.Hour)
	card := Card{State: Review, Stability: 5, Difficulty: 5, LastReview: &last}
	p := s.Preview(card, baseTime())
	for _, r := range []Rating{Again, Hard, Good, Easy} {
		if p[r] == "" {
			t.Fatalf("missing preview for rating %v", r)
		}
	}
	dHard := parseDays(p[Hard])
	dGood := parseDays(p[Good])
	dEasy := parseDays(p[Easy])
	if !(dEasy >= dGood && dGood >= dHard) {
		t.Fatalf("preview ordering broken: Hard=%v Good=%v Easy=%v", p[Hard], p[Good], p[Easy])
	}

	// 新卡处于学习态时，Again 预览是"10分钟"
	fresh := NewCard(baseTime())
	pf := s.Preview(fresh, baseTime())
	if pf[Again] != "10分钟" {
		t.Fatalf("new-card Again preview = %q, want 10分钟", pf[Again])
	}
}

func parseDays(s string) int {
	var d int
	if _, err := fmt.Sscanf(s, "%d天", &d); err != nil {
		return -1
	}
	return d
}

// 性质 6：难度夹取在 [1,10]；连续 Good 拉低难度、连续 Again 拉高难度但不越界。
func TestDifficultyClamped(t *testing.T) {
	s := NewScheduler()
	c := NewCard(baseTime())
	c, _ = s.Review(c, Good, baseTime(), 100)
	for i := 0; i < 30; i++ {
		next := baseTime().Add(time.Duration(400*(i+1)) * 24 * time.Hour)
		var r Rating
		if i < 15 {
			r = Again
		} else {
			r = Good
		}
		c, _ = s.Review(c, r, next, 100)
		if c.Difficulty < MinDifficulty || c.Difficulty > MaxDifficulty {
			t.Fatalf("difficulty out of bounds: %v", c.Difficulty)
		}
	}
	if c.Difficulty <= 5 {
		t.Fatalf("15 consecutive Again should push difficulty up, got %v", c.Difficulty)
	}
}

// 性质 7：长线模拟——Good 链下稳定性和非 fuzz 间隔单调不减，S 有限且为正。
// （fuzz 只在 ±5% 内抖动，封顶前用非 fuzz 的 intervalDays 检验单调性。）
func TestLongRunMonotonic(t *testing.T) {
	s := NewScheduler()
	c := NewCard(baseTime())
	c, _ = s.Review(c, Good, baseTime(), 100)
	prevIvl := s.intervalDays(c.Stability)
	for i := 0; i < 10; i++ {
		next := c.Due.Add(time.Hour) // 到期后再评
		c2, _ := s.Review(c, Good, next, 100)
		if c2.Stability <= 0 || math.IsInf(c2.Stability, 0) {
			t.Fatalf("stability invalid at step %d: %v", i, c2.Stability)
		}
		ivl := s.intervalDays(c2.Stability)
		if ivl < prevIvl {
			t.Fatalf("interval shrank on Good chain: step %d %d < %d", i, ivl, prevIvl)
		}
		prevIvl = ivl
		c = c2
	}
}
