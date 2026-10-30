package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 打分与整库体检：没模型时给人话，输入不合法时 400，不吞错。
func TestInkpalVaultRoutesValidation(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerInkpalVaultHandlers(mux)

	post := func(path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}

	if code, m := post("/api/inkpal/score", `{"path":"a.md","content":"x"}`); code != 400 || m["error"] == nil {
		t.Fatalf("没模型应 400 带 error，实际 %d %v", code, m)
	}
	if code, _ := post("/api/inkpal/score", `{"content":"  "}`); code != 400 {
		t.Fatalf("空内容应 400，实际 %d", code)
	}
	if code, m := post("/api/inkpal/vault-review", `{"notes":[{"path":"a.md"}]}`); code != 400 || m["error"] == nil {
		t.Fatalf("没模型应 400 带 error，实际 %d %v", code, m)
	}
	if code, _ := post("/api/inkpal/vault-review", `{"notes":[]}`); code != 400 {
		t.Fatalf("空清单应 400，实际 %d", code)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/inkpal/score", nil))
	if rec.Code != 405 {
		t.Fatalf("GET 应 405，实际 %d", rec.Code)
	}
}

func TestClampScore(t *testing.T) {
	cases := map[int]int{-3: 0, 0: 0, 3: 3, 5: 5, 9: 5}
	for in, want := range cases {
		if got := clampScore(in); got != want {
			t.Fatalf("clampScore(%d)=%d，应为 %d", in, got, want)
		}
	}
}
