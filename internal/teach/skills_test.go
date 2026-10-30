package teach

import (
	"strings"
	"testing"
)

func TestAllLoadsEverySkill(t *testing.T) {
	skills, err := All()
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(skills) < 5 {
		t.Fatalf("至少应有 5 条技能，实际 %d", len(skills))
	}
	want := map[string]bool{
		"diagnose-state":  false,
		"classify-error":  false,
		"make-exercise":   false,
		"judge-verdict":   false,
		"reclaim-learner": false,
	}
	for _, s := range skills {
		if s.Name == "" || s.Description == "" || s.Body == "" {
			t.Fatalf("技能字段为空: %+v", s)
		}
		if _, ok := want[s.Name]; ok {
			want[s.Name] = true
		}
		// 正文里不该出现 frontmatter 残留
		if strings.Contains(s.Body, "description:") && strings.Contains(s.Body, "\nname:") {
			t.Fatalf("%s 正文里混进了 frontmatter", s.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("缺少技能 %q", name)
		}
	}
}

func TestMetasProgressiveDisclosure(t *testing.T) {
	metas, err := Metas()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range metas {
		if m.Name == "" || m.Description == "" {
			t.Fatalf("元信息不完整: %+v", m)
		}
	}
	// 元信息只该有两个字段（不含正文）——这是渐进披露第一层
	if len(metas) == 0 {
		t.Fatal("元信息为空")
	}
}

func TestGetAndParseErrors(t *testing.T) {
	if s, ok, err := Get("judge-verdict"); err != nil || !ok {
		t.Fatalf("应能取到 judge-verdict: ok=%v err=%v", ok, err)
	} else if !strings.Contains(s.Body, "cause") {
		t.Fatalf("判词技能正文应提到 cause")
	}
	if _, ok, err := Get("nope"); err != nil || ok {
		t.Fatalf("不存在的技能应返回 ok=false, err=nil，实际 ok=%v err=%v", ok, err)
	}

	// 解析边界：name 与目录不一致要报错
	if _, err := parse("dir-a", "---\nname: dir-b\ndescription: x\n---\n正文\n"); err == nil {
		t.Fatal("name 与目录不一致应报错")
	}
	// 缺 frontmatter
	if _, err := parse("d", "没有 frontmatter"); err == nil {
		t.Fatal("缺 frontmatter 应报错")
	}
	// 空正文
	if _, err := parse("d", "---\nname: d\ndescription: x\n---\n\n"); err == nil {
		t.Fatal("空正文应报错")
	}
}
