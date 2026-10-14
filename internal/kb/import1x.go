// import1x.go — ①层迁移对接：前哨 1.x ReadingDoc JSON → KB AddSource。
//
// 1.x 的阅读书架（user_kv `readingDocs` blob）已经是抓取+翻译+健康分过的内容——
// 直接迁入 2.0 知识库①层，来源授权按 URL/来源保守判定（官方域名白/灰名单，论坛 CC 推定）。
package kb

import (
	"encoding/json"
	"strings"
	"time"
)

// ReadingDocV1 前哨 1.x ReadingDoc 的 JSON 形状（只取迁移所需字段）。
type ReadingDocV1 struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
	ZH    string `json:"zh"`
	At    string `json:"at"`
}

// InferLicense 从 URL 推断授权标签（保守：能明确判白名单的才给白名单 license）。
func InferLicense(url string) string {
	low := strings.ToLower(url)
	switch {
	case strings.Contains(low, "wikipedia.org"):
		return "CC-BY-SA"
	case strings.Contains(low, "arxiv.org"):
		return "arXiv"
	case strings.Contains(low, "gutenberg.org"):
		return "Public Domain"
	case strings.Contains(low, "github.com") || strings.Contains(low, "gitlab"):
		return "MIT" // 仓库页面按最常见的开源授权保守标注；导入时 Classify 仍以 URL 复核
	case strings.Contains(low, "stackoverflow.com"):
		return "CC-BY-SA"
	case strings.Contains(low, "docs.") || strings.Contains(low, "go.dev") || strings.Contains(low, "mdn"):
		return "official-docs"
	default:
		return "" // 未知 → Classify 落 Graylist
	}
}

// ImportReadingDocsV1 批量迁入：每篇 1.x 文档作为①层一个片段，归到 nodeID 节点。
// ZH（中文翻译）作为 excerpt 优先（学习者在 2.0 读中文），Text 兜底。
// 返回：迁入数、拒收数（Excluded 红线）、错误切片。
func (kb *KB) ImportReadingDocsV1(nodeID string, raw []byte) (imported, rejected int, errs []error) {
	var docs []ReadingDocV1
	if err := json.Unmarshal(raw, &docs); err != nil {
		return 0, 0, []error{err}
	}
	now := time.Now()
	for _, d := range docs {
		if strings.TrimSpace(d.URL) == "" {
			continue
		}
		excerpt := d.ZH
		if strings.TrimSpace(excerpt) == "" {
			excerpt = d.Text
		}
		at, perr := time.Parse(time.RFC3339, d.At)
		if perr != nil {
			at = now
		}
		license := InferLicense(d.URL)
		if class, _ := Classify(license, d.URL, kb.Rules); class == Excluded {
			rejected++
			continue
		}
		// 迁移启发式：1.x 文档无 license 字段，版权信号从标题/正文扫描
		// （all rights reserved / proprietary → 商业版权信号，拒收）
		sig := strings.ToLower(d.Title + " " + d.Text)
		if strings.Contains(sig, "all rights reserved") || strings.Contains(sig, "proprietary") {
			rejected++
			continue
		}
		_, err := kb.AddSource(nodeID, d.Title, d.URL, truncate(excerpt, 6000), license, at)
		if err != nil {
			rejected++
			errs = append(errs, err)
			continue
		}
		imported++
	}
	return imported, rejected, errs
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
