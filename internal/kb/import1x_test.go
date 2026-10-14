package kb

import (
	"strings"
	"testing"
)

// 1.x ReadingDoc 迁移：白名单来源入账、未知授权灰名单、Excluded 拒收、ZH 优先。
func TestImportReadingDocsV1(t *testing.T) {
	kb := New()
	raw := []byte(`[
  {"url":"https://github.com/x/agent-guide","title":"Agent 指南","text":"EN body","zh":"中文正文内容","at":"2026-10-01T00:00:00Z"},
  {"url":"https://go.dev/ref/mem","title":"Go 内存模型","text":"mem model","zh":"中文内存模型","at":"2026-09-01T00:00:00Z"},
  {"url":"https://paywall.example/book","title":"付费书章节","text":" proprietary content","zh":"","at":"2026-08-01T00:00:00Z"},
  {"url":"https://books.example/ch3","title":"教材第三章","text":"all rights reserved content","zh":"","at":"2026-08-01T00:00:00Z"}
]`)
	imported, rejected, errs := kb.ImportReadingDocsV1("kp-agent", raw)
	if imported != 2 || rejected != 2 {
		t.Fatalf("迁入 %d 拒收 %d errs=%v", imported, rejected, errs)
	}
	if len(errs) != 0 {
		t.Fatalf("不应有错误: %v", errs)
	}
	n := kb.Nodes["kp-agent"]
	if len(n.Sources) != 2 { // 2 白/灰入账
		t.Fatalf("应入账 2 片段: %d", len(n.Sources))
	}
	// ZH 优先作为 excerpt
	for _, s := range n.Sources {
		if s.URL == "https://github.com/x/agent-guide" && !strings.Contains(s.Excerpt, "中文正文") {
			t.Fatal("有中译时应以中文为片段")
		}
	}
	// 付费书（Excluded）不得出现在任何节点
	for _, s := range n.Sources {
		if strings.Contains(s.URL, "paywall") {
			t.Fatal("付费书片段应被拒收")
		}
	}
	// 迁移后可正常出导出（灰名单+白名单分列）
	attr := kb.Attribution()
	if len(attr.Whitelist)+len(attr.Graylist) != 2 {
		t.Fatalf("署名清单应含全部 2 条: %+v", attr)
	}
}
