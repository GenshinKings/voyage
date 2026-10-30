package tutor

import (
	"testing"
	"time"
)

// 平滑：1/1 不该显示成 100%（样本太少，会让人误以为稳）。
func TestRateIsSmoothed(t *testing.T) {
	b1 := &Bucket{Judged: 1, Independent: 1}
	if got := b1.Rate(); got >= 1 {
		t.Fatalf("1 次判定的比率不该到 1，实际 %v", got)
	}
	b2 := &Bucket{Judged: 100, Independent: 100}
	if !(b2.Rate() > b1.Rate()) {
		t.Fatalf("样本多时应更接近 1：%v vs %v", b2.Rate(), b1.Rate())
	}
	if b1.Enough() {
		t.Fatal("1 次判定不该算样本足够")
	}
	if !(&Bucket{Judged: MinSample}).Enough() {
		t.Fatal("正好 MinSample 次应算足够")
	}
}

// 按节 + 按天同时聚合，且不影响总计。
func TestByLessonAndByDayAggregation(t *testing.T) {
	tu := &Tutor{Dir: t.TempDir(), Clock: func() time.Time {
		return time.Date(2026, 10, 30, 20, 0, 0, 0, time.Local)
	}}
	tu.recordVerdict("pass", false, "", "structured", "l1")
	tu.recordVerdict("fail", false, "concept", "structured", "l1")
	tu.recordVerdict("pass", false, "", "structured", "l2")

	st := tu.stats()
	snap := st.Snapshot()

	if snap["judged"] != 3 {
		t.Fatalf("总计应为 3，实际 %v", snap["judged"])
	}
	byLesson, _ := snap["byLesson"].(map[string]any)
	if len(byLesson) != 2 {
		t.Fatalf("应有 2 个节，实际 %v", byLesson)
	}
	l1, _ := byLesson["l1"].(map[string]any)
	if l1["judged"] != 2 || l1["independent"] != 1 {
		t.Fatalf("l1 聚合不对: %v", l1)
	}
	causes, _ := l1["causes"].(map[string]int)
	if causes["concept"] != 1 {
		t.Fatalf("l1 的错误类型应记 concept=1: %v", l1["causes"])
	}
	byDay, _ := snap["byDay"].(map[string]any)
	day, _ := byDay["2026-10-30"].(map[string]any)
	if day["judged"] != 3 {
		t.Fatalf("当天应聚合 3 次，实际 %v", byDay)
	}
}

// 趋势：两窗样本都不够时不显示（不拿小样本编故事）。
func TestTrendNeedsEnoughSample(t *testing.T) {
	tu := &Tutor{Dir: t.TempDir()}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.Local)
	st := tu.stats()

	// 只在前窗放 2 次判定：不够（前窗 = 3~5 天前）
	st.notePassFor("l1", "2026-10-26", true, false)
	st.notePassFor("l1", "2026-10-27", true, false)
	if _, _, _, ok := st.trend(now); ok {
		t.Fatal("样本不足时不该有趋势")
	}

	// 两窗各补到门槛
	for i := 0; i < MinSample; i++ {
		st.notePassFor("l1", "2026-10-26", true, false)  // 前窗（4 天前）
		st.notePassFor("l1", "2026-10-29", false, false) // 后窗（1 天前）
	}
	delta, prevN, curN, ok := st.trend(now)
	if !ok {
		t.Fatal("两窗都过门槛时应给出趋势")
	}
	if prevN < MinSample || curN < MinSample {
		t.Fatalf("样本量应达标: prev=%d cur=%d", prevN, curN)
	}
	if delta >= 0 {
		t.Fatalf("后窗全错、前窗全对，趋势应为负，实际 %v", delta)
	}
}

// 落盘读回保留按节/按天维度。
func TestStatsPersistBuckets(t *testing.T) {
	dir := t.TempDir()
	tu := &Tutor{Dir: dir}
	tu.recordVerdict("pass", false, "", "structured", "l9")

	tu2 := &Tutor{Dir: dir}
	st := tu2.LoadStats()
	if b := st.ByLesson["l9"]; b == nil || b.Judged != 1 {
		t.Fatalf("按节聚合没读回来: %+v", st.ByLesson)
	}
	if len(st.ByDay) == 0 {
		t.Fatal("按天聚合没读回来")
	}
}
