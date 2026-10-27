package tutor

import "strings"

// 误因类别（第六轮-P0-2）：学习者模型的第一块积木。
const (
	CauseConcept   = "concept"   // 概念没懂
	CauseCondition = "condition" // 看漏条件
	CauseEnv       = "env"       // 环境/报错相关
	CauseSlip      = "slip"      // 手滑/拼写
)

// CauseUnknown 收不到合法标签时的兜底——宁可记 unknown 也不静默丢信号。
const CauseUnknown = "unknown"

// ParseCause 从判定回复里取出 [CAUSE:类别|证据]。
// 没有标签/类别非法 → 空 reason（调用方决定记 unknown 还是忽略）。
func ParseCause(reply string) (reason, evidence string) {
	idx := strings.LastIndex(reply, "[CAUSE:")
	if idx < 0 {
		return "", ""
	}
	rest := reply[idx+len("[CAUSE:"):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return "", ""
	}
	body := strings.TrimSpace(rest[:end])
	bar := strings.Index(body, "|")
	if bar < 0 {
		return strings.TrimSpace(body), ""
	}
	return strings.TrimSpace(body[:bar]), strings.TrimSpace(body[bar+1:])
}

// ValidCause 类别合法性（白名单只有四种，防 LLM 口嗨新标签污染分布）。
func ValidCause(r string) bool {
	switch r {
	case CauseConcept, CauseCondition, CauseEnv, CauseSlip:
		return true
	}
	return false
}
