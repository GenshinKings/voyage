package server

import (
	"strings"
	"testing"
	"time"
)

func TestComebackOnlyOnceAfterGap(t *testing.T) {
	s := NewServer(t.TempDir())
	now := time.Date(2026, 10, 30, 21, 0, 0, 0, time.Local)

	// 从没学过：不走召回（那是新手引导的事）
	if s.maybeComeback(now) {
		t.Fatal("从没学过不该触发召回")
	}

	// 昨天学过：不召回
	s.LastLearn = "2026-10-29"
	if s.maybeComeback(now) {
		t.Fatal("只隔一天不该召回")
	}

	// 隔了 5 天：召回一次，文案要求「不催 + 5 分钟小目标」
	s.LastLearn = "2026-10-25"
	if !s.maybeComeback(now) {
		t.Fatal("隔 5 天应该召回")
	}
	msg := s.Comeback
	if msg == "" {
		t.Fatal("召回文案为空")
	}
	for _, want := range []string{"5 分钟", "上次来是"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("召回文案缺少 %q：%s", want, msg)
		}
	}

	// 第二天再查：不重复
	if s.maybeComeback(now.Add(24 * time.Hour)) {
		t.Fatal("同一个招呼不该重复打")
	}

	// 回来学一次：招呼清掉，日期更新
	s.touchLearn()
	if s.Comeback != "" {
		t.Fatalf("回来之后招呼应清空，实际 %q", s.Comeback)
	}
	if s.LastLearn != time.Now().Format("2006-01-02") {
		t.Fatalf("LastLearn 应更新为今天，实际 %s", s.LastLearn)
	}
}
