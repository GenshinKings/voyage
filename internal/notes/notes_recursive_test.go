package notes

import (
	"os"
	"path/filepath"
	"testing"
)

// Obsidian 库的笔记多在子目录里：List 必须递归，且要跳过 .obsidian 这类元数据目录。
func TestListRecursiveSkipsMetaDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "顶层.md"))
	mustWrite(t, filepath.Join(root, "一些笔记", "Golang.md"))
	mustWrite(t, filepath.Join(root, "一些笔记", "深一层", "HTTP.md"))
	mustWrite(t, filepath.Join(root, ".obsidian", "workspace.md")) // 不该被算成笔记
	mustWrite(t, filepath.Join(root, ".trash", "垃圾.md"))

	got := List(root)
	names := map[string]bool{}
	for _, f := range got {
		names[f.Name] = true
	}
	if len(got) != 3 {
		t.Fatalf("应 3 篇（不含 .obsidian/.trash），实际 %d：%v", len(got), names)
	}
	for _, want := range []string{"顶层.md", "一些笔记/Golang.md", "一些笔记/深一层/HTTP.md"} {
		if !names[want] {
			t.Fatalf("缺 %s，实际 %v", want, names)
		}
	}
}

// 子路径能读，越界的要拒。
func TestReadGuardsPathEscape(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sub", "a.md"))

	if _, err := Read(root, "sub/a.md"); err != nil {
		t.Fatalf("子路径应能读：%v", err)
	}
	if _, err := Read(root, "../secret.md"); err == nil {
		t.Fatal("越界路径应被拒")
	}
	if _, err := Read(root, "a.txt"); err == nil {
		t.Fatal("非 md 应被拒")
	}
}

func TestValidDir(t *testing.T) {
	root := t.TempDir()
	if !ValidDir(root) {
		t.Fatal("存在的目录应通过")
	}
	if ValidDir(filepath.Join(root, "nope")) {
		t.Fatal("不存在的目录不该通过")
	}
	if ValidDir(filepath.Join(root, "x.md")) {
		t.Fatal("文件不该通过")
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# x"), 0o644); err != nil {
		t.Fatal(err)
	}
}
