package scheduler

// integration_test.go — 端到端集成：能力评估 → 调度 → 反馈学习 → 履历。
// 模拟 5 天真实使用：AI 助教用 Elo 校准难度带出题，调度器丢清单，
// 用户完成情况回流 bandit，里程碑进履历。全部真实调用，无 mock。

import (
	"strings"
	"testing"
	"time"

	"outpost2/internal/ability"
	"outpost2/internal/bandit"
	"outpost2/internal/evidence"
)

func TestFiveDayStory(t *testing.T) {
	now := time.Now()
	ab := ability.New()
	b := bandit.New(2026)
	lg := evidence.New()
	dels := []Deliverable{
		{ID: "P1", Title: "MCP 学习工具部署品", Deadline: now.AddDate(0, 0, 21), Progress: 0.1,
			Actions: []string{"写 MCP server 骨架", "接第一个工具", "写 README", "部署到公网", "找 3 个真实用户"}},
	}
	ctx := "local"

	for day := 0; day < 5; day++ {
		today := now.AddDate(0, 0, day)
		// 1. 助教按能力带出题（Elo 难度带 50-90%）
		lo, _ := ab.DifficultyBand("agent")
		_ = lo
		// 2. 调度丢清单
		shape := ResolveShape(EnergyNormal, b, ctx)
		plan := Plan(today, dels, nil, applyShape(EnergyNormal, shape), nil)
		// 3. 模拟用户：进度按切片推进；自报「还想来」（前 4 天），第 5 天「今天够了」
		completed := len(plan.Main) > 0
		mood := "还想来"
		if day == 4 {
			mood = "今天够了"
		}
		// 4. 答题事件回流 Elo（每天 2 题，难度=带内，全对——模拟状态良好的学习者）
		for i := 0; i < 2; i++ {
			ab.Answer("agent", lo+60, true)
		}
		// 5. 完成的切片从验收清单消化 + 里程碑进履历
		for range plan.Main {
			if len(dels[0].Actions) > 0 {
				dels[0].Actions = dels[0].Actions[1:]
				dels[0].Progress = min64(0.1+float64(day+1)*0.15, 0.9)
				if day == 2 {
					lg.Record("kp-mcp-server", "写 MCP server", evidence.Used, today, "sandbox:P1", "")
				}
			}
		}
		SettleDay(b, ctx, shape, completed, mood)
	}

	// 断言一：bandit 学会了「standard 对这个人有效」
	got := ResolveShape(EnergyNormal, b, ctx)
	if got != ShapeStandard {
		t.Fatalf("5 天正向反馈后应收敛 standard: %s", got)
	}
	// 断言二：能力评级上移（连续答对带内题）
	if r := ab.Rating("agent"); r <= 1200 {
		t.Fatalf("连对应涨分: %f", r)
	}
	// 断言三：履历有「用过」级条目
	if p, ok := lg.Points["kp-mcp-server"]; !ok || p.Depth != evidence.Used {
		t.Fatalf("履历缺失或深度错: %+v", lg.Points["kp-mcp-server"])
	}
	// 断言四：进度确实推进（切片消化了验收清单）
	if len(dels[0].Actions) == 5 {
		t.Fatal("5 天应消化部分验收清单")
	}
	// 断言五：证据页 HTML 包含知识点
	var sb strings.Builder
	lg.WriteHTML(&sb, "local", now)
	if !strings.Contains(sb.String(), "写 MCP server") {
		t.Fatal("证据页应含知识点")
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
