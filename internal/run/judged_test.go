package run

import (
	"strings"
	"testing"
)

func TestSubmitJudgedGates(t *testing.T) {
	p, _ := TemplateByID("t1-fix-my-annoyance")
	r, err := Start(p)
	if err != nil {
		t.Fatal(err)
	}

	// 第一关 Expect 为空 → 需要外部判定
	if !r.NeedsExternalJudge() {
		t.Fatal("第一关应需要外部判定")
	}

	// 外部判不过：不解锁
	if ok, why := r.SubmitJudged("我做了", false, "没看到启动输出"); ok {
		t.Fatal("外部判不过不该解锁")
	} else if !strings.Contains(why, "启动输出") {
		t.Fatalf("应把原因带回，实际 %q", why)
	}
	if r.Current != 0 {
		t.Fatal("判不过时关卡不该推进")
	}

	// 空产物：即使判过也不放行
	if ok, _ := r.SubmitJudged("   ", true, ""); ok {
		t.Fatal("空产物不该放行")
	}

	// 外包判过：解锁，并记录产物
	if ok, why := r.SubmitJudged("myTool v0.0.1 ready", true, ""); !ok {
		t.Fatalf("外部判过应当解锁，实际：%s", why)
	}
	if r.Current != 1 {
		t.Fatalf("应推进到第 2 关，实际 %d", r.Current)
	}
	if r.Results["s1-skeleton"] == "" {
		t.Fatal("产物应被记录")
	}
}

func TestNeedsExternalJudgeFalseWhenExpectSet(t *testing.T) {
	p, _ := TemplateByID("t1-fix-my-annoyance")
	r, _ := Start(p)
	// 推进到第三关（s3-test，Expect 非空）
	_, _ = r.SubmitJudged("ok", true, "")
	_, _ = r.SubmitJudged("ok", true, "")
	if r.CurrentStage() == nil || r.CurrentStage().ID != "s3-test" {
		t.Fatalf("应停在第 3 关，实际 %v", r.CurrentStage())
	}
	if r.NeedsExternalJudge() {
		t.Fatal("Expect 非空的关卡不该叫外部判定")
	}
}
