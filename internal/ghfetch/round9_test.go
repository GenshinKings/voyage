package ghfetch

// round9_test.go — 第九轮-P0-1 验收：ghfetch 调用点的用户文案不含技术细节，
// 技术现场（HTTP 码、页面片段）只在 Detail 层。
import (
	"strings"
	"testing"

	"voyage/internal/usererr"
)

// 用户文案泄漏黑名单（验收原话：不含 .json、http、C:\、/home/）。
var leaks = []string{".json", "http", "HTTP", "C:\\", "/home/"}

func assertCleanUserCopy(t *testing.T, err error, wantDetailFragment string) {
	t.Helper()
	if err == nil {
		t.Fatal("应有错误")
	}
	for _, l := range leaks {
		if strings.Contains(err.Error(), l) {
			t.Fatalf("用户文案不应含 %q: %q", l, err.Error())
		}
	}
	d, ok := usererr.DetailOf(err)
	if !ok || !strings.Contains(d, wantDetailFragment) {
		t.Fatalf("Detail 应含 %q，got %q（分层=%v）", wantDetailFragment, d, ok)
	}
}

// Trending 页 HTTP 错误：用户看人话，HTTP 码进 Detail。
func TestErrTrendingHTTP(t *testing.T) {
	assertCleanUserCopy(t, errTrendingHTTP(503), "trending 页 HTTP 503")
}

// Trending 解析为 0 条（页面结构变化）：用户不知道「前 200 字节」，日志知道。
func TestErrParseTrending(t *testing.T) {
	garbage := []byte("<html>新改版了，Box-row 没了</html>")
	_, err := parseTrending(garbage, 200)
	assertCleanUserCopy(t, err, "前 200 字节")
	// 技术细节保留完整现场
	d, _ := usererr.DetailOf(err)
	if !strings.Contains(d, "HTTP 200") || !strings.Contains(d, "新改版了") {
		t.Fatalf("Detail 应含 HTTP 码与页面片段: %q", d)
	}
}
