package inbox

import "testing"

func TestAddListOpen(t *testing.T) {
	b := New(t.TempDir())
	if _, err := b.Add("   "); err == nil {
		t.Fatal("空白文本应被拒")
	}
	a, err := b.Add("  选品表要手工抄进 Excel  ")
	if err != nil {
		t.Fatalf("应收下：%v", err)
	}
	if a.Text != "选品表要手工抄进 Excel" {
		t.Fatalf("首尾空白应去掉，实际 %q", a.Text)
	}
	if a.Status != StatusOpen || a.ID == "" || a.CreatedAt == "" {
		t.Fatalf("记录不完整：%+v", a)
	}
	b.Add("笔记找不到")
	if got := len(b.List()); got != 2 {
		t.Fatalf("应有 2 条，实际 %d", got)
	}
	if got := len(b.Open()); got != 2 {
		t.Fatalf("应有 2 条待处理，实际 %d", got)
	}
}

func TestPromoteRequiresCheck(t *testing.T) {
	b := New(t.TempDir())
	it, _ := b.Add("端口老被占用")
	got, ok := b.Promote(it.ID, "修掉端口占用", "重启后能起来并打印端口号", "")
	if !ok {
		t.Fatal("应能 promote")
	}
	if got.Status != StatusPlanned || got.Check == "" {
		t.Fatalf("promote 后状态/验收不对：%+v", got)
	}
	if _, ok := b.Promote(it.ID, "再来一次", "x", ""); ok {
		t.Fatal("已处理的不能再 promote")
	}
	if len(b.Open()) != 0 {
		t.Fatal("promote 后不该还留在待处理")
	}
}

func TestDropKeepsRecord(t *testing.T) {
	b := New(t.TempDir())
	it, _ := b.Add("想学 Rust")
	if _, ok := b.Drop(it.ID); !ok {
		t.Fatal("应能放弃")
	}
	all := b.List()
	if len(all) != 1 || all[0].Status != StatusDropped {
		t.Fatalf("放弃后应保留记录：%+v", all)
	}
	if _, ok := b.Drop(it.ID); ok {
		t.Fatal("不能重复放弃")
	}
}

func TestPersistAcrossReload(t *testing.T) {
	dir := t.TempDir()
	b := New(dir)
	it, _ := b.Add("Ozon 图片要一张张下")
	b.Promote(it.ID, "写个批量下载", "一条命令下完 5 张", "先看有没有现成工具")

	b2 := New(dir)
	b2.Load()
	all := b2.List()
	if len(all) != 1 || all[0].Status != StatusPlanned || all[0].Goal == "" {
		t.Fatalf("读回不对：%+v", all)
	}
}
