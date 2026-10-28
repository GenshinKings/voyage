package memory

// decay_test.go — 第七轮 §四：衰减与遗忘（防记忆变垃圾场）。
import (
	"testing"
	"time"
)

// 验收 4（单元层）：ts 40 天前 + confidence 0.4 → Decay 后进 cold，检索不返回。
func TestDecaySendsStaleLowConfidenceToCold(t *testing.T) {
	now := time.Now()
	s := New()
	mk := func(id string, ts time.Time, conf float64, pinned bool) {
		if _, err := s.Add(Entry{ID: id, Text: "Python " + id, Kind: KindFact,
			Source: "session:x", Ts: ts, Confidence: conf, Pinned: pinned}, ts); err != nil {
			t.Fatal(err)
		}
	}
	mk("stale", now.AddDate(0, 0, -40), 0.4, false)
	mk("fresh", now.AddDate(0, 0, -1), 0.9, false)
	mk("pinned", now.AddDate(0, 0, -40), 0.4, true) // 身份/长期承诺：不衰减
	mk("used", now.Add(-2*time.Hour), 0.4, false)   // ts 老，但 2 小时前刚被引用过
	if err := s.Save(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	n := s.Decay(now, 30*24*time.Hour, 0.5)
	if n != 1 {
		t.Fatalf("只应衰减 1 条: %d", n)
	}
	if e := s.Get("stale"); e.Status != StatusCold {
		t.Fatalf("40 天未引用且 confidence<0.5 应进 cold: %s", e.Status)
	}
	if e := s.Get("pinned"); e.Status != StatusActive {
		t.Fatal("pinned 不衰减")
	}
	if e := s.Get("used"); e.Status != StatusActive {
		t.Fatal("近期被引用过的不衰减（以 lastUsedAt 计）")
	}
	// 检索：cold 不返回；即使状态没翻转，满足衰减判据的也会被跳过（双保险）
	got := s.Relevant([]string{"Python"}, now, 10)
	ids := map[string]bool{}
	for _, e := range got {
		ids[e.ID] = true
	}
	if ids["stale"] {
		t.Fatal("cold 条目不得出现在检索结果")
	}
	if !ids["fresh"] || !ids["pinned"] {
		t.Fatalf("fresh/pinned 应可检索: %v", ids)
	}
	// 未翻转状态的场景（跳过 Decay 直接检索）：同样不返回
	s2 := New()
	_, _ = s2.Add(Entry{ID: "ghost", Text: "Python ghost", Kind: KindFact,
		Source: "s", Ts: now.AddDate(0, 0, -40), Confidence: 0.4}, now.AddDate(0, 0, -40))
	if got := s2.Relevant([]string{"Python"}, now, 10); len(got) != 0 {
		t.Fatalf("满足衰减判据但状态未翻转的条目也应被检索跳过: %d", len(got))
	}
}

// superseded 检索永不返回。
func TestSupersededNeverRetrieved(t *testing.T) {
	now := time.Now()
	s := New()
	if _, err := s.Add(Entry{ID: "old", Text: "Python 旧结论", Kind: KindFact,
		Source: "s", Ts: now.AddDate(0, 0, -2), Confidence: 0.9}, now); err != nil {
		t.Fatal(err)
	}
	if !s.Supersede("old") {
		t.Fatal("Supersede 命中应返回 true")
	}
	if s.Supersede("不存在") {
		t.Fatal("Supersede 未命中应返回 false")
	}
	if got := s.Relevant([]string{"Python"}, now, 10); len(got) != 0 {
		t.Fatalf("superseded 检索永不返回: %d", len(got))
	}
}

// Cite 上限 0.99；time decay 排序：新者靠前。
func TestCiteCapAndRecencyOrdering(t *testing.T) {
	now := time.Now()
	s := New()
	_, _ = s.Add(Entry{ID: "a", Text: "Python 甲", Kind: KindFact, Source: "s", Ts: now.AddDate(0, 0, -20), Confidence: 0.97}, now)
	_, _ = s.Add(Entry{ID: "b", Text: "Python 乙", Kind: KindFact, Source: "s", Ts: now.AddDate(0, 0, -2), Confidence: 0.9}, now)
	s.Cite("a", now)
	if e := s.Get("a"); e.Confidence != 0.99 {
		t.Fatalf("confidence 上限 0.99: %.3f", e.Confidence)
	}
	got := s.Relevant([]string{"Python"}, now, 10)
	if len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("time decay：新者应靠前: %v", got)
	}
}
