package tutor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voyage/internal/llm"
	"voyage/internal/notes"
	"voyage/internal/reflib"
)

// toolMockClient 先回一次工具调用（toolTool），再回最终文本——模拟 function calling 两轮。
type toolMockClient struct {
	llm.ChatClient // 内嵌：Chat/ChatJSON 用不到（实现 ToolClient 走 ChatTools）
	rounds         int
	calls          []llm.ToolCall
}

func (m *toolMockClient) ChatTools(system string, msgs []llm.Message, tools []llm.Tool) (string, []llm.ToolCall, error) {
	m.rounds++
	if m.rounds == 1 {
		args, _ := json.Marshal(map[string]string{"title": "演示仓库", "digest": "学完能跑通一次演示", "content": "# Demo\n正文"})
		return "", []llm.ToolCall{{ID: "call-1", Name: "kb_save", ArgsRaw: string(args)}}, nil
	}
	// 第二轮：应已收到工具结果
	last := msgs[len(msgs)-1]
	if last.Role != "tool" || !strings.Contains(last.Content, "已入库") {
		return "", nil, &wantToolResult{got: last.Role + ":" + last.Content}
	}
	return "进货完成：存了 1 篇。", nil, nil
}

type wantToolResult struct{ got string }

func (w *wantToolResult) Error() string { return "期望 tool 结果，收到 " + w.got }

func kbSaveTool(dir string) Tool {
	return Tool{
		Name: "kb_save", Description: "存入参考库",
		Schema: map[string]any{"type": "object"},
		Fn: func(args json.RawMessage) (string, error) {
			var req struct{ Title, Digest, Content string }
			if err := json.Unmarshal(args, &req); err != nil {
				return "", err
			}
			d := reflib.Doc{ID: "test-" + reflib.SanitizeID(req.Title), Title: req.Title, Digest: req.Digest}
			if err := reflib.Add(dir, d, req.Content); err != nil {
				return "", err
			}
			return "已入库教学参考库：" + d.ID, nil
		},
	}
}

func TestRunAgentToolLoop(t *testing.T) {
	dir := t.TempDir()
	refDir := reflib.Ensure(dir)
	mc := &toolMockClient{}
	tu := &Tutor{Dir: dir, Client: mc, RefDir: refDir}
	final, conv, err := tu.runAgent("SYS", []llm.Message{{Role: "user", Content: "进货"}}, []Tool{kbSaveTool(refDir)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(final, "进货完成") {
		t.Fatalf("最终回复不对: %q", final)
	}
	// 完整对话：user → assistant(带tool_calls) → tool → assistant(最终)
	if len(conv) != 4 || conv[1].ToolCalls == nil || conv[2].Role != "tool" {
		t.Fatalf("工具帧结构不对: %+v", conv)
	}
	// 工具真执行了：参考库有货、导航自动更新
	if idx := reflib.LoadIndex(refDir); len(idx.Docs) != 1 {
		t.Fatalf("参考库应有 1 篇: %+v", idx.Docs)
	}
	if _, err := os.Stat(filepath.Join(refDir, reflib.NavFile)); err != nil {
		t.Fatal("导航应已更新")
	}
}

func TestRunAgentFallbackNoTools(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{"普通回复"}}
	tu := &Tutor{Dir: dir, Client: mc}
	final, _, err := tu.runAgent("SYS", []llm.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil || final != "普通回复" {
		t.Fatalf("无工具应退化 Chat: %q %v", final, err)
	}
	// ChatClient 不实现 ToolClient + 有工具 → 同样退化
	if _, _, err := (&Tutor{Dir: dir, Client: mc}).runAgent("SYS", []llm.Message{{Role: "user", Content: "hi"}}, []Tool{kbSaveTool(dir)}); err != nil {
		t.Fatalf("不支持工具的客户端应降级: %v", err)
	}
}

func TestCurateOnce(t *testing.T) {
	dir := t.TempDir()
	refDir := reflib.Ensure(dir)
	mc := &toolMockClient{}
	tu := &Tutor{Dir: dir, Client: mc, RefDir: refDir}
	report, err := tu.CurateOnce("- 当前在学科目：机器学习\n", []Tool{kbSaveTool(refDir)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "进货完成") {
		t.Fatalf("报告不对: %q", report)
	}
	// 无工具应报错
	if _, err := tu.CurateOnce("x", nil); err == nil {
		t.Fatal("无工具应报错")
	}
}

func TestReviewNotes(t *testing.T) {
	dir := t.TempDir()
	notesDir := notes.Ensure(dir)
	mc := &mockClient{replies: []string{"✅ n1.md：梯度下降说法正确。\n⚠️ 建议补充学习率的作用。\n总评：笔记习惯好，补一条例子。"}}
	tu := &Tutor{Dir: dir, Client: mc, NotesDir: notesDir}
	// 空笔记目录应报错
	if _, err := tu.ReviewNotes("- 在学科目：机器学习\n"); err == nil {
		t.Fatal("无笔记应报错")
	}
	if err := os.WriteFile(filepath.Join(notesDir, "n1.md"), []byte("梯度下降就是往低处走"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := tu.ReviewNotes("- 在学科目：机器学习\n- 《线性回归》（已收尾）：能说清梯度下降\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "总评") {
		t.Fatalf("报告不对: %q", report)
	}
	// 无 LLM 应报错
	if _, err := (&Tutor{Dir: dir, NotesDir: notesDir}).ReviewNotes("x"); err == nil {
		t.Fatal("无 LLM 应报错")
	}
}

func TestDirectionProposalVoid(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{
		`{"nodes":[{"title":"A","brief":"b"}]}`,
		"方向清晰。[PROPOSE]{\"subject\":\"推荐系统入门\",\"lessons\":[{\"title\":\"是什么\",\"brief\":\"b\"}]}",
		"了解了，测试内容不算数。[PROPOSE-VOID] 你想成为 FDE 工程师——先问一句：RAG 还是 Agent？",
	}}
	tu := &Tutor{Dir: dir, Client: mc}
	// 第一轮：产出提案
	_, proposal, err := tu.DirectionSend("想学推荐系统")
	if err != nil || proposal == nil {
		t.Fatalf("应产出提案: %v", err)
	}
	// 第二轮：用户转向 → 顾问输出 [PROPOSE-VOID] → 提案作废
	_, proposal, err = tu.DirectionSend("上述为测试内容。我想成为 FDE 工程师")
	if err != nil {
		t.Fatal(err)
	}
	if proposal != nil {
		t.Fatalf("转新方向后旧提案应作废: %+v", proposal)
	}
	if s := tu.LoadDirection(); s.Proposal != nil {
		t.Fatal("direction.json 里提案应已清空")
	}
}
