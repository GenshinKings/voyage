package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// /api/skills：列表只回元信息，按名字才取正文（渐进披露）。
func TestSkillsRoutes(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerTeachHandlers(mux)

	// 列表
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/skills", nil))
	if rec.Code != 200 {
		t.Fatalf("列表状态码想 200 得到 %d", rec.Code)
	}
	var list struct {
		Skills []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Body        string `json:"body"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("不是合法 JSON：%v", err)
	}
	if len(list.Skills) < 5 {
		t.Fatalf("应至少 5 条技能，实际 %d", len(list.Skills))
	}
	for _, s := range list.Skills {
		if s.Name == "" || s.Description == "" {
			t.Fatalf("元信息不完整: %+v", s)
		}
		if s.Body != "" {
			t.Fatalf("列表不该带正文（渐进披露），%s 带上了", s.Name)
		}
	}

	// 取单条正文
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/skills?name=judge-verdict", nil))
	if rec.Code != 200 {
		t.Fatalf("单条状态码想 200 得到 %d", rec.Code)
	}
	var one struct {
		Skill struct {
			Name string `json:"name"`
			Body string `json:"body"`
		} `json:"skill"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Skill.Name != "judge-verdict" || one.Skill.Body == "" {
		t.Fatalf("单条技能内容不对: %+v", one.Skill)
	}

	// 不存在 -> 404 JSON
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/skills?name=nope", nil))
	if rec.Code != 404 {
		t.Fatalf("不存在应 404，实际 %d", rec.Code)
	}
	var e map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e["error"] == nil {
		t.Fatalf("404 应是 JSON 且带 error: %s", rec.Body.String())
	}
}
