package insight

import "testing"

func TestLines(t *testing.T) {
	reply := "漂亮，这才是真正的破案。\n\n[INSIGHT:线上访问的路径可能被网关/平台先接管，你看到的响应未必来自自己的进程]\n\n下一句。\n[INSIGHT:  换个没人抢的路径名，就能确认响应是不是自己的  ]"
	got := Lines(reply)
	if len(got) != 2 {
		t.Fatalf("应抽出 2 条，实际 %d：%v", len(got), got)
	}
	if got[0] != "线上访问的路径可能被网关/平台先接管，你看到的响应未必来自自己的进程" {
		t.Errorf("第一条不对：%q", got[0])
	}
	if got[1] != "换个没人抢的路径名，就能确认响应是不是自己的" {
		t.Errorf("第二条应去掉首尾空白：%q", got[1])
	}
}

func TestLinesNoMarker(t *testing.T) {
	if got := Lines("普通的一句话，没有标记"); len(got) != 0 {
		t.Errorf("没有标记时应返回空，实际：%v", got)
	}
}

func TestAddDedupes(t *testing.T) {
	dir := t.TempDir()
	_, created, err := Add(dir, "线上响应未必来自自己的进程", "lesson:1")
	if err != nil || !created {
		t.Fatalf("第一条应写入：created=%v err=%v", created, err)
	}
	_, created2, err := Add(dir, "  线上响应未必 来自自己的进程  ", "lesson:2")
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("只差空白的一句不该重复写入")
	}
	if n := len(Load(dir)); n != 1 {
		t.Errorf("应只有 1 条，实际 %d", n)
	}
}
