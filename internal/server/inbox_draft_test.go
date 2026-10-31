package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 起草接口：没模型要让用户看得懂去哪配；id 不存在要 404；GET 要 405。
func TestInboxDraftValidation(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerInboxHandlers(mux)
	s.registerInboxDraftHandler(mux)

	post := func(path, body string) (int, map[string]any) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}

	// 收一条，拿到 id
	_, added := post("/api/inbox", `{"text":"选品表要手工抄进 Excel"}`)
	item, _ := added["item"].(map[string]any)
	id, _ := item["id"].(string)
	if id == "" {
		t.Fatalf("没收下条目：%v", added)
	}

	// 没模型：400 + 可读的错
	if code, m := post("/api/inbox/draft", `{"id":"`+id+`"}`); code != 400 || m["error"] == nil {
		t.Fatalf("没模型应 400 带 error，实际 %d %v", code, m)
	}
	// 缺 id
	if code, _ := post("/api/inbox/draft", `{}`); code != 400 {
		t.Fatalf("缺 id 应 400，实际 %d", code)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/inbox/draft", nil))
	if rec.Code != 405 {
		t.Fatalf("GET 应 405，实际 %d", rec.Code)
	}
}
