package tutor

// verdict_test.go — 第八轮-P0-1 验收：标记容错矩阵 + 双通道判定。
import (
	"context"
	"strings"
	"testing"

	"voyage/internal/llm"
)

func TestParsePassMarkTolerances(t *testing.T) {
	cases := []struct {
		reply string
		kind  string
		warn  bool
	}{
		{"很好。\n[PASS]\n", "pass", false},
		{"答对了 [PASS] 继续", "pass", false},               // 行内
		{"答对了  [ PASS ]  继续", "pass", false},           // 括号内空白
		{"答对了。\n【PASS】", "pass", false},                // 全角【】
		{"［PASS:partial］", "partial", false},           // 全角方括号
		{"【PASS:partial】", "partial", false},           // 全角中括号+partial
		{"答对了。\n[pass]", "pass", false},                // 小写
		{"例子：\n```\n[PASS]\n```\n照着改。", "", true},      // 代码块内=误写
		{"~~~\n【PASS】\n~~~", "", true},                 // 波浪围栏同样拦截
		{"还没答到点上，再想想。", "", false},                     // 无标记
		{"外层【PASS】加上```代码块里 [PASS]```", "pass", false}, // 围栏外有=正常判过
	}
	for i, c := range cases {
		kind, warn := ParsePassMark(c.reply)
		if kind != c.kind {
			t.Fatalf("case %d %q: kind=%q 期望 %q", i, c.reply, kind, c.kind)
		}
		if (warn != "") != c.warn {
			t.Fatalf("case %d %q: warn=%q 期望 warn=%v", i, c.reply, warn, c.warn)
		}
	}
}

// verdictMock 教学回复走 Chat 轮转，判定走 ChatJSON（judgeJSON）。
type verdictMock struct {
	replies   []string
	n         int
	judgeJSON string
	judgeErr  error
	jsonN     int
}

func (m *verdictMock) Chat(system string, msgs []llm.Message) (string, error) {
	if m.n < len(m.replies) {
		r := m.replies[m.n]
		m.n++
		return r, nil
	}
	return m.replies[len(m.replies)-1], nil
}
func (m *verdictMock) ChatJSON(system string, msgs []llm.Message) (string, error) {
	m.jsonN++
	if m.judgeErr != nil {
		return "", m.judgeErr
	}
	return m.judgeJSON, nil
}

// 开一场「检查题在等答案」的会话。
func openCheckSession(t *testing.T, tu *Tutor, lesson Lesson) *Session {
	t.Helper()
	s, _, err := tu.StartOrResume(context.Background(), lesson)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// 标记缺失 → 结构化通道接住，仍正常收尾；diagnostics 记 structured。
func TestSendStructuredChannelWhenMarkerMissing(t *testing.T) {
	dir := t.TempDir()
	mc := &verdictMock{replies: []string{
		"检查题：把输出贴上来？",  // 开场
		"很好，这就是正确的输出。", // 学生回答后：没有标记
	}, judgeJSON: `{"pass":true,"reason":"输出正确","cause":"none"}`}
	tu := &Tutor{Dir: dir, Client: mc}
	lesson := Lesson{ID: "l1", Subject: "测试", Title: "变量", Brief: "能说出变量是什么", Status: "active", Order: 1}
	openCheckSession(t, tu, lesson)
	v, err := tu.Send(context.Background(), lesson, "输出是 hello")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "pass" || v.Channel != "structured" {
		t.Fatalf("应由结构化通道判过: %+v", v)
	}
	s := tu.Load("l1")
	if s.Verdict != "pass" {
		t.Fatalf("会话应收尾: %q", s.Verdict)
	}
	if n := len(s.Diagnostics); n == 0 || s.Diagnostics[n-1].Channel != "structured" || s.Diagnostics[n-1].Verdict != "pass" {
		t.Fatalf("diagnostics 缺失或不对: %+v", s.Diagnostics)
	}
}

// 验收：吐在代码块里 → 不得判过（结构化判 fail，误因带回）。
func TestSendMarkerInCodeFenceNotPass(t *testing.T) {
	dir := t.TempDir()
	mc := &verdictMock{replies: []string{
		"检查题：把输出贴上来？",
		"看这个例子：\n```\n[PASS]\n```\n你的输出不对。",
	}, judgeJSON: `{"pass":false,"reason":"他没贴出自己的输出","cause":"unfinished"}`}
	tu := &Tutor{Dir: dir, Client: mc}
	lesson := Lesson{ID: "l2", Subject: "测试", Title: "变量", Brief: "能说出变量是什么", Status: "active", Order: 1}
	openCheckSession(t, tu, lesson)
	v, err := tu.Send(context.Background(), lesson, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "" {
		t.Fatalf("代码块内标记不得判过: %+v", v)
	}
	if v.Cause != "unfinished" {
		t.Fatalf("结构化 fail 应带回误因: %+v", v)
	}
	s := tu.Load("l2")
	if s.Verdict != "" {
		t.Fatalf("会话不得收尾: %q", s.Verdict)
	}
	last := s.Diagnostics[len(s.Diagnostics)-1]
	if last.Channel != "structured" || last.Verdict != "fail" || !strings.Contains(last.Note, "代码块") {
		t.Fatalf("诊断应记结构化 fail 与误写警告: %+v", last)
	}
}

// 验收：全角【PASS】正常判过（走标记通道，不烧判定调用）。
func TestSendFullWidthMarkPassesViaMarker(t *testing.T) {
	dir := t.TempDir()
	mc := &verdictMock{replies: []string{
		"检查题：把输出贴上来？",
		"答对了。\n【PASS】",
	}}
	tu := &Tutor{Dir: dir, Client: mc}
	lesson := Lesson{ID: "l3", Subject: "测试", Title: "变量", Brief: "能说出变量是什么", Status: "active", Order: 1}
	openCheckSession(t, tu, lesson)
	v, err := tu.Send(context.Background(), lesson, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "pass" || v.Channel != "marker" || mc.jsonN != 0 {
		t.Fatalf("全角标记应走 marker 通道: %+v jsonN=%d", v, mc.jsonN)
	}
}

// 判定器不可用 → 不收尾不 panic（记 error 诊断，课堂继续）。
func TestSendStructuredChannelErrorKeepsLessonOpen(t *testing.T) {
	dir := t.TempDir()
	mc := &verdictMock{replies: []string{
		"检查题：把输出贴上来？",
		"嗯，继续。",
	}, judgeErr: errJudgeDown}
	tu := &Tutor{Dir: dir, Client: mc}
	lesson := Lesson{ID: "l4", Subject: "测试", Title: "变量", Brief: "能说出变量是什么", Status: "active", Order: 1}
	openCheckSession(t, tu, lesson)
	v, err := tu.Send(context.Background(), lesson, "hello")
	if err != nil || v.Kind != "" {
		t.Fatalf("判定器挂了应继续对话: %+v %v", v, err)
	}
	last := tu.Load("l4").Diagnostics[len(tu.Load("l4").Diagnostics)-1]
	if last.Channel != "structured" || last.Verdict != "error" {
		t.Fatalf("应记 error 诊断: %+v", last)
	}
}

var errJudgeDown = &judgeDown{}

type judgeDown struct{}

func (*judgeDown) Error() string { return "judge down" }
