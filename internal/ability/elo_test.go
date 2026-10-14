package ability

import (
	"math"
	"testing"
)

// 期望正确率的边界性质：同难度 50%，低难度高，高难度低。
func TestExpectedBand(t *testing.T) {
	tr := New()
	if e := tr.Expected("go", 1200); math.Abs(e-0.5) > 0.01 {
		t.Fatalf("同难度期望应≈50%%: %f", e)
	}
	if e := tr.Expected("go", 900); e < 0.8 {
		t.Fatalf("低 300 分期望应≥80%%: %f", e)
	}
	if e := tr.Expected("go", 1500); e > 0.2 {
		t.Fatalf("高 300 分期望应≤20%%: %f", e)
	}
}

// 答对难题涨分多、答对易题涨分少（Elo 核心）；难度带随能力上移。
func TestRatingMovesAndBandFollows(t *testing.T) {
	tr := New()
	before := tr.Rating("go")
	// 连续答对 1500 难度的题（能力 1200 打高难度）
	for i := 0; i < 6; i++ {
		tr.Answer("go", 1500, true)
	}
	after := tr.Rating("go")
	if after <= before {
		t.Fatalf("连对难题应涨分: %f→%f", before, after)
	}
	low, high := tr.DifficultyBand("go")
	if low >= high || high <= before {
		t.Fatalf("难度带应随 rating 上移: band=(%f,%f) before=%f", low, high, before)
	}
	// 90% 带端校验：该带难度下期望正确率应≈90%
	if e := tr.Expected("go", low); math.Abs(e-0.9) > 0.03 {
		t.Fatalf("low 端期望正确率应≈90%%: %f", e)
	}
}

// 冷启动透明度：games 计数正确；>10 局后 K 降（稳定期）。
func TestGamesAndKDecay(t *testing.T) {
	tr := New()
	for i := 0; i < 15; i++ {
		tr.Answer("python", 1200, i%2 == 0)
	}
	d := tr.Report()[0]
	if d.Games != 15 {
		t.Fatalf("games 应=15: %d", d.Games)
	}
	// 稳定期：连对 3 次同难度，涨幅应明显小于 K=64 的理论上限
	for i := 0; i < 3; i++ {
		before := tr.Rating("python")
		tr.Answer("python", 1400, true)
		gain := tr.Rating("python") - before
		if gain > 64*0.9 {
			t.Fatalf("games>10 后单次涨幅应受 K=24 约束: %f", gain)
		}
	}
}
