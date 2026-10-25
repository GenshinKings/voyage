package iterate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRepo 建一个最小 Go 仓库（git init + go.mod + internal/x 包），供闸门测试。
func fakeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", args, out)
		}
	}
	must("git", "init", "-q")
	must("git", "config", "user.email", "t@t")
	must("git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n\ngo 1.26\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "internal", "x"), 0o755)
	os.MkdirAll(filepath.Join(dir, "cmd", "outpost2"), 0o755)
	os.WriteFile(filepath.Join(dir, "cmd", "outpost2", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "x", "x.go"), []byte("package x\n\nfunc Add(a, b int) int { return a + b }\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "internal", "x", "x_test.go"), []byte("package x\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) { if Add(1, 2) != 3 {\n\tt.Fatal(\"bad\")\n} }\n"), 0o644)
	must("git", "add", ".")
	must("git", "commit", "-qm", "init")
	return dir
}

func TestSafePathWhitelist(t *testing.T) {
	repo := fakeRepo(t)
	// 白名单内
	p, err := SafePath(repo, "internal/x/x.go")
	if err != nil || !strings.Contains(filepath.ToSlash(p), "internal/x/x.go") {
		t.Fatalf("internal/ 应放行: %v %v", p, err)
	}
	// 黑名单：数据目录/cmd/go.mod/越界
	for _, bad := range []string{"outpost2-data/llm.json", "cmd/outpost2/main.go", "go.mod", "../escape", "README.md", ""} {
		if _, err := SafePath(repo, bad); err == nil {
			t.Fatalf("%q 应拒收", bad)
		}
	}
}

func TestGatesAndRestore(t *testing.T) {
	repo := fakeRepo(t)
	// 闸门初始全绿
	if out, err := RunTests(repo); err != nil {
		t.Fatalf("初始测试应绿: %s", out)
	}
	if out, err := Build(repo); err != nil {
		t.Fatalf("初始构建应过: %s", out)
	}
	// 助教写坏代码 → 测试闸门红 → Restore 复原
	p, _ := SafePath(repo, "internal/x/x.go")
	os.WriteFile(p, []byte("package x\n\nfunc Add(a, b int) int { return a - b }\n"), 0o644)
	if _, err := RunTests(repo); err == nil {
		t.Fatal("写坏后测试应红")
	}
	Restore(repo)
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "a + b") {
		t.Fatal("Restore 应复原代码")
	}
	if _, err := RunTests(repo); err != nil {
		t.Fatalf("复原后应绿: %v", err)
	}
	// 闸门全绿 → Commit 落库
	os.WriteFile(p, []byte("package x\n\n// Add 加法。\nfunc Add(a, b int) int { return a + b }\n"), 0o644)
	if _, err := RunTests(repo); err != nil {
		t.Fatal(err)
	}
	if err := Commit(repo, "iter: 注释 Add"); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("git", "-C", repo, "log", "--oneline", "-1").CombinedOutput()
	if !strings.Contains(string(out), "iter: 注释 Add") {
		t.Fatalf("应产生提交: %s", out)
	}
}
