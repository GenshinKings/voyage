// tools.go — 助教的工具注册表与 agent 循环（function calling）。
//
// 设计（用户 2026-10-25 决策：接入的助教会知道怎么抓）：
//   - Tool 是「名字+描述+JSON schema+执行函数」；server 侧组装（GitHub 抓取/入库/阅读），
//     tutor 只管跑循环——工具与教学解耦。
//   - runAgent：模型可连续调用工具直到给出最终回复；客户端不支持 function calling
//     （mock/旧模型）自动退化为普通 Chat——教学流程永不因工具层而中断。
package tutor

import (
	"context"
	"encoding/json"
	"fmt"

	"voyage/internal/llm"
)

// Tool 助教可调用的一个工具。
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON schema（parameters）
	Fn          func(args json.RawMessage) (string, error)
}

func (t Tool) wire() llm.Tool {
	return llm.Tool{Name: t.Name, Description: t.Description, Parameters: t.Schema}
}

// maxToolRounds 单次回复内工具循环上限（防失控；到顶强制出最终回复）。
const maxToolRounds = 8

// runAgent 带工具循环的对话：返回最终文本回复与完整对话（含工具调用帧——调用方持久化）。
// 工具执行失败不中断——错误文本作为工具结果回给模型，让它自行调整。
func (t *Tutor) runAgent(ctx context.Context, system string, msgs []llm.Message, tools []Tool) (string, []llm.Message, error) {
	fallback := func() (string, []llm.Message, error) {
		out, err := llm.ChatWith(ctx, t.Client, system, msgs)
		conv := append(append([]llm.Message{}, msgs...), llm.Message{Role: "assistant", Content: out})
		return out, conv, err
	}
	if len(tools) == 0 {
		return fallback()
	}
	tc, ok := t.Client.(llm.ToolClient)
	if !ok {
		return fallback()
	}
	wire := make([]llm.Tool, 0, len(tools))
	byName := map[string]Tool{}
	for _, tl := range tools {
		wire = append(wire, tl.wire())
		byName[tl.Name] = tl
	}
	conv := append([]llm.Message{}, msgs...)
	for round := 0; round < maxToolRounds; round++ {
		content, calls, err := tc.ChatTools(system, conv, wire)
		if err != nil {
			return "", conv, err
		}
		if len(calls) == 0 {
			conv = append(conv, llm.Message{Role: "assistant", Content: content})
			return content, conv, nil
		}
		// assistant 帧一次性带全 tool_calls（不用指针回填——append 扩容会拷贝旧值，指针别名会丢帧）
		asst := llm.Message{Role: "assistant", Content: content}
		for _, c := range calls {
			asst.ToolCalls = append(asst.ToolCalls, llm.ToolCall{ID: c.ID, Name: c.Name, ArgsRaw: c.ArgsRaw})
		}
		conv = append(conv, asst)
		for _, c := range calls {
			out := t.execTool(byName, c)
			if out == "" { // tool 帧的 content 必须是字符串（空串会被 omitempty 变 null，服务端拒收）
				out = "（工具执行成功但无输出）"
			}
			conv = append(conv, llm.Message{Role: "tool", ToolCallID: c.ID, Content: out})
		}
	}
	// 轮数用尽：不给工具，强制收口
	content, _, err := tc.ChatTools(system+"\n（工具轮数已用尽——不要再调用工具，直接给出最终回复。）", conv, nil)
	if err != nil {
		return "", conv, err
	}
	conv = append(conv, llm.Message{Role: "assistant", Content: content})
	return content, conv, nil
}

// execTool 执行一次工具调用（panic 与错误都转成结果文本）。
func (t *Tutor) execTool(byName map[string]Tool, c llm.ToolCall) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具内部错误: %v", r)
		}
	}()
	tl, ok := byName[c.Name]
	if !ok {
		return fmt.Sprintf("未知工具 %s", c.Name)
	}
	out, err := tl.Fn(json.RawMessage(c.ArgsRaw))
	if err != nil {
		return "工具执行失败: " + err.Error()
	}
	return out
}
