package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 教学指标接口：没有数据也要能安全返回（前端不能因为没数据就崩）。
func TestStatsRouteEmpty(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("状态码想 200 得到 %d", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("不是合法 JSON：%v body=%q", err, rec.Body.String())
	}
	if _, ok := m["judged"]; !ok {
		t.Fatalf("应返回 judged 字段，实际 %v", m)
	}
}
