package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 出题取材：多语言 + 跳过测试文件 + 空目录返回空串。
func TestLearnerMaterialMultiLang(t *testing.T) {
	dir := t.TempDir()

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("app.py", "def parse_invoice(path):\n    return open(path).read()\n")
	write("component.tsx", "export function PriceTag(props) {\n  return props.value\n}\n")
	write("main.go", "package main\n\nfunc compute(a int) int { return a * 2 }\n")
	write("app_test.py", "def test_nothing():\n    pass\n")
	write("styles.css", "body { margin: 0 }\n")

	got := learnerMaterial(dir)
	if got == "" {
		t.Fatal("多语言目录不该返回空串")
	}
	for _, want := range []string{"app.py", "parse_invoice", "component.tsx", "PriceTag"} {
		if !strings.Contains(got, want) {
			t.Fatalf("材料里缺少 %q：\n%s", want, got)
		}
	}
	if strings.Contains(got, "app_test.py") {
		t.Fatalf("测试文件不该进材料：\n%s", got)
	}
	if strings.Contains(got, "styles.css") {
		t.Fatalf("非源码文件不该进材料：\n%s", got)
	}
}

// 空目录/不存在目录 = 空串，出题退回通用例子（保持旧行为）。
func TestLearnerMaterialEmpty(t *testing.T) {
	if got := learnerMaterial(""); got != "" {
		t.Fatalf("空目录参数应返回空串，实际 %q", got)
	}
	if got := learnerMaterial(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("不存在的目录应返回空串，实际 %q", got)
	}
	empty := t.TempDir()
	if got := learnerMaterial(empty); got != "" {
		t.Fatalf("空目录应返回空串，实际 %q", got)
	}
	// 只有测试文件也算空
	if err := os.WriteFile(filepath.Join(empty, "x_test.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := learnerMaterial(empty); got != "" {
		t.Fatalf("只有测试文件时应返回空串，实际 %q", got)
	}
}
