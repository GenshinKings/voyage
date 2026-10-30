package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 错误出口必须是 JSON：前端 api() 靠 j.error 取人话，
// 之前 36 处 http.Error 返回纯文本，用户只会看到笼统的「请求失败」。
func TestErrorResponsesAreJSON(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerLearnHandlers(mux)
	s.registerRunHandlers(mux)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		code   int
		want   string
	}{
		{"复习卡不存在", "POST", "/api/complete-review", `{"id":"nope","rating":3}`, 404, "复习卡不存在"},
		{"rating 越界", "POST", "/api/complete-review", `{"id":"nope","rating":9}`, 404, "复习卡不存在"},
		{"开始未知模板", "POST", "/api/run/start", `{"templateId":"nope"}`, 400, "没有这个模板"},
		{"交作业但没开始", "POST", "/api/run/submit", `{"output":"x"}`, 400, "还没开始一个项目"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var req *http.Request
			if c.body != "" {
				req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			} else {
				req = httptest.NewRequest(c.method, c.path, nil)
			}
			mux.ServeHTTP(rec, req)

			if rec.Code != c.code {
				t.Fatalf("状态码想 %d 得到 %d", c.code, rec.Code)
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Fatalf("错误响应应是 JSON，实际 Content-Type=%q body=%q", ct, rec.Body.String())
			}
			var m map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
				t.Fatalf("错误响应不是合法 JSON：%v body=%q", err, rec.Body.String())
			}
			msg, _ := m["error"].(string)
			if !strings.Contains(msg, c.want) {
				t.Fatalf("错误文案想含 %q，实际 %q", c.want, msg)
			}
		})
	}
}
