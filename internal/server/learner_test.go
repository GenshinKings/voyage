package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第六轮-P1-2：取材器审计测试——真实文件名/函数名进摘要；无效目录返回空=退回通用例子。
func TestLearnerMaterial(t *testing.T) {
	dir := t.TempDir()
	// 混入非 .go 与 _test.go：应被剔除
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util_test.go"), []byte("func helperTest() {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	code := "package main\n\n// LoadUser 读用户\nfunc LoadUser(id int) error { return nil }\n\ntype Repo struct{}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	m := learnerMaterial(dir)
	if m == "" {
		t.Fatal("有真实 .go 文件却拿到空摘要")
	}
	for _, want := range []string{"main.go", "LoadUser()", "func LoadUser", "type Repo"} {
		if !strings.Contains(m, want) {
			t.Errorf("摘要缺 %q：%s", want, m)
		}
	}
	if strings.Contains(m, "helperTest") {
		t.Error("_test.go 不应进入取材")
	}
	// 无效目录 → 空串（退回通用例子，不报错）
	if got := learnerMaterial(filepath.Join(dir, "nope")); got != "" {
		t.Errorf("无效目录应返回空串，got %q", got)
	}
	if got := learnerMaterial(""); got != "" {
		t.Errorf("空目录参数应返回空串，got %q", got)
	}
}
