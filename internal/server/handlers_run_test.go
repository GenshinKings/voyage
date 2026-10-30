package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 三个接口跑一遍：选模板 → 交作业（转待判 → 判过）→ 看进度。
func TestRunRoutesFlow(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)
	s.registerReviewHandlers(mux)

	do := func(path, body string) map[string]any {
		rec := httptest.NewRecorder()
		if body == "" {
			mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		} else {
			mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		}
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}

	tp := do("/api/run/templates", "")
	if list, _ := tp["templates"].([]any); len(list) != 4 {
		t.Fatalf("应返回 4 个模板，实际 %d", len(list))
	}
	if st := do("/api/run/state", ""); st["started"] != false {
		t.Fatal("未开始时应 started=false")
	}

	start := do("/api/run/start", `{"templateId":"t1-fix-my-annoyance"}`)
	if start["ok"] != true || start["progress"] != "0/6" {
		t.Fatalf("开始失败或进度不对：%v", start)
	}

	// 空提交：直接拒（连待判都不进）
	bad := do("/api/run/submit", `{"output":"   "}`)
	if bad["ok"] != false || bad["hint"] == nil {
		t.Fatalf("空提交应被拒并给出下一步：%v", bad)
	}

	// 有效产物：没有模型时进待判，不静默判过
	pending := do("/api/run/submit", `{"output":"myTool v0.0.1 ready"}`)
	if pending["ok"] != false {
		t.Fatalf("没有模型时不该直接判过：%v", pending)
	}
	id, _ := pending["pendingReview"].(string)
	if id == "" {
		t.Fatalf("应返回待判 id：%v", pending)
	}

	// 判过之后解锁下一关
	decided := do("/api/reviews/verdict", `{"id":"`+id+`","pass":true}`)
	if decided["ok"] != true || decided["advanced"] != true {
		t.Fatalf("判过之后应推进：%v", decided)
	}
	if st := do("/api/run/state", ""); st["progress"] != "1/6" {
		t.Fatalf("进度应到 1/6，实际 %v", st["progress"])
	}
}
