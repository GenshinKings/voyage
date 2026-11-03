package metrics

import (
	"os"
	"testing"
	"time"
)

func TestRetentionRate(t *testing.T) {
	s := New(t.TempDir())
	_ = s.Record(Event{Kind: KindRetention, Key: "c1", OK: true})
	_ = s.Record(Event{Kind: KindRetention, Key: "c2", OK: false})
	_ = s.Record(Event{Kind: KindRetention, Key: "c3", OK: true})
	sum := s.Summarize(0)
	if sum.RetentionTotal != 3 || sum.RetentionPass != 2 {
		t.Fatalf("计数不对：%+v", sum)
	}
	if sum.RetentionRate < 0.66 || sum.RetentionRate > 0.67 {
		t.Errorf("保持率应为 2/3，实际 %v", sum.RetentionRate)
	}
}

func TestSummarizeWindow(t *testing.T) {
	s := New(t.TempDir())
	_ = s.Record(Event{At: time.Now().AddDate(0, 0, -30), Kind: KindRetention, Key: "old", OK: false})
	_ = s.Record(Event{Kind: KindRetention, Key: "new", OK: true})
	if sum := s.Summarize(7); sum.RetentionTotal != 1 {
		t.Errorf("7 天窗口应只算 1 条，实际 %d", sum.RetentionTotal)
	}
	if sum := s.Summarize(0); sum.RetentionTotal != 2 {
		t.Errorf("全部历史应 2 条，实际 %d", sum.RetentionTotal)
	}
}

func TestTransferCounts(t *testing.T) {
	s := New(t.TempDir())
	_ = s.Record(Event{Kind: KindTransfer, Key: "状态机", OK: true})
	_ = s.Record(Event{Kind: KindTransfer, Key: "循环", OK: false})
	if got := s.Summarize(0).TransferHits; got != 1 {
		t.Errorf("迁移命中只算成功的，实际 %d", got)
	}
}

func TestMarkSessionSpontaneousReturn(t *testing.T) {
	s := New(t.TempDir())
	// 第一次进来不算回归
	if got, err := s.MarkSession(); got || err != nil {
		t.Errorf("第一次不该算自发回归：got=%v err=%v", got, err)
	}
	// 手动塞一条 25 小时前的回归记录，再进来就该算
	_ = s.Record(Event{At: time.Now().Add(-25 * time.Hour), Kind: KindReturn, Key: "session", OK: true})
	if got, err := s.MarkSession(); !got || err != nil {
		t.Errorf("相隔 25 小时回来应该算自发回归：got=%v err=%v", got, err)
	}
	if got := s.Summarize(0).ReturnHits; got != 2 {
		t.Errorf("回归命中应为 2（历史一条 + 这次一条），实际 %d", got)
	}
}

func TestBrokenLineSkipped(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	_ = s.Record(Event{Kind: KindRetention, Key: "c1", OK: true})
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{坏行\n")
	_ = f.Close()
	if got := s.Summarize(0).RetentionTotal; got != 1 {
		t.Errorf("坏行不该毁掉统计，实际 %d", got)
	}
}
