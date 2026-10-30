package review

import "testing"

func TestEnqueueAndDecide(t *testing.T) {
	dir := t.TempDir()
	m := New(dir)

	a := m.Enqueue(Item{Target: "立骨架", Output: "myTool v0.0.1 ready", Reason: "没有可用模型"})
	if a.ID == "" || a.Status != StatusPending || a.CreatedAt == "" {
		t.Fatalf("入队记录不完整: %+v", a)
	}
	if got := len(m.Pending()); got != 1 {
		t.Fatalf("待判应 1 条，实际 %d", got)
	}

	got, ok := m.Decide(a.ID, true, "", "", "self")
	if !ok || got.Status != StatusApproved || got.DecidedBy != "self" {
		t.Fatalf("判定结果不对: ok=%v item=%+v", ok, got)
	}
	if len(m.Pending()) != 0 {
		t.Fatal("判过之后不该还留在待判里")
	}

	// 重复判同一条：不该成功
	if _, ok := m.Decide(a.ID, false, "x", "y", "self"); ok {
		t.Fatal("已判过的记录不该能再判")
	}
}

func TestPersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	m := New(dir)
	it := m.Enqueue(Item{Target: "README", Output: "有是什么/怎么装/怎么用"})
	m.Decide(it.ID, false, "少了「怎么用」", "补一段真实例子", "self")

	m2 := New(dir)
	m2.Load()
	all := m2.All()
	if len(all) != 1 {
		t.Fatalf("读回应有 1 条，实际 %d", len(all))
	}
	if all[0].Status != StatusRejected || all[0].Where == "" {
		t.Fatalf("读回内容不对: %+v", all[0])
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	m := New(t.TempDir())
	m.Load()
	if len(m.All()) != 0 {
		t.Fatal("没有文件时应是空队列")
	}
}
