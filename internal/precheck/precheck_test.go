package precheck

import (
	"context"
	"errors"
	"testing"
)

func fakeChat(out string, err error) ChatFunc {
	return func(ctx context.Context, system, user string) (string, error) { return out, err }
}

func TestParseOK(t *testing.T) {
	v, err := Parse(`好的，判定如下：
{"verdict":"ok","why":"术语他上节见过","suggestion":"直接上","terms":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != VerdictOK || v.Why == "" {
		t.Fatalf("解析不对：%+v", v)
	}
}

func TestParseFallsBackToBase(t *testing.T) {
	cases := []string{
		"完全不是 JSON",
		`{"verdict":"随便编的"}`,
		`{"verdict":"ok"`,
	}
	for _, c := range cases {
		v, err := Parse(c)
		if err != nil {
			t.Fatalf("%q 不该报错：%v", c, err)
		}
		if v.Verdict != VerdictBase {
			t.Errorf("%q 应退回 needs-base，实际 %s", c, v.Verdict)
		}
	}
}

func TestJudgeSendsLearnerAndLesson(t *testing.T) {
	var got string
	chat := func(ctx context.Context, system, user string) (string, error) {
		got = user
		return `{"verdict":"too-hard","why":"依赖他卡过的循环","suggestion":"先补一个小循环"}`, nil
	}
	v, err := Judge(context.Background(), chat,
		Learner{WeakPoints: []string{"循环"}, DoneCount: 1},
		Lesson{Title: "番茄钟", Terms: []string{"状态机"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Verdict != VerdictTooHard {
		t.Errorf("判定应透传，实际 %s", v.Verdict)
	}
	for _, want := range []string{"循环", "番茄钟", "状态机"} {
		if !contains(got, want) {
			t.Errorf("发给判官的内容里少了 %q", want)
		}
	}
}

func TestJudgePropagatesError(t *testing.T) {
	_, err := Judge(context.Background(), fakeChat("", errors.New("llm 挂了")), Learner{}, Lesson{})
	if err == nil {
		t.Error("LLM 报错应该传上来，不能假装判定成功")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
