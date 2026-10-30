package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 索引要能回答「什么笔记在什么位置」：目录分组、标题、路径都对。
func TestBuildIndexGroupsByDir(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "根笔记.md"), "# 标题\n\n这是根目录笔记的正文")
	write(t, filepath.Join(root, "一些笔记", "Golang.md"), "# Golang\n\nHandlerFunc 是函数类型")
	write(t, filepath.Join(root, "一些笔记", "深一层", "HTTP.md"), "正文")
	write(t, filepath.Join(root, ".obsidian", "x.md"), "不该进索引")

	idx := Build(root)
	if len(idx.Entries) != 3 {
		t.Fatalf("应 3 篇，实际 %d", len(idx.Entries))
	}
	if got := idx.ByDir[""]; len(got) != 1 || got[0] != "根笔记.md" {
		t.Fatalf("根目录分组不对：%v", got)
	}
	if got := idx.ByDir["一些笔记"]; len(got) != 1 || got[0] != "一些笔记/Golang.md" {
		t.Fatalf("子目录分组不对：%v", got)
	}
	if got := idx.ByDir["一些笔记/深一层"]; len(got) != 1 {
		t.Fatalf("深一层分组不对：%v", got)
	}

	// 标题与首行正文
	for _, e := range idx.Entries {
		if e.Path == "一些笔记/Golang.md" {
			if e.Title != "Golang" {
				t.Fatalf("标题应为 Golang，实际 %q", e.Title)
			}
			if !strings.Contains(e.First, "HandlerFunc") {
				t.Fatalf("首行正文应含 HandlerFunc，实际 %q", e.First)
			}
		}
	}

	// 按修改时间倒序
	for i := 1; i < len(idx.Entries); i++ {
		if idx.Entries[i-1].ModTime < idx.Entries[i].ModTime {
			t.Fatal("索引应按修改时间倒序")
		}
	}
}

func TestBuildIndexEmptyDir(t *testing.T) {
	idx := Build(t.TempDir())
	if len(idx.Entries) != 0 || idx.ByDir == nil {
		t.Fatalf("空目录应给空索引且 ByDir 非 nil：%+v", idx)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
