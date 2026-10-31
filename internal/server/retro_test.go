package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voyage/internal/run"
)

// 复盘：类型必须合法、必须写「下次怎么办」，记下来后能统计出主导卡点。
func TestRetroRoutes(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "复盘测试",
		Tier:      run.TierTool,
		Stages:    []run.Stage{{ID: "s1", Goal: "g", Check: "c"}},
		Results:   map[string]string{},
	}

	post := func(body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/retro", strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}

	// 合法
	if code, m := post(`{"stageId":"s1","kind":"env","note":"先看端口有没有被占用"}`); code != 200 || m["ok"] != true {
		t.Fatalf("该记录成功：%d %v", code, m)
	}
	// 类型非法
	if code, _ := post(`{"stageId":"s1","kind":"whatever","note":"x"}`); code != 400 {
		t.Fatalf("非法类型应 400，实际 %d", code)
	}
	// 没写做法
	if code, _ := post(`{"stageId":"s1","kind":"concept","note":"   "}`); code != 400 {
		t.Fatalf("空 note 应 400，实际 %d", code)
	}
	// 缺 stageId
	if code, _ := post(`{"kind":"concept","note":"x"}`); code != 400 {
		t.Fatalf("缺 stageId 应 400，实际 %d", code)
	}

	// 再记一次环境类，主导卡点应该是 env
	post(`{"stageId":"s2","kind":"env","note":"先启动服务再看报错"}`)
	if got := s.RunState.DominantKind(); got != "env" {
		t.Fatalf("主导卡点应为 env，实际 %q", got)
	}
	if c := s.RunState.RetroCount()["env"]; c != 2 {
		t.Fatalf("env 应 2 次，实际 %d", c)
	}

	// state 里能看到复盘
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/run/state", nil))
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["retro"] == nil {
		t.Fatalf("state 应带 retro：%v", st)
	}
}
