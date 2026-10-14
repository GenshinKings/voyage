package evidence

import (
	"strings"
	"testing"
	"time"
)

// 履历单向升级：known→used→done 只升不降；source/evidence 刷新。
func TestRecordUpgradesOnly(t *testing.T) {
	now := time.Now()
	l := New()
	l.Record("kp-mcp", "MCP 协议", Known, now, "lesson:F-AI-lesson-20261013", "")
	l.Record("kp-mcp", "MCP 协议", Used, now.Add(time.Hour), "sandbox:mcp-101", "")
	// 误报 Known（低深度）不应降级
	l.Record("kp-mcp", "MCP 协议", Known, now.Add(2*time.Hour), "", "")
	p := l.Points["kp-mcp"]
	if p.Depth != Used {
		t.Fatalf("深度应停在 Used（只升不降）: %v", p.Depth)
	}
	if p.Source != "sandbox:mcp-101" {
		t.Fatalf("source 应刷新为最新: %s", p.Source)
	}
	l.Record("kp-mcp", "MCP 协议", Done, now.Add(3*time.Hour), "deliverable:P1", "https://github.com/x/p1")
	if l.Points["kp-mcp"].Depth != Done {
		t.Fatal("应升至 Done")
	}
}

// 计数与排序：做过>用过>知道，同级最近优先。
func TestCountsAndSort(t *testing.T) {
	now := time.Now()
	l := New()
	l.Record("a", "甲", Known, now, "s", "")
	l.Record("b", "乙", Done, now, "s", "")
	l.Record("c", "丙", Used, now.Add(time.Hour), "s", "")
	k, u, d := l.Counts()
	if k != 1 || u != 1 || d != 1 {
		t.Fatalf("计数错误: %d/%d/%d", k, u, d)
	}
	list := l.List()
	if list[0].ID != "b" || list[1].ID != "c" || list[2].ID != "a" {
		t.Fatalf("排序错误: %v", list)
	}
}

// HTML 导出：包含三项计数与证据链接，纯事实无装饰。
func TestHTMLExport(t *testing.T) {
	now := time.Now()
	l := New()
	l.Record("kp-1", "FSRS 调度", Done, now, "deliverable:P2", "https://example.com/ev")
	var b strings.Builder
	l.WriteHTML(&b, "测试者", now)
	out := b.String()
	for _, want := range []string{"做过 <b>1</b>", "FSRS 调度", "做过", "https://example.com/ev", "测试者 的学习履历"} {
		if !strings.Contains(out, want) {
			t.Fatalf("证据页缺少: %s", want)
		}
	}
	if strings.Contains(out, "100%") || strings.Contains(out, "掌握度") {
		t.Fatal("证据页不得出现掌握度表演（宪法 1.4）")
	}
}
