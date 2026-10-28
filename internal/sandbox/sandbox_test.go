package sandbox

import (
	"os"
	"strings"
	"testing"
)

// 全链路：开沙盒 → 验收不通过（bug 在）→ 用户修文件 → 验收通过 → 路径安全。
func TestBreakFixLoop(t *testing.T) {
	root := t.TempDir() + "/ws"
	c := Library()["bc-mcp-typo"]
	s, err := Create(root, c)
	if err != nil {
		t.Fatal(err)
	}
	// 初始状态：bug 在，验收不过
	res, err := s.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatal("bug 未修时验收应不通过: " + res.Detail)
	}
	// 用户「修一半」（只修 typo 不修 upper）——确定性验收必须仍拦截
	half := strings.Replace(s.Case.Files["main.go"], "appand", "append", 1)
	if err := writeFileIn(root, "main.go", half); err != nil {
		t.Fatal(err)
	}
	if res, _ := s.Verify(); res.Passed {
		t.Fatal("修一半不应通过（func appand 定义仍在）")
	}
	// 全修（两处 appand + ToUpperShim）
	full := strings.ReplaceAll(half, "appand", "append")
	full = strings.Replace(full, "strings.ToUpperShim", `func(s string) string { return strings.ToUpper(s) }`, 1)
	if err := writeFileIn(root, "main.go", full); err != nil {
		t.Fatal(err)
	}
	res, err = s.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Fatalf("全修应通过: %s", res.Detail)
	}
}

// 路径安全：案例文件名不得逃逸沙盒根。
func TestPathSafety(t *testing.T) {
	root := t.TempDir()
	if err := PathSafety(root, root+"/ok.go"); err != nil {
		t.Fatalf("根内路径应放行: %v", err)
	}
	for _, evil := range []string{root + "/../escape.go", "C:/Windows/evil.go", root + "/../../x"} {
		if err := PathSafety(root, evil); err == nil {
			t.Fatalf("逃逸路径应被拒: %s", evil)
		}
	}
}

// 非空工作区拒绝开箱（防误伤用户文件）。
func TestRefuseNonEmptyRoot(t *testing.T) {
	root := t.TempDir()
	if err := writeFileIn(root, "user-file.txt", "别动我的东西"); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(root, Library()["bc-mcp-typo"]); err == nil {
		t.Fatal("非空工作区应拒绝开箱")
	}
}

func writeFileIn(root, rel, content string) error {
	return osWriteFile(root+"/"+rel, content)
}

func osWriteFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
