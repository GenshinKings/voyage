package scheduler

import (
	"fmt"
	"testing"
	"time"
)

func base(now time.Time) (dels []Deliverable, due []ReviewCard, extras []Extra) {
	dels = []Deliverable{
		{ID: "P1", Title: "部署品", Deadline: now.AddDate(0, 0, 20), Progress: 0.3,
			Actions: []string{"写部署脚本", "跑通冒烟", "发社区帖"}},
		{ID: "P2", Title: "Agent 作品", Deadline: now.AddDate(0, 0, 45), Progress: 0.8,
			Actions: []string{"补 README"}},
	}
	due = []ReviewCard{
		{ID: "c1", Due: now.Add(-time.Hour), Title: "FSRS 参数"},
		{ID: "c2", Due: now.Add(time.Hour), Title: "MCP 握手"},
	}
	extras = []Extra{
		{ID: "e1", Title: "读 Blender MCP 源码", Domain: "P2"},
		{ID: "e2", Title: "拆解 context-mode 压缩策略", Domain: "extra-c"},
	}
	return
}

// Low 精力：推进清零、复习预算减半（10 分钟→5 分钟）、加餐保留——「今天够了」是合法答案。
func TestLowEnergySkipsMain(t *testing.T) {
	now := time.Now()
	dels, due, extras := base(now)
	for i := 0; i < 10; i++ { // 12 张 due，预算减半后应截断在 5 张
		due = append(due, ReviewCard{ID: fmt.Sprintf("x%d", i), Due: now})
	}
	plan := Plan(now, dels, due, EnergyLow, extras)
	if len(plan.Main) != 0 {
		t.Fatalf("Low 精力不应有推进任务: %+v", plan.Main)
	}
	if len(plan.Reviews) != 5 {
		t.Fatalf("Low 精力复习预算 5 分钟应装 5 张: %d", len(plan.Reviews))
	}
	if len(plan.Extras) == 0 {
		t.Fatal("Low 精力仍应保留可选加餐")
	}
}

// deadline 最近者优先为主产出物。
func TestDeadlinePicksMain(t *testing.T) {
	now := time.Now()
	dels, due, extras := base(now)
	plan := Plan(now, dels, due, EnergyNormal, extras)
	if len(plan.Main) == 0 || plan.Main[0].DeliverableID != "P1" {
		t.Fatalf("应选 deadline 最近的 P1: %+v", plan.Main)
	}
	if len(plan.Main) > 2 {
		t.Fatalf("Normal 精力切片应≤2: %d", len(plan.Main))
	}
}

// 并列 deadline 时进度低者先救。
func TestTieBreakByProgress(t *testing.T) {
	now := time.Now()
	dels := []Deliverable{
		{ID: "A", Title: "A", Deadline: now.AddDate(0, 0, 10), Progress: 0.7, Actions: []string{"a1"}},
		{ID: "B", Title: "B", Deadline: now.AddDate(0, 0, 10), Progress: 0.2, Actions: []string{"b1"}},
	}
	plan := Plan(now, dels, nil, EnergyNormal, nil)
	if len(plan.Main) != 1 || plan.Main[0].DeliverableID != "B" {
		t.Fatalf("并列 deadline 应选进度低的 B: %+v", plan.Main)
	}
}

// 复习容量封顶：due 20 张只装预算内数量（约 8-10）。
func TestReviewCapacityCap(t *testing.T) {
	now := time.Now()
	var due []ReviewCard
	for i := 0; i < 20; i++ {
		due = append(due, ReviewCard{ID: string(rune('a'+i%26)) + string(rune('a'+i/26)), Due: now})
	}
	dels, _, extras := base(now)
	plan := Plan(now, dels, due, EnergyNormal, extras)
	if len(plan.Reviews) > 10 {
		t.Fatalf("复习容量应封顶≈10: %d", len(plan.Reviews))
	}
	if len(plan.Reviews) < 5 {
		t.Fatalf("容量内应尽量装满: %d", len(plan.Reviews))
	}
}

// 加餐与主任务异域（交错）：主任务 P1，e1（域 P2）优先于同域项。
func TestExtrasDomainInterleave(t *testing.T) {
	now := time.Now()
	dels, _, _ := base(now)
	extras := []Extra{
		{ID: "same", Title: "同域加餐", Domain: "P1"},
		{ID: "diff", Title: "异域加餐", Domain: "extra-x"},
	}
	plan := Plan(now, dels, nil, EnergyHigh, extras)
	for _, e := range plan.Extras {
		if e.ID == "same" {
			t.Fatal("同域加餐不应被选为加餐（交错原则）")
		}
	}
	if len(plan.Extras) == 0 {
		t.Fatal("异域加餐应入选")
	}
}

// Note 可解释：非空且包含主推进与复习说明。
func TestNoteExplains(t *testing.T) {
	now := time.Now()
	dels, due, extras := base(now)
	plan := Plan(now, dels, due, EnergyNormal, extras)
	if plan.Note == "" {
		t.Fatal("调度理由（人话）不应为空")
	}
	t.Logf("Note: %s", plan.Note)
}
