package tutor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"voyage/internal/llm"
)

// 关掉页面（ctx 取消）之后，助教这条链不该继续等模型。
func TestSendHonorsCanceledContext(t *testing.T) {
	hit := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	tu := &Tutor{Dir: dir, Client: llm.NewClient(llm.Config{BaseURL: srv.URL, Key: "k", Model: "m"})}
	lesson := Lesson{ID: "ctx-1", Subject: "测试", Title: "取消", Brief: "能说出变量是什么", Status: "active", Order: 1}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := tu.Send(ctx, lesson, "答")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ctx 已取消却成功返回")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("取消后仍等了 %v（说明没有短路）", elapsed)
	}
}

// StartOrResume 也应尊重取消（它同样会调模型生成开场）。
func TestStartOrResumeHonorsCanceledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	tu := &Tutor{Dir: t.TempDir(), Client: llm.NewClient(llm.Config{BaseURL: srv.URL, Key: "k", Model: "m"})}
	lesson := Lesson{ID: "ctx-2", Subject: "测试", Title: "取消", Brief: "能说出变量是什么", Status: "active", Order: 1}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, _, err := tu.StartOrResume(ctx, lesson)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ctx 已取消却成功返回")
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("取消后仍等了 %v", elapsed)
	}
}
