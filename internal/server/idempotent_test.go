package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"voyage/internal/run"
)

// 连点两次交作业只该前进一关（幂等）。
func TestSubmitIdempotent(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "幂等",
		Tier:      run.TierTool,
		Stages: []run.Stage{
			{ID: "s1", Goal: "第一步", Check: "有输出", Expect: []string{"对"}},
			{ID: "s2", Goal: "第二步", Check: "有输出", Expect: []string{"对"}},
		},
		Results: map[string]string{},
		Started: "2026-10-31T23:00:00+08:00",
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/submit", strings.NewReader(`{"output":"对"}`)))
	if rec.Code != 200 {
		t.Fatalf("第一次应交成功：%d %s", rec.Code, rec.Body.String())
	}
	if got := s.RunState.Progress(); got != "1/2" {
		t.Fatalf("应到 1/2，实际 %s", got)
	}

	// 第二次：这一关已经过了，不许再判一次
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/submit", strings.NewReader(`{"output":"对"}`)))
	if s.RunState.Progress() != "1/2" {
		t.Fatalf("连点不该推进到 %s", s.RunState.Progress())
	}
}

// 并发交同一关：状态不能被打乱（单写者 + 幂等）。
func TestSubmitConcurrentNoDoubleAdvance(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "并发",
		Tier:      run.TierTool,
		Stages: []run.Stage{
			{ID: "s1", Goal: "a", Check: "有输出", Expect: []string{"对"}},
			{ID: "s2", Goal: "b", Check: "有输出", Expect: []string{"对"}},
			{ID: "s3", Goal: "c", Check: "有输出", Expect: []string{"对"}},
		},
		Results: map[string]string{},
		Started: "2026-10-31T23:00:00+08:00",
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/submit", strings.NewReader(`{"output":"对"}`)))
		}()
	}
	wg.Wait()

	if got := s.RunState.Progress(); got != "1/3" {
		t.Fatalf("8 个并发提交只该推进一关，实际 %s", got)
	}
	// 结果里只该有 s1
	if len(s.RunState.Results) != 1 {
		t.Fatalf("结果只该有一条，实际 %d", len(s.RunState.Results))
	}
}
