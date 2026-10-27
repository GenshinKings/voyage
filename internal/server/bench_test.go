package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 小穗四轮·性能基线（第四轮-性能与教育学.md）：ProtectStateAPI/GraphAPI <10ms 阈值
// 改动前跑一次基线，改动后同跑对比——把数字贴进 commit。

func benchServer(tb testing.TB) *Server {
	dir := tb.TempDir()
	// 默认 llm.json（无真实 key 也能走完整 handler 序列化路径；benchmark 不调 LLM）
	_ = os.WriteFile(filepath.Join(dir, "llm.json"), []byte(`{"active":"default","profiles":{"default":{"name":"default","baseUrl":"http://127.0.0.1:9","key":"bench","model":"bench"}}}`), 0o600)
	s := NewServer(dir)
	if s.Tutor == nil {
		tb.Fatal("tutor nil")
	}
	return s
}

func benchmarkHandler(b *testing.B, path string) *Server {
	s := benchServer(b)
	h := s.Handlers()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != 200 {
			b.Fatalf("%s -> %d", path, r.Code)
		}
	}
	return s
}

func BenchmarkStateAPI(b *testing.B) { benchmarkHandler(b, "/api/state") }
func BenchmarkGraphAPI(b *testing.B) { benchmarkHandler(b, "/api/graph") }

// 阈值断言：单次响应 >10ms 即 fail（性能护栏第 4 条）
func TestSnapshotLatencyThreshold(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	s := benchServer(t)
	h := s.Handlers()
	for _, path := range []string{"/api/state", "/api/graph"} {
		start := time.Now()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		elapsed := time.Since(start)
		if elapsed > 10*time.Millisecond {
			t.Fatalf("%s 单次响应 %v > %v 阈值——性能护栏见 red", path, elapsed, 10*time.Millisecond)
		}
		t.Logf("%s 本次响应 %v（阈值 %v）", path, elapsed, 10*time.Millisecond)
	}
}
