package usererr

import (
	"errors"
	"testing"
)

func TestLayeredError(t *testing.T) {
	e := New("今天的出题还没接上模型，去设置里贴一个就能用", "LLM 未配置（outpost2-data/llm.json）")
	if e.Error() != "今天的出题还没接上模型，去设置里贴一个就能用" {
		t.Fatalf("Error() 应返回用户文案: %q", e.Error())
	}
	d, ok := DetailOf(e)
	if !ok || d != "LLM 未配置（outpost2-data/llm.json）" {
		t.Fatalf("DetailOf 应取技术细节: %q %v", d, ok)
	}
	// 普通错误：DetailOf 回退到 err.Error()（日志照记，行为不变）
	plain := errors.New("知识点不存在")
	if d, ok := DetailOf(plain); ok || d != "知识点不存在" {
		t.Fatalf("普通错误应回退 err.Error(): %q %v", d, ok)
	}
	// Unwrap 链可见：errors.Is/As 能穿透
	if !errors.Is(e, e.wrapped) {
		t.Fatal("Unwrap 应保持链可见")
	}
}
