// Package kb — 前哨 2.0 知识库三层管线（宪法 §3.2）。
//
// ①原文锚定层：仅授权白名单来源，片段可回溯、带可信度×时效权重；
// ②助教理解层：多源综合的原创讲法，无原书指纹（相似度闸门），锚定①层；
// ③批判批注层：片面/已证伪/争议标注，冲突检测派生节点状态。
// 授权：知识库整体 CC-BY-SA 导出（share-alike）；商业教科书只读不存。
//
// 本包是引擎层：不抓网络（抓取在教务/应用层），只消费已抓取的片段并维护
// 三层结构、授权红线、相似度闸门、冲突状态机与导出。
package kb

import (
	"fmt"
	"strings"
	"time"
)

// LicenseClass 授权分级。
type LicenseClass int

const (
	// Whitelist 随便用：开源授权/公有领域/CC 开源系/arXiv——可入库、可导出、可再分发
	Whitelist LicenseClass = iota
	// Graylist 学习可以、再分发需逐条核：官方文档、无明确授权的公开页——可入库锚定，
	// CC-BY-SA 导出时进入「待核清单」单独陈列
	Graylist
	// Excluded 只读不存：商业教科书、付费课程、无授权且非官方页——AddSource 直接拒收
	Excluded
)

// LicenseRule 一条授权规则。
type LicenseRule struct {
	Match  []string     // 许可标识或域名片段（小写包含匹配）
	Class  LicenseClass
	Reason string
}

// DefaultRules 默认授权规则表（按序匹配，先命中先得）。
// 数据依据：MIT/Apache/BSD 开源授权、CC 开源系、arXiv 官方文本挖掘授权、
// 公有领域（PD）——全部允许学习整理与再分发；CC-BY-SA 的 share-alike
// 恰好与知识库自身授权同构（宪法：共产主义=法律层可强制）。
func DefaultRules() []LicenseRule {
	return []LicenseRule{
		{Match: []string{"mit", "apache-2.0", "apache 2.0", "bsd", "isc", "mpl-2.0", "unlicense", "cc0", "cc-by", "cc-by-sa", "cc by", "creative commons", "arxiv", "public domain", "pd", "gutenberg", "wikipedia", "stack overflow", "stackoverflow"}, Class: Whitelist, Reason: "开放授权/公有领域/CC 开源系"},
		{Match: []string{"docs.", "documentation", "developer.", "/docs", "official", "mdn", "go.dev", "python.org", "rust-lang.org"}, Class: Graylist, Reason: "官方文档——学习锚定可以，再分发前逐条核"},
		{Match: []string{"all rights reserved", "copyright", "©", "oreilly", "manning", "pearson", "mcgraw", " textbook", "课程", "付费", "proprietary", "commercial"}, Class: Excluded, Reason: "商业版权内容——只读不存（宪法红线）"},
	}
}

// Classify 授权判定：看许可标识，再看 URL 片段。未知 → Graylist（保守）。
func Classify(license, url string, rules []LicenseRule) (LicenseClass, string) {
	low := strings.ToLower(license + " \n " + url)
	for _, r := range rules {
		for _, m := range r.Match {
			if m != "" && strings.Contains(low, strings.ToLower(m)) {
				return r.Class, r.Reason
			}
		}
	}
	return Graylist, "授权未知——保守入 Graylist（可锚定，导出进待核清单）"
}

// Source ①原文锚定层的一个片段。
type Source struct {
	ID          string    `json:"id"`
	NodeID      string    `json:"nodeId"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Excerpt     string    `json:"excerpt"` // 原文片段（白名单/灰名单来源）
	License     string    `json:"license"`
	Class       LicenseClass `json:"class"`
	PublishedAt time.Time `json:"publishedAt"` // 来源自身时间（时效加权）
	FetchedAt   time.Time `json:"fetchedAt"`
	Weight      float64   `json:"weight"` // 可信度×时效，入账时计算
}

// TrustBase 来源类型基础可信度（可被 license 微调）。
// 顺序：官方文档/规范 > 教科书级开源书 > 论文 > 高质量文档 > 论坛。
func TrustBase(url, license string) float64 {
	low := strings.ToLower(url + " " + license)
	switch {
	case containsAny(low, []string{"mdn", "go.dev", "python.org", "spec", "rfc", "official"}):
		return 1.0
	case containsAny(low, []string{"arxiv", "acm", "ieee", "nature", "pnas"}):
		return 0.95
	case containsAny(low, []string{"github.com", "gitlab", "readthedocs", "gutenberg"}):
		return 0.85
	case containsAny(low, []string{"wikipedia", "stackoverflow", "juejin", "v2ex", "csdn", "blog"}):
		return 0.6
	default:
		return 0.5
	}
}

// TimeWeight 时效权重：半衰 3 年线性衰减到 0.4 下限（新源优先，老源不清零——
// 数学与 HTTP 缓存无关的经典内容仍然经典）。
func TimeWeight(published, now time.Time) float64 {
	ageYears := now.Sub(published).Hours() / 24 / 365
	if ageYears < 0 {
		ageYears = 0
	}
	w := 1.0 - 0.2*ageYears
	if w < 0.4 {
		w = 0.4
	}
	return w
}

// SourceWeight 综合 = 可信基 × 时效。
func SourceWeight(url, license string, published, now time.Time) float64 {
	return TrustBase(url, license) * TimeWeight(published, now)
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ErrSourceRejected 来源被授权红线拒收。
type ErrSourceRejected struct {
	URL    string
	Reason string
}

func (e *ErrSourceRejected) Error() string {
	return fmt.Sprintf("来源被拒收（只读不存）: %s — %s", e.URL, e.Reason)
}
