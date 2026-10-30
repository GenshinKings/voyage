package run

import (
	"strings"
	"testing"
)

func TestTemplatesShape(t *testing.T) {
	ps := Templates()
	if len(ps) != 4 {
		t.Fatalf("应有四档模板，实际 %d", len(ps))
	}
	for _, p := range ps {
		if len(p.Stages) < 6 {
			t.Fatalf("%s 关卡太少（%d），从零做项目至少要六步", p.ID, len(p.Stages))
		}
		for _, s := range p.Stages {
			if strings.TrimSpace(s.Goal) == "" || strings.TrimSpace(s.Check) == "" || strings.TrimSpace(s.Hint) == "" {
				t.Fatalf("%s 的关卡 %s 三个字段必须都写", p.ID, s.ID)
			}
		}
		// 每档都要有「发版」和「测试」两条硬关卡
		var hasRelease, hasTest bool
		for _, s := range p.Stages {
			if s.ID == "s5-release" {
				hasRelease = true
			}
			if s.ID == "s3-test" {
				hasTest = true
			}
		}
		if !hasRelease || !hasTest {
			t.Fatalf("%s 缺关键关卡（测试=%v 发版=%v）", p.ID, hasTest, hasRelease)
		}
	}
}

func TestSubmitGatesProgress(t *testing.T) {
	p, ok := TemplateByID("t1-fix-my-annoyance")
	if !ok {
		t.Fatal("找不到 t1 模板")
	}
	r, err := Start(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Progress(); got != "0/6" {
		t.Fatalf("初始进度应为 0/6，实际 %s", got)
	}

	// 空提交不解锁
	if ok, reason := r.Submit("  "); ok || reason == "" {
		t.Fatal("空提交应被拒绝且给出原因")
	}
	if r.Current != 0 {
		t.Fatal("空提交不该推进关卡")
	}

	// 第一关（无 Expect）任何非空输出都过
	if ok, reason := r.Submit("myTool v0.0.1 ready"); !ok {
		t.Fatalf("第一关应通过，实际：%s", reason)
	}
	if r.Current != 1 {
		t.Fatalf("应推进到第 2 关，实际 %d", r.Current)
	}

	// 第二关同理
	if ok, _ := r.Submit("input=a -> output=[1,2,3]"); !ok {
		t.Fatal("第二关应通过")
	}

	// 第三关要求出现测试成功字样
	if ok, reason := r.Submit("all good"); ok {
		t.Fatalf("第三关不该通过（缺测试输出）：%s", reason)
	}
	if ok, reason := r.Submit("go test ./...\nok  \tmytool\t0.01s"); !ok {
		t.Fatalf("第三关应通过，实际：%s", reason)
	}
}

func TestDeliverable(t *testing.T) {
	p, _ := TemplateByID("t2-remember-things")
	r, _ := Start(p)
	// 依次满足所有关卡
	for {
		st := r.CurrentStage()
		if st == nil {
			break
		}
		out := "done"
		if len(st.Expect) > 0 {
			out = strings.Join(st.Expect, " ") + " ok"
		}
		if ok, reason := r.Submit(out); !ok {
			t.Fatalf("关卡 %s 未通过：%s", st.ID, reason)
		}
	}
	if !r.Finished {
		t.Fatal("全部提交后应标记完成")
	}
	d := r.Deliverable()
	for _, want := range []string{p.Title, "v0.1", "证据"} {
		if !strings.Contains(d, want) {
			t.Fatalf("交付证明缺 %q：\n%s", want, d)
		}
	}
}

func TestStartEmptyStages(t *testing.T) {
	if _, err := Start(Project{ID: "x"}); err == nil {
		t.Fatal("没有关卡的项目不该能开始")
	}
}
