package exercise

import (
	"os"
	"strings"
	"testing"

	"outpost2/internal/llm"
)

type mockClient struct {
	n        int
	chatOut  []string
	jsonOut  []string
	chatJSON func(system string, msgs []llm.Message) (string, error)
	chat     func(system string, msgs []llm.Message) (string, error)
}

func (m *mockClient) Chat(system string, msgs []llm.Message) (string, error) {
	m.n++
	return m.chatOut[m.n-1], nil
}
func (m *mockClient) ChatJSON(system string, msgs []llm.Message) (string, error) {
	m.n++
	return m.jsonOut[m.n-1], nil
}

func TestGenerateParseThreeShapes(t *testing.T) {
	shapes := []string{
		`[{"title":"算后验","topic":"贝叶斯","problem":"病率1%…","x":1}]`,
		`{"exercises":[{"title":"算后验","topic":"贝叶斯","problem":"病率1%…"}]}`,
		`{"questions":[{"title":"算后验","topic":"贝叶斯","problem":"病率1%…"}]}`,
	}
	for i, raw := range shapes {
		e := &Engine{Client: &mockClient{jsonOut: []string{raw}}}
		list, err := e.Generate([]string{"Fee Bayesian Stats / 贝叶斯公式"}, "")
		if err != nil {
			t.Fatalf("shape %d: %v", i, err)
		}
		if len(list) != 1 || list[0].Status != "active" || !strings.Contains(list[0].Messages[0].Content, "实操练习") {
			t.Fatalf("shape %d: %+v", i, list)
		}
		if list[0].ID == "" {
			t.Fatalf("shape %d: 空 ID", i)
		}
	}
}

func TestGenerateEmptyProblemSkipped(t *testing.T) {
	raw := `{"exercises":[{"title":"a","topic":"t","problem":""},{"title":"b","topic":"t","problem":"真题面"}]}`
	e := &Engine{Client: &mockClient{jsonOut: []string{raw}}}
	list, err := e.Generate([]string{"t"}, "")
	if err != nil || len(list) != 1 || list[0].Title != "b" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
}

func TestAnswerPassMarksDone(t *testing.T) {
	e := &Engine{Client: &mockClient{chatOut: []string{"先说结论：0.94 对。\n关键在你用了先验 1% 而不是报警率。\n\n[PASS]"}}}
	ex := &Exercise{ID: "x", Status: "active", Messages: []llm.Message{{Role: "assistant", Content: "题面"}}}
	reply, done, err := e.Answer(ex, "0.94，因为基率只有 1%")
	if err != nil || !done || ex.Status != "done" {
		t.Fatalf("done=%v status=%s err=%v", done, ex.Status, err)
	}
	if !strings.Contains(reply, "[PASS]") {
		t.Fatal("回复应含 [PASS]")
	}
	if len(ex.Messages) != 3 {
		t.Fatalf("消息数=%d，应追加 user+assistant", len(ex.Messages))
	}
}

func TestAnswerNoPassStaysActive(t *testing.T) {
	e := &Engine{Client: &mockClient{chatOut: []string{"方向对，但把 5% 误报的基数搞错了——想想 999 人里有多少人会触发。"}}}
	ex := &Exercise{ID: "x", Status: "active"}
	_, done, err := e.Answer(ex, "50%吧")
	if err != nil || done || ex.Status != "active" {
		t.Fatalf("done=%v status=%s err=%v", done, ex.Status, err)
	}
}

func TestGenerateNoClient(t *testing.T) {
	e := &Engine{}
	if _, err := e.Generate([]string{"t"}, ""); err == nil {
		t.Fatal("无 Client 应报错")
	}
}

func TestAnswerFileSubmission(t *testing.T) {
	path := t.TempDir() + "/solution.json"
	if err := os.WriteFile(path, []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Client: &mockClient{chatOut: []string{"格式对，capability 声明漏了 tools。\n\n[PASS]"}}}
	ex := &Exercise{ID: "f1", Status: "active"}
	_, _, err := e.Answer(ex, path)
	if err != nil {
		t.Fatal(err)
	}
	submitted := ex.Messages[0].Content
	if !strings.Contains(submitted, "以本地文件提交") || !strings.Contains(submitted, "jsonrpc") {
		t.Fatalf("提交内容应含文件内容，got: %s", submitted)
	}
	// 不存在的路径=当普通文本
	ex2 := &Exercise{ID: "f2", Status: "active"}
	e2 := &Engine{Client: &mockClient{chatOut: []string{"还没写完，继续。\n再看看 params。"}}}
	if _, _, err := e2.Answer(ex2, "Z:/不存在的路径/xx.json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ex2.Messages[0].Content, "以本地文件提交") {
		t.Fatal("不存在的路径不应按文件处理")
	}
}
