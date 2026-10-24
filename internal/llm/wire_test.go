package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDumpToolWire(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		captured = b
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"gh_trending","arguments":"{}"}}]}}]}`))
	}))
	defer srv.Close()
	c := &Client{Config: Config{BaseURL: srv.URL, Key: "k", Model: "m"}, HTTP: srv.Client()}
	tools := []Tool{{Name: "gh_trending", Description: "d", Parameters: map[string]any{"type": "object"}}}
	_, calls, err := c.ChatTools("SYS", []Message{{Role: "user", Content: "go"}}, tools)
	if err != nil || len(calls) != 1 {
		t.Fatalf("first: %v %v", calls, err)
	}
	// 模拟 runAgent 的第二跳：assistant(带tool_calls) + tool 结果
	msgs := []Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "c1", Name: "gh_trending", ArgsRaw: "{}"}}},
		{Role: "tool", ToolCallID: "c1", Content: "结果"},
	}
	if _, _, err := c.ChatTools("SYS", msgs, tools); err != nil {
		t.Fatal(err)
	}
	t.Logf("wire: %s", captured)
	var pretty map[string]any
	json.Unmarshal(captured, &pretty)
	out, _ := json.MarshalIndent(pretty, "", " ")
	t.Logf("pretty:\n%s", out)
}
