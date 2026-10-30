package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 三个接口跑一遍：选模板 → 交作业（不过/过）→ 看进度。
func TestRunRoutesFlow(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	do := func(path, body string) map[string]any {
		var resp *httptest.ResponseRecorder
		if body == "" {
			resp = httptest.NewRecorder()
			mux.ServeHTTP(resp, httptest.NewRequest("GET", path, nil))
		} else {
			resp = httptest.NewRecorder()
			mux.ServeHTTP(resp, httptest.NewRequest("POST", path, strings.NewReader(body)))
		}
		var m map[string]any
		_ = json.Unmarshal(resp.Body.Bytes(), &m)
		return m
	}

	// 模板列表
	tp := do("/api/run/templates", "")
	list, _ := tp["templates"].([]any)
	if len(list) != 4 {
		t.Fatalf("应返回 4 个模板，实际 %d", len(list))
	}

	// 没开始时查进度
	if st := do("/api/run/state", ""); st["started"] != false {
		t.Fatal("未开始时应 started=false")
	}

	// 开始
	start := do("/api/run/start", `{"templateId":"t1-fix-my-annoyance"}`)
	if start["ok"] != true {
		t.Fatalf("开始失败：%v", start)
	}
	if start["progress"] != "0/6" {
		t.Fatalf("初始进度应为 0/6，实际 %v", start["progress"])
	}

	// 交空作业：应被拒且给 hint
	bad := do("/api/run/submit", `{"output":"   "}`)
	if bad["ok"] != false || bad["hint"] == nil {
		t.Fatalf("空提交应被拒并给出下一步：%v", bad)
	}

	// 交有效作业：过关
	good := do("/api/run/submit", `{"output":"myTool v0.0.1 ready"}`)
	if good["ok"] != true {
		t.Fatalf("有效提交应过关：%v", good)
	}
	if good["progress"] != "1/6" {
		t.Fatalf("应推进到 1/6，实际 %v", good["progress"])
	}

	// 进度接口能看到当前关
	st := do("/api/run/state", "")
	if st["started"] != true || st["progress"] != "1/6" {
		t.Fatalf("进度接口不对：%v", st)
	}
}
