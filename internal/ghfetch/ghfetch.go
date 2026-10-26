// Package ghfetch — GitHub 内容抓取原语（助教工具的底层）。
//
// 四个原语：Trending（今日热门）/ Search（定向搜索）/ Browse（仓库概览+文件树）/
// Raw（抓单个源文件）。设计约束（宪法：轻量单二进制）：纯标准库，无第三方依赖；
// 明确的非目标——不抓二进制/超长文件（源文件 cap 64KB，工具输出另行截断），
// 不做认证（匿名限额对个人每日抓取足够，触限时报友好错误）。
package ghfetch

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Client GitHub 抓取客户端。Base* 字段可注入测试桩（默认真实端点）。
type Client struct {
	HTTP         *http.Client
	BaseTrending string // 默认 https://github.com/trending
	BaseAPI      string // 默认 https://api.github.com
	BaseRaw      string // 默认 https://raw.githubusercontent.com
	UserAgent    string
}

// New 构造客户端（proxy 空=直连；如 http://127.0.0.1:7897）。
func New(proxy string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil && u.Scheme != "" {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &Client{
		HTTP:         &http.Client{Timeout: 30 * time.Second, Transport: tr},
		BaseTrending: "https://github.com/trending",
		BaseAPI:      "https://api.github.com",
		BaseRaw:      "https://raw.githubusercontent.com",
		UserAgent:    "voyage-outpost2",
	}
}

// Repo 一个仓库条目（Trending/Search 的返回行）。
type Repo struct {
	FullName string `json:"fullName"`
	Desc     string `json:"desc"`
	Lang     string `json:"lang,omitempty"`
	Stars    int    `json:"stars"`
	URL      string `json:"url"`
}

func (c *Client) get(rawURL string, accept string) ([]byte, int, http.Header, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	if resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return b, resp.StatusCode, resp.Header, fmt.Errorf("GitHub 匿名限额用尽（60 次/小时）——稍后再试")
	}
	if resp.StatusCode >= 400 && len(strings.TrimSpace(string(b))) == 0 {
		return b, resp.StatusCode, resp.Header, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	return b, resp.StatusCode, resp.Header, nil
}

// ---------- Trending（HTML 解析） ----------

var (
	artRe   = regexp.MustCompile(`(?s)<article[^>]*class="[^"]*Box-row[^"]*"[^>]*>(.*?)</article>`)
	hrefRe  = regexp.MustCompile(`href="/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/?"`)
	descRe  = regexp.MustCompile(`(?s)<p class="col-9[^"]*"[^>]*>\s*(.*?)</p>`)
	numRe   = regexp.MustCompile(`([\d][\d,]*)`)
	langRe  = regexp.MustCompile(`itemprop="programmingLanguage">([^<]+)<`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = htmlUnescape(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// htmlUnescape 常见 HTML 实体（不引入 x/net 依赖，覆盖描述里的高频实体）。
func htmlUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&#x27;", "'")
	return r.Replace(s)
}

// Trending 抓 GitHub Trending 页（lang 如 "go"/"python"，空=全部语言）。
// 解析失败（页面结构变化）返回带原文片段的错误，便于排查。
func (c *Client) Trending(lang string) ([]Repo, error) {
	u := c.BaseTrending
	if lang != "" {
		u += "/" + strings.ToLower(strings.TrimSpace(lang))
	}
	b, code, _, err := c.get(u, "text/html")
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("trending 页 HTTP %d", code)
	}
	var out []Repo
	for _, m := range artRe.FindAllStringSubmatch(string(b), -1) {
		chunk := m[1]
		hm := hrefRe.FindStringSubmatch(chunk)
		if hm == nil {
			continue
		}
		r := Repo{FullName: hm[1], URL: "https://github.com/" + hm[1]}
		if dm := descRe.FindStringSubmatch(chunk); dm != nil {
			r.Desc = stripTags(dm[1])
		}
		if i := strings.Index(chunk, `/stargazers"`); i >= 0 {
			// star svg 的 path 数据里全是数字——跳到 </svg> 之后再取第一个数字
			tail := chunk[i:]
			if j := strings.Index(tail, `</svg>`); j >= 0 {
				tail = tail[j+6:]
			} else if len(tail) > 600 {
				tail = tail[:600]
			}
			if nm := numRe.FindStringSubmatch(stripTags(tail)); nm != nil {
				r.Stars, _ = strconv.Atoi(strings.ReplaceAll(nm[1], ",", ""))
			}
		}
		if lm := langRe.FindStringSubmatch(chunk); lm != nil {
			r.Lang = strings.TrimSpace(lm[1])
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trending 解析为 0 条——页面结构可能变了（HTTP %d，前 200 字节：%s）", code, preview(b, 200))
	}
	return out, nil
}

func preview(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// ---------- Search / Detail / Tree / Raw（JSON API + raw） ----------

// Search 定向搜索仓库（GitHub search 语法，如 "决策树 tutorial language:Python"，n≤20）。
func (c *Client) Search(query string, n int) ([]Repo, error) {
	if n <= 0 || n > 20 {
		n = 10
	}
	q := url.Values{"q": {query}, "sort": {"stars"}, "order": {"desc"}, "per_page": {strconv.Itoa(n)}}
	b, code, _, err := c.get(c.BaseAPI+"/search/repositories?"+q.Encode(), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("search HTTP %d: %s", code, preview(b, 160))
	}
	var sr struct {
		Items []struct {
			FullName    string `json:"full_name"`
			Description string `json:"description"`
			Language    string `json:"language"`
			Stargazers  int    `json:"stargazers_count"`
			HTMLURL     string `json:"html_url"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, err
	}
	out := make([]Repo, 0, len(sr.Items))
	for _, it := range sr.Items {
		out = append(out, Repo{FullName: it.FullName, Desc: it.Description, Lang: it.Language, Stars: it.Stargazers, URL: it.HTMLURL})
	}
	return out, nil
}

// Detail 仓库元信息（default branch 是抓文件的必要前提）。
type Detail struct {
	DefaultBranch string `json:"defaultBranch"`
	Desc          string `json:"desc"`
	Stars         int    `json:"stars"`
}

func (c *Client) Detail(repo string) (*Detail, error) {
	b, code, _, err := c.get(c.BaseAPI+"/repos/"+repo, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("仓库 %s HTTP %d（不存在或限额）", repo, code)
	}
	var jr struct {
		DefaultBranch string `json:"default_branch"`
		Description   string `json:"description"`
		Stargazers    int    `json:"stargazers_count"`
	}
	if err := json.Unmarshal(b, &jr); err != nil {
		return nil, err
	}
	return &Detail{DefaultBranch: jr.DefaultBranch, Desc: jr.Description, Stars: jr.Stargazers}, nil
}

// Tree 仓库文件树（排除噪声目录，路径数封顶——树是助教判断「README 之外还有什么可学」的依据）。
func (c *Client) Tree(repo string) ([]string, error) {
	d, err := c.Detail(repo)
	if err != nil {
		return nil, err
	}
	b, code, _, err := c.get(fmt.Sprintf("%s/repos/%s/git/trees/%s?recursive=1", c.BaseAPI, repo, url.PathEscape(d.DefaultBranch)), "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	if code >= 400 {
		return nil, fmt.Errorf("文件树 HTTP %d", code)
	}
	var jt struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			Size int    `json:"size,omitempty"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(b, &jt); err != nil {
		return nil, err
	}
	skip := map[string]bool{"node_modules": true, ".git": true, "vendor": true, "dist": true, "build": true, "__pycache__": true, ".github": true}
	out := []string{}
	for _, t := range jt.Tree {
		if t.Type != "blob" {
			continue
		}
		parts := strings.Split(t.Path, "/")
		bad := false
		for _, p := range parts {
			if skip[p] {
				bad = true
				break
			}
		}
		if bad {
			continue
		}
		out = append(out, t.Path)
		if len(out) >= 300 {
			break
		}
	}
	if jt.Truncated && len(out) >= 300 {
		out = append(out, "…（树被截断，只列了前 300 个文件）")
	}
	return out, nil
}

// MaxFileBytes 单文件抓取上限（64KB，与练习提交上限一致）。
const MaxFileBytes = 64 << 10

// Raw 抓一个源文件（文本；二进制拒收；超长截断标注）。
func (c *Client) Raw(repo, branch, path string) (string, error) {
	if strings.ContainsAny(path, "\\") || strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("非法路径 %q", path)
	}
	u := fmt.Sprintf("%s/%s/%s/%s", c.BaseRaw, repo, branch, path)
	b, code, _, err := c.get(u, "text/plain")
	if err != nil || code >= 400 {
		// raw 域被网络掐断时降级 API contents（base64）——api.github.com 路由独立于 raw，通常仍可达
		if b2, code2, _, err2 := c.get(fmt.Sprintf("%s/repos/%s/contents/%s?ref=%s", c.BaseAPI, repo, url.PathEscape(path), url.PathEscape(branch)), "application/vnd.github+json"); err2 == nil && code2 < 400 {
			var jr struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			if json.Unmarshal(b2, &jr) == nil && jr.Encoding == "base64" {
				if dec, decErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(jr.Content, "\n", "")); decErr == nil {
					b = dec
				} else {
					return "", err
				}
			} else {
				return "", fmt.Errorf("文件 %s HTTP %d", path, code)
			}
		} else {
			return "", fmt.Errorf("文件 %s HTTP %d（raw 与 API 均不可达）", path, code)
		}
	}
	if isBinary(b) {
		return "", fmt.Errorf("%s 是二进制文件，不抓（本工具只取文本）", path)
	}
	s := string(b)
	if len(b) > MaxFileBytes {
		s = s[:MaxFileBytes] + "\n…（超过 64KB，已截断）"
	}
	return s, nil
}

// Readme 抓仓库 README（走 /readme API 拿真实下载地址——兼容 .rst/.adoc 等命名）。
func (c *Client) Readme(repo string) (string, error) {
	b, code, _, err := c.get(c.BaseAPI+"/repos/"+repo+"/readme", "application/vnd.github+json")
	if err != nil {
		return "", err
	}
	if code >= 400 {
		return "", fmt.Errorf("README HTTP %d", code)
	}
	var jr struct {
		DownloadURL string `json:"download_url"`
	}
	if err := json.Unmarshal(b, &jr); err != nil || jr.DownloadURL == "" {
		return "", fmt.Errorf("README 下载地址缺失")
	}
	pu, err := url.Parse(jr.DownloadURL)
	if err != nil {
		return "", err
	}
	// download_url 形如 https://raw.githubusercontent.com/owner/repo/branch/path
	parts := strings.SplitN(strings.TrimPrefix(pu.Path, "/"), "/", 4)
	if len(parts) < 4 {
		return "", fmt.Errorf("README 地址无法解析: %s", jr.DownloadURL)
	}
	return c.Raw(parts[0]+"/"+parts[1], parts[2], parts[3])
}

// isBinary 判定是否二进制（NUL 字节或非法 UTF-8）。
func isBinary(b []byte) bool {
	n := len(b)
	if n > 8000 {
		n = 8000
	}
	probe := b[:n]
	if strings.ContainsRune(string(probe), 0) {
		return true
	}
	return !utf8.Valid(probe)
}
