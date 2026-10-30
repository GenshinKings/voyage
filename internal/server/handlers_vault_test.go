package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 笔记仓库：设一个真目录后，笔记数要跟着变；路径不存在要 400；清空要退回默认 notes/。
func TestVaultRoute(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerVaultHandlers(mux)

	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "笔记一.md"), []byte("# 一"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "笔记二.md"), []byte("# 二"), 0o644); err != nil {
		t.Fatal(err)
	}

	do := func(method, body string) map[string]any {
		rec := httptest.NewRecorder()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, "/api/vault", nil)
		} else {
			req = httptest.NewRequest(method, "/api/vault", strings.NewReader(body))
		}
		mux.ServeHTTP(rec, req)
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m
	}

	// 默认状态：空笔记
	got := do("GET", "")
	if got["notes"] != float64(0) {
		t.Fatalf("默认应 0 篇，实际 %v", got["notes"])
	}

	// 设仓库
	body, _ := json.Marshal(map[string]string{"path": vault})
	got = do("POST", string(body))
	if got["ok"] != true || got["notes"] != float64(2) {
		t.Fatalf("设仓库后应看到 2 篇：%v", got)
	}
	if s.NotesDir != vault {
		t.Fatalf("NotesDir 应指到仓库，实际 %s", s.NotesDir)
	}
	if s.Tutor != nil && s.Tutor.NotesDir != vault {
		t.Fatal("Tutor.NotesDir 没同步，巡检会看旧目录")
	}

	// 不存在的路径要拒
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/vault", strings.NewReader(`{"path":"Z:\\nope\\nope"}`)))
	if rec.Code != 400 {
		t.Fatalf("不存在的目录应 400，实际 %d", rec.Code)
	}

	// 清空 -> 退回默认
	got = do("POST", `{"path":""}`)
	if got["ok"] != true || s.NotesDir != s.defaultNotesDir {
		t.Fatalf("清空后应退回默认目录：%v", got)
	}
}

// 落盘后重启能读回来（仓库被挪走时静默退回默认，不阻塞启动）。
func TestVaultPersistAndMissing(t *testing.T) {
	dir := t.TempDir()
	vault := t.TempDir()
	os.WriteFile(filepath.Join(vault, "a.md"), []byte("x"), 0o644)

	s := NewServer(dir)
	mux := http.NewServeMux()
	s.registerVaultHandlers(mux)
	body, _ := json.Marshal(map[string]string{"path": vault})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/vault", strings.NewReader(string(body))))

	s2 := NewServer(dir)
	if s2.NotesDir != vault {
		t.Fatalf("重启后应恢复仓库 %s，实际 %s", vault, s2.NotesDir)
	}

	// 把仓库删掉，重启应退回默认而不是炸掉
	os.RemoveAll(vault)
	s3 := NewServer(dir)
	if s3.NotesDir != s3.defaultNotesDir {
		t.Fatalf("仓库不存在时应退回默认，实际 %s", s3.NotesDir)
	}
}
