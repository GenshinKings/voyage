package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCreatesTemplateAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := Ensure(dir)
	if p == "" {
		t.Fatal("Ensure 应返回路径")
	}
	if filepath.Base(p) != FileName {
		t.Fatalf("路径应以 %s 结尾: %s", FileName, p)
	}
	if b, err := os.ReadFile(p); err != nil || !strings.Contains(string(b), Title) {
		t.Fatal("缺失时应写默认模板")
	}
	// 已有用户内容不被覆盖
	if err := os.WriteFile(p, []byte(Title+"\n\n- 后端工程师\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Ensure(dir) != p {
		t.Fatal("已存在时应返回同一路径")
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "后端工程师") {
		t.Fatal("Ensure 不应覆盖用户画像内容")
	}
}

func TestBlockEmptyTemplateInjectsNothing(t *testing.T) {
	dir := t.TempDir()
	p := Ensure(dir)
	if got := Block(p); got != "" {
		t.Fatalf("纯模板（只有注释）不应注入: %q", got)
	}
	if got := Block(""); got != "" {
		t.Fatal("空路径应返回空")
	}
	if got := Block(filepath.Join(dir, "不存在.md")); got != "" {
		t.Fatal("缺失文件应返回空")
	}
}

func TestBlockInjectsBody(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	content := Title + "\n\n- 后端工程师，Go 为主\n- 每天 ~1 小时\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Block(p)
	if !strings.Contains(got, "后端工程师") || !strings.Contains(got, "学习者画像") {
		t.Fatalf("应注入画像正文: %q", got)
	}
	if strings.Contains(got, "<!--") {
		t.Fatal("注释不应注入")
	}
}

func TestSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := Save(p, Title+"\n- 测试\n"); err != nil {
		t.Fatal(err)
	}
	if got := Load(p); !strings.Contains(got, "- 测试") {
		t.Fatalf("Load 应回读: %q", got)
	}
	if err := Save("", "x"); err == nil {
		t.Fatal("空路径应报错")
	}
}
