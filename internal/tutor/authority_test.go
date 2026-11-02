package tutor

import (
	"strings"
	"testing"
)

// 学生实测 2026-11-02：上文给过完整代码、学生说「用的是最新的」，
// 助教仍坚持「你用的还是旧代码」。这条规则就是防止它再犯。
func TestTeachSystemDeclaresStudentAuthority(t *testing.T) {
	for _, want := range []string{
		"事实权威",
		"他对自己环境的说法永远优先于你的推断",
		"那不是现在的状态",
		"不允许第二次反驳同一件事",
		"不要用质问",
	} {
		if !strings.Contains(teachSystem, want) {
			t.Errorf("teachSystem 缺 %q", want)
		}
	}
}

// 这条规则不能把「该验证的地方」也一起废掉：确认环境要用动作，不是不许问。
func TestTeachSystemStillAsksForEvidence(t *testing.T) {
	if !strings.Contains(teachSystem, "把现在跑出来的那行贴上来") {
		t.Error("应保留「用一个动作确认」的正向写法")
	}
}
