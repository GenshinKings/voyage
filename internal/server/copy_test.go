package server

// copy_test.go — 第八轮-P1-1 验收：文案映射表每个 key 都有值；规格五条白话逐字锁定。
import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCopyTableEveryKeyHasValue(t *testing.T) {
	var m map[string]string
	if err := json.Unmarshal(copyJSON, &m); err != nil {
		t.Fatalf("copy.json 应可解析: %v", err)
	}
	if len(m) == 0 {
		t.Fatal("copy.json 不应为空")
	}
	for k, v := range m {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			t.Fatalf("每个 key 都必须有非空值: %q=%q", k, v)
		}
	}
}

// 规格点名的五条映射（第八轮需求 P1-1），逐字锁定——改文案要先改需求。
func TestCopySpecFivePhrases(t *testing.T) {
	exact := map[string]string{
		"guide.sample":    "我先给你看个例子",
		"guide.hint":      "我提示你一点",
		"guide.ask":       "你先说说看",
		"verdict.partial": "半懂也记下了，再答一次就过",
		"level":           "我讲得多细",
	}
	for k, want := range exact {
		if got := copyTable[k]; got != want {
			t.Fatalf("key %q 应为 %q，got %q", k, want, got)
		}
	}
}

// /api/state 下发 copy 表（前端 STATE.copy 引用 key）。
func TestCopyServedViaState(t *testing.T) {
	s := NewServer(t.TempDir())
	h := s.Handlers()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/state", nil))
	if r.Code != 200 {
		t.Fatalf("/api/state → %d", r.Code)
	}
	var out struct {
		Copy map[string]string `json:"copy"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil || len(out.Copy) == 0 {
		t.Fatalf("state 应携带 copy 表: %v %v", err, out.Copy)
	}
}
