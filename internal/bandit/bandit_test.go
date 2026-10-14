package bandit

import (
	"math"
	"testing"
)

// 奖励恒定的臂应被收敛选中（利用学习生效）。
func TestConvergesToRewardingArm(t *testing.T) {
	b := New(42)
	arms := []string{"standard", "sprint", "maintain"}
	for i := 0; i < 200; i++ {
		chosen := b.Pick("plan", arms)
		reward := 0.1
		if arms[chosen] == "sprint" {
			reward = 0.95 // 只有 sprint 高奖励
		}
		b.Settle("plan", arms[chosen], reward)
	}
	stats := b.Stats("plan", arms)
	if stats[0].Arm != "sprint" {
		t.Fatalf("200 轮后应收敛到 sprint: %+v", stats)
	}
	if stats[0].Mean < 0.8 {
		t.Fatalf("sprint 均值应≥0.8: %f", stats[0].Mean)
	}
}

// 后验迁移兼容：1.x 格式的 {α,β} map 可直接注入并继续工作。
func TestPosteriorMigrationFromV1(t *testing.T) {
	b := New(7)
	// 模拟 1.x 训练过的后验（如 TunerBandit 的 "域|难度|乘数"）
	b.Posterior["memo|1.0"] = [2]float64{91, 9} // 高奖励史
	b.Posterior["memo|1.25"] = [2]float64{5, 5}
	arms := []string{"1.0", "1.25"}
	// 利用期：多次采样应绝大多数选 1.0
	pick1 := 0
	for i := 0; i < 100; i++ {
		if arms[b.Pick("memo", arms)] == "1.0" {
			pick1++
		}
	}
	if pick1 < 85 {
		t.Fatalf("已训练后验应主导选择（1.0 被选 %d/100）", pick1)
	}
	// 继续训练：环境反转后（1.0 连续低奖励）应能转移
	for i := 0; i < 300; i++ {
		chosen := b.Pick("memo", arms)
		r := 0.05
		if arms[chosen] == "1.25" {
			r = 0.95
		}
		b.Settle("memo", arms[chosen], r)
	}
	stats := b.Stats("memo", arms)
	if stats[0].Arm != "1.25" {
		t.Fatalf("环境反转后应转移到 1.25: %+v", stats)
	}
}

// 透明度：Stats 均值与样本数正确。
func TestStatsTransparency(t *testing.T) {
	b := New(1)
	arms := []string{"a", "b"}
	b.Settle("ctx", "a", 1)
	b.Settle("ctx", "a", 1)
	b.Settle("ctx", "b", 0)
	stats := b.Stats("ctx", arms)
	byArm := map[string]ArmStatRow{}
	for _, r := range stats {
		byArm[r.Arm] = r
	}
	if a := byArm["a"]; a.Mean < 0.74 || a.Samples != 2 {
		t.Fatalf("a 臂统计错误: %+v", a)
	}
	// b 臂一次失败：Beta(1,1)先验下后验均值 = 1/3（平滑性质，非 0）
	if bb := byArm["b"]; bb.Samples != 1 || math.Abs(bb.Mean-1.0/3.0) > 0.01 {
		t.Fatalf("b 臂统计错误: %+v", bb)
	}
}

// 未见过的新臂享受均匀先验（新选项有探索机会）。
func TestNewArmGetsExploration(t *testing.T) {
	b := New(3)
	arms := []string{"old", "brand_new"}
	for i := 0; i < 50; i++ {
		chosen := b.Pick("ctx", arms)
		b.Settle("ctx", arms[chosen], 0.9) // old 被持续强化
	}
	// 加入新臂后（先验均匀），至少应被探索一次
	b.Posterior["ctx|brand_new"] = [2]float64{1, 1}
	picked := false
	for i := 0; i < 60; i++ {
		if arms[b.Pick("ctx", arms)] == "brand_new" {
			picked = true
			break
		}
	}
	if !picked {
		t.Fatal("新臂应在探索中被尝试")
	}
}
