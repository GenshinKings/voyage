package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voyage/internal/run"
)

// 「我哪里还不确定」：提交时记下来，事后能对账，判断命中率算得出来。
func TestUncertainAndReconcile(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	post := func(path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}

	// 起一个只有一关、且验收可机器判的 Run（Expect 命中即过）
	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "测试任务",
		Tier:      run.TierTool,
		Stages: []run.Stage{{
			ID:     "s1",
			Goal:   "打印版本号",
			Check:  "输出里有版本号",
			Expect: []string{"v0.0.1"},
		}},
		Results: map[string]string{},
	}

	if code, m := post("/api/run/submit", `{"output":"v0.0.1 ready","uncertain":"不确定它退没退出"}`); code != 200 || m["ok"] != true {
		t.Fatalf("该过：%d %v", code, m)
	}
	if got := s.RunState.Uncertain["s1"]; got != "不确定它退没退出" {
		t.Fatalf("不确定没记下来：%q", got)
	}

	// 没有对账记录时命中率是 -1（未知，不是 0）
	if r := s.RunState.JudgementHit(); r != -1 {
		t.Fatalf("没对账时应 -1，实际 %v", r)
	}

	// 对账接口
	if code, m := post("/api/run/reconcile", `{"stageId":"s1","outcome":"was-problem"}`); code != 200 || m["ok"] != true {
		t.Fatalf("对账失败：%d %v", code, m)
	}
	if got := s.RunState.Reconcile["s1"]; got != "was-problem" {
		t.Fatalf("对账没存下来：%q", got)
	}
	if r := s.RunState.JudgementHit(); r != 1 {
		t.Fatalf("一次全是真问题，命中率应 1，实际 %v", r)
	}

	// 非法 outcome 要拒
	if code, _ := post("/api/run/reconcile", `{"stageId":"s1","outcome":"maybe"}`); code != 400 {
		t.Fatalf("非法 outcome 应 400，实际 %d", code)
	}
}
