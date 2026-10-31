package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voyage/internal/run"
)

// 「我不知道从哪下手」：没模型时也给一条静态台阶，而且降级结果会存住。
func TestHelpGivesScaffold(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerHelpHandlers(mux)

	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "学 docker",
		Tier:      run.TierTool,
		Stages: []run.Stage{{
			ID:    "s1",
			Goal:  "用 docker 跑一个容器",
			Check: "docker ps 能看到容器在跑",
		}},
		Results: map[string]string{},
		Started: "2026-10-31T23:00:00+08:00",
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/help", strings.NewReader(`{}`)))
	if rec.Code != 200 {
		t.Fatalf("应 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	text, _ := resp["text"].(string)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("应该给出台阶：%v", resp)
	}
	// 存住了：第二次直接命中缓存
	if s.RunState.Scaffold["s1"] == "" {
		t.Fatal("台阶应该存在这一关上")
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/help", strings.NewReader(`{}`)))
	var again map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &again)
	if again["cached"] != true {
		t.Fatalf("第二次应走缓存：%v", again)
	}
}

// 没有进行中的任务时不该乱给台阶。
func TestHelpNeedsRun(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerHelpHandlers(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/help", strings.NewReader(`{}`)))
	if rec.Code != 400 {
		t.Fatalf("没任务应 400，实际 %d", rec.Code)
	}
}
