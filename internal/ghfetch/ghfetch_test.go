package ghfetch

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stub 起一个假 GitHub（trending HTML + api JSON + raw 文本），返回注入了桩地址的客户端。
// api 的值里可用 %BASE% 占位符（会被替换为桩地址，用于拼 download_url 等绝对地址）。
func stub(t *testing.T, trendingHTML string, api map[string]string, raw map[string]string) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/trending", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, trendingHTML)
	})
	// /trending/{lang} 子路径同桩
	mux.HandleFunc("/trending/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, trendingHTML)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api")
		if body, ok := api[path]; ok {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, body)
			return
		}
		http.Error(w, "not found", 404)
	})
	// raw 挂兜底路径（真实 raw URL 无 /raw/ 前缀，与 Readme 的 download_url 解析一致）
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/")
		if body, ok := raw[key]; ok {
			fmt.Fprint(w, body)
			return
		}
		http.Error(w, "not found", 404)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	for k, v := range api {
		api[k] = strings.ReplaceAll(v, "%BASE%", srv.URL)
	}
	c := New("")
	c.BaseTrending = srv.URL + "/trending"
	c.BaseAPI = srv.URL + "/api"
	c.BaseRaw = srv.URL
	return c
}

const fixtureTrending = `<html><body>
<article class="Box-row">
  <h2><a href="/cloudflare/security-audit-skill/">cloudflare / security-audit-skill</a></h2>
  <p class="col-9 color-fg-muted my-1 pr-4"> A coding-agent skill that turns your agent into a &amp; security auditor</p>
  <a href="/cloudflare/security-audit-skill/stargazers" class="d-inline-block float-sm-right"> <svg/> 42,031</a>
  <span itemprop="programmingLanguage">Go</span>
</article>
<article class="Box-row">
  <h2><a href="/Graphify-Labs/graphify/">Graphify-Labs / graphify</a></h2>
  <p class="col-9 color-fg-muted my-1 pr-4">知识图谱生成器</p>
  <a href="/Graphify-Labs/graphify/stargazers"> 1,204</a>
</article>
</body></html>`

func TestTrendingParse(t *testing.T) {
	c := stub(t, fixtureTrending, nil, nil)
	repos, err := c.Trending("")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("应解析出 2 条: %d", len(repos))
	}
	r := repos[0]
	if r.FullName != "cloudflare/security-audit-skill" || r.Stars != 42031 || r.Lang != "Go" {
		t.Fatalf("首条解析错误: %+v", r)
	}
	if !strings.Contains(r.Desc, "& security auditor") || strings.Contains(r.Desc, "<") {
		t.Fatalf("描述应去标签且解码实体: %q", r.Desc)
	}
	// lang 路径参数
	c2 := stub(t, fixtureTrending, nil, nil)
	if _, err := c2.Trending("Go"); err != nil {
		t.Fatal(err)
	}
	// 结构变化 → 明确报错而非空列表
	if _, err := stub(t, "<html></html>", nil, nil).Trending(""); err == nil {
		t.Fatal("解析 0 条应报错")
	}
}

func TestSearchParse(t *testing.T) {
	api := map[string]string{
		"/search/repositories": `{"items":[{"full_name":"a/b","description":"教程","language":"Python","stargazers_count":100,"html_url":"https://github.com/a/b"}]}`,
	}
	c := stub(t, "", api, nil)
	repos, err := c.Search("决策树 language:Python", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "a/b" || repos[0].Stars != 100 {
		t.Fatalf("search 解析错误: %+v", repos)
	}
}

func TestDetailTreeRawReadme(t *testing.T) {
	api := map[string]string{
		"/repos/o/r":                `{"default_branch":"main","description":"d","stargazers_count":7}`,
		"/repos/o/r/git/trees/main": `{"tree":[{"path":"README.md","type":"blob"},{"path":"examples/lr.py","type":"blob","size":100},{"path":"node_modules/x.js","type":"blob"},{"path":"data.bin","type":"blob"}]}`,
		"/repos/o/r/readme":         `{"download_url":"%BASE%/o/r/main/README.md"}`,
	}
	c := stub(t, "", api, map[string]string{
		"o/r/main/README.md":      "# Demo 教程\nQuick Start",
		"o/r/main/examples/lr.py": "print('hi')",
		"o/r/main/data.bin":       "ok",
	})
	d, err := c.Detail("o/r")
	if err != nil {
		t.Fatal(err)
	}
	if d.DefaultBranch != "main" || d.Stars != 7 {
		t.Fatalf("detail 错误: %+v", d)
	}
	tree, err := c.Tree("o/r")
	if err != nil {
		t.Fatal(err)
	}
	// 只滤噪声目录（node_modules）；.bin 保留在树里（抓取时才拒二进制）
	if len(tree) != 3 || !strings.Contains(tree[0], "README") {
		t.Fatalf("树应滤掉 node_modules 剩 3 条: %v", tree)
	}
	raw, err := c.Raw("o/r", "main", "examples/lr.py")
	if err != nil || !strings.Contains(raw, "hi") {
		t.Fatalf("raw 失败: %q %v", raw, err)
	}
	if _, err := c.Raw("o/r", "main", "a\\b.txt"); err == nil {
		t.Fatal("非法路径应报错")
	}
	// 二进制拒收
	bin := append([]byte("PK\x00\x03"), make([]byte, 32)...)
	c2 := stub(t, "", nil, map[string]string{"o/r/main/data.bin": string(bin)})
	if _, err := c2.Raw("o/r", "main", "data.bin"); err == nil || !strings.Contains(err.Error(), "二进制") {
		t.Fatalf("二进制应拒收: %v", err)
	}
	// 超长截断
	long := strings.Repeat("x", MaxFileBytes+100)
	c3 := stub(t, "", nil, map[string]string{"o/r/main/big.md": long})
	got, err := c3.Raw("o/r", "main", "big.md")
	if err != nil || !strings.Contains(got, "已截断") {
		t.Fatalf("超长应截断: %v", err)
	}
	// Readme 走 API 下载地址
	rm, err := c.Readme("o/r")
	if err != nil || !strings.Contains(rm, "Quick Start") {
		t.Fatalf("readme 失败: %q %v", rm, err)
	}
}
