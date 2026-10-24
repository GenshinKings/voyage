package reflib

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAndAddRoundtrip(t *testing.T) {
	dir := Ensure(t.TempDir())
	if dir == "" {
		t.Fatal("Ensure 应返回路径")
	}
	d := Doc{ID: "20261024-demo-skill", Title: "演示项目", URL: "https://github.com/x/y", Domain: "F", Date: "2026-10-24", Digest: "六阶段流水线范本"}
	if err := Add(dir, d, "# Demo\n\n原文内容"); err != nil {
		t.Fatal(err)
	}
	// 文件落盘
	if b, err := os.ReadFile(filepath.Join(dir, "20261024-demo-skill.md")); err != nil || !strings.Contains(string(b), "原文内容") {
		t.Fatalf("原文文件未落盘: %v", err)
	}
	// 清单回读
	idx := LoadIndex(dir)
	if len(idx.Docs) != 1 || idx.Docs[0].Title != "演示项目" || idx.Docs[0].File != "20261024-demo-skill.md" {
		t.Fatalf("清单不完整: %+v", idx.Docs)
	}
	// 同 ID 再入库 = 覆盖（幂等）
	d2 := d
	d2.Digest = "更新后的摘要"
	if err := Add(dir, d2, "# Demo v2"); err != nil {
		t.Fatal(err)
	}
	if idx := LoadIndex(dir); len(idx.Docs) != 1 || idx.Docs[0].Digest != "更新后的摘要" {
		t.Fatalf("同 ID 应覆盖不追加: %+v", LoadIndex(dir).Docs)
	}
}

func TestAddValidation(t *testing.T) {
	dir := Ensure(t.TempDir())
	if err := Add(dir, Doc{ID: " ", Title: "x"}, "c"); err == nil {
		t.Fatal("空 id 应报错")
	}
	if err := Add(dir, Doc{ID: "x", Title: ""}, "c"); err == nil {
		t.Fatal("空 title 应报错")
	}
	// 危险字符被清洗
	if err := Add(dir, Doc{ID: "../evil", Title: "x"}, "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.md")); err != nil {
		t.Fatal("id 应被清洗为安全文件名")
	}
}

func TestDigestInjection(t *testing.T) {
	dir := Ensure(t.TempDir())
	if got := Digest(dir, 30); got != "" {
		t.Fatalf("空清单应返回空串: %q", got)
	}
	if got := Digest("", 30); got != "" {
		t.Fatalf("空目录应返回空串: %q", got)
	}
	for i, title := range []string{"甲项目", "乙教程", "丙规范"} {
		if err := Add(dir, Doc{ID: "doc-" + string(rune('0'+i)), Title: title, Digest: "摘要" + string(rune('0'+i))}, "c"); err != nil {
			t.Fatal(err)
		}
	}
	got := Digest(dir, 30)
	for _, want := range []string{"《甲项目》", "摘要0", "《丙规范》"} {
		if !strings.Contains(got, want) {
			t.Fatalf("注入文本缺 %q: %q", want, got)
		}
	}
	// 上限生效
	if got := Digest(dir, 2); strings.Contains(got, "丙规范") || strings.Count(got, "《") != 2 {
		t.Fatalf("max 上限应生效: %q", got)
	}
	// 手编清单里的无 title 条目（Add 层已拒收）应被跳过
	if err := SaveIndex(dir, Index{Docs: append(LoadIndex(dir).Docs, Doc{ID: "hand-edited", File: "x.md"})}); err != nil {
		t.Fatal(err)
	}
	if got := Digest(dir, 30); strings.Contains(got, "hand-edited") || strings.Count(got, "《") != 3 {
		t.Fatalf("无 title 条目应跳过: %q", got)
	}
}

func TestAddExternalPathRef(t *testing.T) {
	dir := Ensure(t.TempDir())
	d := Doc{ID: "g-microsoft-lesson", Title: "LLM 从零课程", Path: `G:\02_学习与论文\Microsoft-lesson`, Digest: "nn-zero-to-hero 到 DeepSeek 攻坚"}
	if err := Add(dir, d, ""); err != nil {
		t.Fatal(err)
	}
	// 外部引用不写文件，只登记
	if _, err := os.Stat(filepath.Join(dir, "gmicrosoftlesson.md")); !os.IsNotExist(err) {
		t.Fatal("外部引用不应写库内文件")
	}
	idx := LoadIndex(dir)
	if len(idx.Docs) != 1 || idx.Docs[0].Path == "" {
		t.Fatalf("外部引用应入清单: %+v", idx.Docs)
	}
	// 注入与库内文档同权
	if got := Digest(dir, 30); !strings.Contains(got, "LLM 从零课程") {
		t.Fatalf("外部引用应注入: %q", got)
	}
}

func TestSyncNavAutoUpdate(t *testing.T) {
	dir := Ensure(t.TempDir())
	// 首次使用：空库也能生成导航骨架
	if err := SyncNav(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, NavFile))
	if err != nil || !strings.Contains(string(b), "教学参考库导航") || !strings.Contains(string(b), "暂无条目") {
		t.Fatalf("空库应生成骨架导航: %v", err)
	}
	// 入库自动更新导航（库内 + 外部各一条）
	if err := Add(dir, Doc{ID: "a", Title: "库内一篇", Digest: "摘要A", File: "a.md"}, "正文"); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, Doc{ID: "b", Title: "外部一处", Path: "G:\\x\\y", Digest: "摘要B"}, ""); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, NavFile))
	if err != nil {
		t.Fatal(err)
	}
	nav := string(b)
	for _, want := range []string{"库内文档 1 篇", "外部资料 1 处", "《库内一篇》", "《外部一处》", "`a.md`", "`G:\\x\\y`"} {
		if !strings.Contains(nav, want) {
			t.Fatalf("导航缺 %q:\n%s", want, nav)
		}
	}
}
