package tutor

// 第六轮-P0-1：苏格拉底渐隐式引导——同一节课三种引导模式，随人切换，不随课程切换。
//
// 判据写成常量并注释来源（纪律：为什么是 3 和 2）：
//
//	升档=连续 3 次判定通过——两次通过可能是时运（环境运气/蒙对），第三次才说明方法开始内化；
//	3 也是刻意练习里「最小可判定样本」的下限，低于它换档会被单次噪声牵着走。
//	降档=连续 2 次未通过 / 点「我卡住了」——第一次错大概率是环境或手滑（第六轮-P0-2 误因分类正是为此），
//	第二次错才构成「理解缺口」信号，此时给完整样例（退回一档），避免习得性无助。
const (
	GuidePassStreak = 3
	GuideFailStreak = 2
)

// 引导档位：1 样例（完整例子+直接指令）→ 2 半提示（方向不给答案）→ 3 提问（苏格拉底）。
const (
	GuideSample = 1
	GuideHint   = 2
	GuideAsk    = 3
)

// GuideName 档位名（前端「今天三步·讲」小字与日志用）。
func GuideName(level int) string {
	switch level {
	case GuideAsk:
		return "提问"
	case GuideHint:
		return "半提示"
	default:
		return "样例"
	}
}

// NextGuide 纯函数：凑满阈值升/降**一级**并把连击清零——每档至少停留一次事件，
// 防止长连击跨档跳级（半提示还没体验过就跳到提问，渐隐失去中间态）。
func NextGuide(level, passStreak, failStreak int) int {
	if level < GuideSample {
		level = GuideSample
	}
	if level > GuideAsk {
		level = GuideAsk
	}
	if passStreak >= GuidePassStreak && level < GuideAsk {
		level++
		passStreak = 0
	}
	if failStreak >= GuideFailStreak && level > GuideSample {
		level--
		failStreak = 0
	}
	return level
}

// guidePrompt 把档位变成 system 指令（固定话术，写死可审计）。
func guidePrompt(level int) string {
	switch level {
	case GuideAsk:
		return "\n\n【引导档位·提问（第 3 档）】学习者最近连续通过——这一轮你只用提问引导（例：「你觉得这行执行完，变量还是原来的值吗？」），不直接给答案，不出现「你应该……」类直接指令；等他自己说出结论，说对了再收尾。"
	case GuideHint:
		return "\n\n【引导档位·半提示（第 2 档）】这一轮给方向不给答案：说出「你要找的东西在 A 里，想想它对 B 做了什么」式的对象与关系，但不代他写出成品。"
	default:
		return "\n\n【引导档位·样例（第 1 档）】新手或刚卡过——先给一个完整可运行的例子 + 直接指令（「照着这个改，把 X 换成 Y」），不许只提问悬空。"
	}
}
