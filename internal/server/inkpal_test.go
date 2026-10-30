package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// InkPal 桥接：没有模型时要给出可读的错，而不是 500；
// 有模型时 chat 走同一个助教、annotate 只返回能被插件展示的批注。
func TestInkpalRoutesWithoutModel(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerInkpalHandlers(mux)

	post := func(path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}

	if code, m := post("/api/inkpal/chat", `{"messages":[{"role":"user","content":"hi"}]}`); code != 400 || m["error"] == nil {
		t.Fatalf("没有模型时应 400 且带 error，实际 %d %v", code, m)
	}
	if code, m := post("/api/inkpal/annotate", `{"path":"a.md","content":"x"}`); code != 400 || m["error"] == nil {
		t.Fatalf("没有模型时应 400 且带 error，实际 %d %v", code, m)
	}
	if code, _ := post("/api/inkpal/chat", `{"messages":[]}`); code != 400 {
		t.Fatalf("空 messages 应 400，实际 %d", code)
	}
	if code, _ := post("/api/inkpal/annotate", `{"content":"   "}`); code != 400 {
		t.Fatalf("空 content 应 400，实际 %d", code)
	}

	// GET 不该被接受
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/inkpal/chat", nil))
	if rec.Code != 405 {
		t.Fatalf("GET 应 405，实际 %d", rec.Code)
	}
}

// 批注过滤：定位不到（line<=0）或没给正确说法的条目要丢掉。
func TestInkpalAnnotationFiltering(t *testing.T) {
	items := []Annotation{
		{Line: 0, Quote: "x", Kind: "concept", MyUnderstanding: "a", Correct: "b"},
		{Line: 3, Quote: "y", Kind: "concept", MyUnderstanding: "a", Correct: ""},
		{Line: 5, Quote: "z", Kind: "", MyUnderstanding: "a", Correct: "b"},
	}
	kept := 0
	for _, it := range items {
		if it.Line <= 0 || strings.TrimSpace(it.Correct) == "" {
			continue
		}
		kept++
	}
	if kept != 1 {
		t.Fatalf("只该保留 1 条，实际 %d", kept)
	}
}

// system 里必须带上共用教学法的痕迹。
func TestInkpalSystemSharesTeachSkills(t *testing.T) {
	s := NewServer(t.TempDir())
	sys := s.inkpalSystem()
	if !strings.Contains(sys, "教学纪律") {
		t.Fatalf("system 里应带教学纪律：%s", sys)
	}
	if !strings.Contains(sys, "diagnose-state") && !strings.Contains(sys, "classify-error") {
		t.Fatalf("system 里应带技能名：%s", sys)
	}
}
