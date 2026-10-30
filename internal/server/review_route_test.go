package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 没有可用模型时，闯关不该静默判过，而是进待判队列；由人（自己）判过之后才推进。
func TestRunSubmitFallsBackToReview(t *testing.T) {
	s := NewServer(t.TempDir()) // 没配模型
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)
	s.registerReviewHandlers(mux)

	post := func(path, body string) map[string]any {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}
	get := func(path string) map[string]any {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}

	post("/api/run/start", `{"templateId":"t1-fix-my-annoyance"}`)

	// 第一关 Expect 为空、没有模型 -> 转待判
	res := post("/api/run/submit", `{"output":"myTool v0.0.1 ready"}`)
	if res["ok"] != false {
		t.Fatalf("没有模型时不该判过: %v", res)
	}
	id, _ := res["pendingReview"].(string)
	if id == "" {
		t.Fatalf("应返回待判 id: %v", res)
	}
	if res["progress"] != "0/6" {
		t.Fatalf("转待判时进度不该推进: %v", res["progress"])
	}

	// 待判列表里有它
	list := get("/api/reviews")
	if list["count"] != float64(1) {
		t.Fatalf("待判应有 1 条: %v", list)
	}

	// 判不过：进度不动，记录里带差在哪
	bad := post("/api/reviews/verdict", `{"id":"`+id+`","pass":false,"where":"没看到启动成功","action":"把版本号打印出来"}`)
	if bad["ok"] != true {
		t.Fatalf("判定接口失败: %v", bad)
	}
	if bad["advanced"] != false {
		t.Fatalf("判不过不该推进: %v", bad["advanced"])
	}

	// 再来一次交作业 -> 又是一条待判，然后判过
	res2 := post("/api/run/submit", `{"output":"myTool v0.0.2 ready"}`)
	id2, _ := res2["pendingReview"].(string)
	if id2 == "" {
		t.Fatalf("第二次也该转待判: %v", res2)
	}
	good := post("/api/reviews/verdict", `{"id":"`+id2+`","pass":true}`)
	if good["ok"] != true || good["advanced"] != true {
		t.Fatalf("判过之后应推进进度: %v", good)
	}
	if got := get("/api/run/state"); got["progress"] != "1/6" {
		t.Fatalf("进度应到 1/6，实际 %v", got["progress"])
	}

	// 重复判同一条：404 JSON
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/reviews/verdict", strings.NewReader(`{"id":"`+id2+`","pass":true}`)))
	if rec.Code != 404 {
		t.Fatalf("重复判应 404，实际 %d", rec.Code)
	}
}
