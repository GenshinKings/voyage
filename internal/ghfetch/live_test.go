package ghfetch

import (
	"os"
	"strings"
	"testing"
)

// TestLiveTrending 真连诊断（默认跳过）：GHFETCH_LIVE=1 时跑。
func TestLiveTrending(t *testing.T) {
	if os.Getenv("GHFETCH_LIVE") != "1" {
		t.Skip("需要 GHFETCH_LIVE=1")
	}
	c := New(os.Getenv("GHFETCH_PROXY"))
	repos, err := c.Trending("")
	if err != nil {
		if b, _, _, gerr := c.get(c.BaseTrending, "text/html"); gerr == nil {
			os.WriteFile("trending-fail.html", b, 0644)
			t.Logf("失败页已存 trending-fail.html（%d bytes，Box-row=%d，artRe=%d）",
				len(b), strings.Count(string(b), "Box-row"), len(artRe.FindAllStringSubmatch(string(b), -1)))
		}
		t.Fatalf("trending: %v", err)
	}
	t.Logf("解析 %d 条，首条: %+v", len(repos), repos[0])
}

func TestLiveTrendingDump(t *testing.T) {
	if os.Getenv("GHFETCH_LIVE") != "1" {
		t.Skip("需要 GHFETCH_LIVE=1")
	}
	c := New(os.Getenv("GHFETCH_PROXY"))
	b, code, _, err := c.get(c.BaseTrending, "text/html")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile("trending-dump.html", b, 0644)
	t.Logf("HTTP %d, %d bytes, Box-row 计数: %d", code, len(b), strings.Count(string(b), "Box-row"))
}

func TestLiveTrendingRegex(t *testing.T) {
	if os.Getenv("GHFETCH_LIVE") != "1" {
		t.Skip("需要 GHFETCH_LIVE=1")
	}
	b, err := os.ReadFile("trending-dump.html")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("open: %d, close: %d, artRe 匹配: %d",
		strings.Count(string(b), `<article class="Box-row">`),
		strings.Count(string(b), `</article>`),
		len(artRe.FindAllStringSubmatch(string(b), -1)))
}
