package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestChatCtxCancelAborts：页面关了（ctx 取消）就不该继续等、也不该重试。
func TestChatCtxCancelAborts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Key: "k", Model: "m"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 一开始就取消

	start := time.Now()
	_, err := c.ChatCtx(ctx, "sys", []Message{{Role: "user", Content: "hi"}})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ctx 已取消却返回成功")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("取消后仍然等了 %v（说明没短路）", elapsed)
	}
}

// TestChatJSONCtxStillWorks：加 ctx 之后 JSON 路径行为不变（围栏照样剥）。
func TestChatJSONCtxStillWorks(t *testing.T) {
	fence := strings.Repeat("`", 3)
	content := fence + "json\n{\"a\":1}\n" + fence

	respBody, err := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{"role": "assistant", "content": content},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBody)
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Key: "k", Model: "m"})
	out, err := c.ChatJSONCtx(context.Background(), "sys", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("ChatJSONCtx 失败: %v", err)
	}
	if out != `{"a":1}` {
		t.Fatalf("围栏没剥干净: %q", out)
	}
}
