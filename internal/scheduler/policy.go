package scheduler

// policy.go — 计划形态的 bandit 策略层（宪法 §3.1 + 六公理 4/6）。
//
// 自报精力是硬约束（Low 永远保养——bandit 无权推翻身体），Normal/High 时
// 由 Thompson bandit 在历史反馈中学习「该形态对该学习者今天灵不灵」。
// 奖励映射（「学会了且想继续学」的可计算近似）：
//   完成且自报「还想来」→ 1.0；完成但「今天够了」→ 0.6；未完成 → 0.1。
// 愧疚拉回的会话不得作为正向样本喂入（宪法案例零）。

import "voyage/internal/bandit"

type PlanShape string

const (
	ShapeStandard PlanShape = "standard" // 2 片推进
	ShapeSprint   PlanShape = "sprint"   // 3 片推进
	ShapeMaintain PlanShape = "maintain" // 0 片推进（保养日）
)

// ResolveShape 合成自报精力与 bandit 学习：Low 硬约束直通保养；
// Normal 时 bandit 可在 standard/maintain 间调；High 时在 sprint/standard 间调。
// 键纪律：Pick 与 SettleDay 使用同一个 context 键——键不一致=学习信号写丢
// （本次 P30 集成测试抓出的真 bug）。
func ResolveShape(self Energy, b *bandit.Bandit, context string) PlanShape {
	switch self {
	case EnergyLow:
		return ShapeMaintain
	case EnergyHigh:
		arms := []string{string(ShapeSprint), string(ShapeStandard)}
		return PlanShape(arms[b.Pick(context, arms)])
	default:
		arms := []string{string(ShapeStandard), string(ShapeMaintain)}
		return PlanShape(arms[b.Pick(context, arms)])
	}
}

// SettleDay 日终结算：completed=当日清单完成过半；mood=会话末自报。
func SettleDay(b *bandit.Bandit, context string, shape PlanShape, completed bool, mood string) {
	reward := 0.1
	switch {
	case completed && mood == "还想来":
		reward = 1.0
	case completed:
		reward = 0.6
	}
	b.Settle(context, string(shape), reward)
}

// applyShape 把形态翻译为 EffectiveEnergy（复用 v1 的精力配额机制）。
func applyShape(self Energy, shape PlanShape) Energy {
	switch shape {
	case ShapeSprint:
		return EnergyHigh
	case ShapeMaintain:
		return EnergyLow
	default:
		return EnergyNormal
	}
}
