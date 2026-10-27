package kb

import (
	"strings"
)

// fingerprint.go — 相似度闸门（宪法 §3.2 ②层纪律：助教理解不得带原书指纹）。
//
// v0 实现：字符 5-gram shingle 指纹 + Jaccard 相似度。纯标准库、无外部依赖。
// 判定：Jaccard ≥ 0.30 判为「贴近复制」（GateFailed）；原创综合通常 <0.05。
// 阈值取保守低值——宁可错杀重写，不可放过抄袭（改写抄袭是版权实质相似的高危形态）。

const shingleN = 5
const gateThreshold = 0.30

// Shingles 生成字符 n-gram 集合（去空白、小写——对抗「换个标点就算改写」）。
func Shingles(text string) map[string]struct{} {
	clean := normalize(text)
	r := []rune(clean)
	out := map[string]struct{}{}
	if len(r) < shingleN {
		if len(r) > 0 {
			out[clean] = struct{}{}
		}
		return out
	}
	for i := 0; i+shingleN <= len(r); i++ {
		out[string(r[i:i+shingleN])] = struct{}{}
	}
	return out
}

func normalize(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch c {
		case ' ', '\t', '\n', '\r', '，', '。', '、', '；', '：', '（', '）', '(', ')', ',', '.', ';', ':', '"', '\'', '“', '”', '‘', '’':
			// 去空白与标点：标点洗不改写实质
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// Jaccard 两个指纹集合的交并比。
func Jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// Similarity 便捷：文本 a 与 b 的 shingle Jaccard。
func Similarity(a, b string) float64 {
	return Jaccard(Shingles(a), Shingles(b))
}

// GateResult 相似度闸门结果。
type GateResult struct {
	Passed     bool    `json:"passed"`
	MaxJaccard float64 `json:"maxJaccard"`
	WorstURL   string  `json:"worstUrl"` // 最贴近的原文（重写时的对照对象）
	Threshold  float64 `json:"threshold"`
}

// GateMentorView ②层闸门：助教综合讲法 vs 其锚定的全部原文。
// 任何一篇 Jaccard ≥ 阈值 → 不通过（改写抄袭嫌疑，须重写）。
func GateMentorView(mentorText string, anchored []Source) GateResult {
	g := GateResult{Threshold: gateThreshold, Passed: true}
	mf := Shingles(mentorText)
	for _, s := range anchored {
		j := Jaccard(mf, Shingles(s.Excerpt))
		if j > g.MaxJaccard {
			g.MaxJaccard = j
			g.WorstURL = s.URL
		}
		if j >= gateThreshold {
			g.Passed = false
		}
	}
	return g
}
