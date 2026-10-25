package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureListRead(t *testing.T) {
	dir := Ensure(t.TempDir())
	if dir == "" {
		t.Fatal("Ensure 应返回路径")
	}
	if got := List(dir); len(got) != 0 {
		t.Fatalf("首启应为空列表: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("乙"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("甲"), 0o644)
	os.WriteFile(filepath.Join(dir, "忽略.txt"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "子目录.md"), 0o755)
	list := List(dir)
	if len(list) != 2 {
		t.Fatalf("应只有 2 篇 md: %+v", list)
	}
	if got, err := Read(dir, "a.md"); err != nil || got != "甲" {
		t.Fatalf("读取失败: %q %v", got, err)
	}
	// 路径逃逸拒收
	if _, err := Read(dir, "..\\llm.json"); err == nil {
		t.Fatal("路径逃逸应报错")
	}
	if _, err := Read(dir, "不存在.md"); err == nil {
		t.Fatal("缺失文件应报错")
	}
}

func TestTranscript(t *testing.T) {
	dir := Ensure(t.TempDir())
	if got := Transcript(dir, 1000); got != "" {
		t.Fatalf("空目录应返回空: %q", got)
	}
	os.WriteFile(filepath.Join(dir, "n1.md"), []byte("笔记一内容"), 0o644)
	os.WriteFile(filepath.Join(dir, "n2.md"), []byte("笔记二内容"), 0o644)
	got := Transcript(dir, 0)
	for _, want := range []string{"n1.md", "笔记一内容", "n2.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("应含 %q: %q", want, got)
		}
	}
	// 截断：maxBytes 小于内容时只保留第一篇并标注
	got = Transcript(dir, 30)
	if !strings.Contains(got, "已截断") {
		t.Fatalf("应标注截断: %q", got)
	}
}
