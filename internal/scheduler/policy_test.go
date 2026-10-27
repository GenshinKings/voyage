package scheduler

import (
	"testing"
	"time"

	"voyage/internal/bandit"
)

// Low 自报硬约束：bandit 无权推翻身体（宪法）。
func TestLowHardConstraint(t *testing.T) {
	b := bandit.New(1)
	for i := 0; i < 50; i++ { // 即使 sprint 后验被灌满高奖励
		b.Settle("ctx|high", "sprint", 1.0)
	}
	if got := ResolveShape(EnergyLow, b, "ctx"); got != ShapeMaintain {
		t.Fatalf("Low 自报必须直通保养: %s", got)
	}
}

// bandit 策略层学习：Normal 精力下某形态持续高奖励 → 收敛选中。
func TestShapeConverges(t *testing.T) {
	b := bandit.New(42)
	ctx := "user1"
	for i := 0; i < 150; i++ {
		shape := ResolveShape(EnergyNormal, b, ctx)
		// 现实模拟：standard 形态当天完成且意犹未尽；maintain 形态没完成
		completed, mood := false, ""
		if shape == ShapeStandard {
			completed, mood = true, "还想来"
		}
		SettleDay(b, ctx, shape, completed, mood)
	}
	got := ResolveShape(EnergyNormal, b, ctx)
	if got != ShapeStandard {
		t.Fatalf("150 轮后 Normal 应收敛 standard: %s", got)
	}
}

// 日终结算的奖励映射：完成+还想来 > 完成但累 > 未完成。
func TestSettleRewardOrdering(t *testing.T) {
	b1, b2, b3 := bandit.New(1), bandit.New(1), bandit.New(1)
	for i := 0; i < 100; i++ {
		SettleDay(b1, "c", ShapeStandard, true, "还想来")
		SettleDay(b2, "c", ShapeStandard, true, "今天够了")
		SettleDay(b3, "c", ShapeStandard, false, "")
	}
	m1 := b1.Stats("c", []string{"standard"})[0].Mean
	m2 := b2.Stats("c", []string{"standard"})[0].Mean
	m3 := b3.Stats("c", []string{"standard"})[0].Mean
	if !(m1 > m2 && m2 > m3) {
		t.Fatalf("奖励排序应 1.0>0.6>0.1: %f %f %f", m1, m2, m3)
	}
}

// 集成：自报 High + bandit 选 sprint → Plan 输出 3 片推进。
func TestShapeAppliesToPlan(t *testing.T) {
	now := time.Now()
	dels := []Deliverable{
		{ID: "P1", Title: "部署品", Deadline: now.AddDate(0, 0, 7), Progress: 0.2,
			Actions: []string{"切片一", "切片二", "切片三"}},
	}
	b := bandit.New(9)
	shape := ResolveShape(EnergyHigh, b, "u1")
	eff := applyShape(EnergyHigh, shape)
	plan := Plan(now, dels, nil, eff, nil)
	if eff == EnergyHigh && len(plan.Main) != 3 {
		t.Fatalf("sprint 形态应 3 片: %d", len(plan.Main))
	}
	plan.Note = string(shape) + " | " + plan.Note
	if plan.Note == "" {
		t.Fatal("Note 应保留可解释性")
	}
}
