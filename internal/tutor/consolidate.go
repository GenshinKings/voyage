// consolidate.go — 第七轮：夜间巩固（睡眠重放的工程化）。
//
// 每天低峰一次：当天对话 + 已有记忆 → LLM 输出操作列表（ADD/UPDATE/DELETE/NOOP）
// → server 侧逐条过 schema 校验后应用。tutor 只管与模型对话，库操作全在 server/memory.go。
package tutor

import (
	"encoding/json"
	"fmt"

	"voyage/internal/llm"
)

// MemOp 巩固操作。ADD 必带 text/kind/source/confidence；UPDATE/DELETE 带 id。
// ts 与条目 id 由 server 侧生成——模型不给写 id 的权限（防撞车/伪造）。
type MemOp struct {
	Op         string  `json:"op"` // ADD | UPDATE | DELETE | NOOP
	Text       string  `json:"text,omitempty"`
	Kind       string  `json:"kind,omitempty"`
	Source     string  `json:"source,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	ID         string  `json:"id,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

const consolidateSystem = `你是前哨 AI 助教的记忆巩固环节（模拟睡眠重放）：把今天的对话整理成对长期记忆库的操作列表。

输入是「今天的对话」（每场标注了会话 id）与「已有记忆」（每条标注了 id）。

规则：
- 只记稳定的事实/偏好/承诺/印象/结论——不记寒暄、不出现在对话里的猜测不记
- 与已有条目矛盾 → UPDATE 那一条（给 id 与新说法；旧值系统自动进 history，绝不允许新旧两条相反说法并存）
- 与已有条目重复 → NOOP（reason 写和哪条重复）
- 已过时且被明确取代的条目 → DELETE（给 id 与 reason）
- 目标 5~7 条 ADD；今天确实没什么值得记的，就只输出 NOOP——禁止凑数，禁止编造
- 每条 ADD 必须齐备：text（一句话）、kind（fact|preference|commitment|impression|conclusion 五选一）、source（"session:会话id@日期"，取自对话标注）、confidence（0~1 小数，你的把握）

输出严格 JSON（无围栏）：
{"ops":[
 {"op":"ADD","text":"...","kind":"fact","source":"session:xxxx@2026-10-28","confidence":0.9},
 {"op":"UPDATE","id":"m-...","text":"...","source":"session:...","confidence":0.95},
 {"op":"DELETE","id":"m-...","reason":"已被新事实取代"},
 {"op":"NOOP","reason":"与 m-... 重复"}]}`

// Consolidate 跑一次巩固对话，返回操作列表（不落库——应用与校验在 server 侧）。
func (t *Tutor) Consolidate(conversations, existing string) ([]MemOp, error) {
	if t.Client == nil {
		return nil, fmt.Errorf("LLM 未配置")
	}
	user := "已有记忆：\n" + existing + "\n\n今天的对话：\n" + conversations + "\n\n开始巩固。"
	raw, err := t.Client.ChatJSON(t.withAgents(consolidateSystem), []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Ops []MemOp `json:"ops"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("巩固 JSON 解析失败: %w", err)
	}
	return out.Ops, nil
}
