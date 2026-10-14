package kb

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

// kb.go — 知识节点与三层结构的状态机。

// NodeStatus 节点知识状态（由批判批注派生，不手工设置）。
type NodeStatus string

const (
	StatusSparse    NodeStatus = "sparse"    // 来源不足（<2 片段），结论待沉淀
	StatusConsensus NodeStatus = "consensus" // 多源一致，无冲突批注
	StatusDisputed  NodeStatus = "disputed"  // 存在 disputed 批注——出辨析题，不假装唯一答案
	StatusRefuted   NodeStatus = "refuted"   // 存在 refuted 批注——旧结论已证伪，教新结论
)

// MentorView ②助教理解层：原创综合讲法（无原书指纹，闸门校验）。
type MentorView struct {
	ID        string     `json:"id"`
	Text      string     `json:"text"`
	Anchors   []string   `json:"anchors"`      // 锚定的 Source.ID（原文可回溯）
	Gate      *GateResult `json:"gate,omitempty"` // 相似度闸门结果
	// 品味进化（宪法 3.5）：讲后效果统计——由应用层回流
	Taught    int        `json:"taught"`              // 讲过几次
	PassFirst int        `json:"passFirst"`           // 讲后一次通过次数
}

// Mean 效果均值（一次通过率）——品味进化的权重信号。
func (m *MentorView) Mean() float64 {
	if m.Taught == 0 {
		return -1 // 未教过（无数据）
	}
	return float64(m.PassFirst) / float64(m.Taught)
}

// CritiqueKind 批注类型。
type CritiqueKind string

const (
	CritiqueOutdated CritiqueKind = "outdated" // 过时（有更新结论）
	CritiqueBiased   CritiqueKind = "biased"   // 片面
	CritiqueDisputed CritiqueKind = "disputed" // 有争议（多源分歧）
	CritiqueRefuted  CritiqueKind = "refuted"  // 已证伪
)

// Critique ③批判批注层。
type Critique struct {
	ID          string       `json:"id"`
	Kind        CritiqueKind `json:"kind"`
	Claim       string       `json:"claim"`       // 被批注的原始声明（或其来源片段 id）
	Correction  string       `json:"correction"`  // 修正/当前共识
	SourceID    string       `json:"sourceId"`    // 修正依据（①层片段）
	Confidence  float64      `json:"confidence"`  // 0-1（多源一致度；全自动判定不可得，呈现争议+分级）
	At          time.Time    `json:"at"`
}

// KNode 知识点节点：多源片段 + 助教理解 + 批判批注 + 前后置。
type KNode struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Sources  []Source   `json:"sources"`
	Views    []MentorView `json:"views"`
	Critiques []Critique `json:"critiques"`
	Prereqs  []string   `json:"prereqs,omitempty"` // 前置知识点
	Nexts    []string   `json:"nexts,omitempty"`   // 后续知识点
}

// Status 派生状态：refuted > disputed > sparse > consensus。
func (n *KNode) Status() NodeStatus {
	for _, c := range n.Critiques {
		if c.Kind == CritiqueRefuted {
			return StatusRefuted
		}
	}
	for _, c := range n.Critiques {
		if c.Kind == CritiqueDisputed {
			return StatusDisputed
		}
	}
	if len(n.Sources) < 2 {
		return StatusSparse
	}
	return StatusConsensus
}

// BestView 按品味权重选最佳讲法：一次通过率降序，未教过的排后（无数据不装懂）。
func (n *KNode) BestView() *MentorView {
	var best *MentorView
	for i := range n.Views {
		v := &n.Views[i]
		if best == nil {
			best = v
			continue
		}
		bm, vm := best.Mean(), v.Mean()
		if vm >= 0 && (bm < 0 || vm > bm) {
			best = v
		}
	}
	return best
}

// KB 知识库。
type KB struct {
	Nodes map[string]*KNode `json:"nodes"`
	Rules []LicenseRule     `json:"-"`
	Now   func() time.Time  `json:"-"`
}

func New() *KB {
	return &KB{Nodes: map[string]*KNode{}, Rules: DefaultRules(), Now: time.Now}
}

// AddSource 入账①层片段：授权红线检查（Excluded 拒收）→ 权重计算 → 归点。
func (kb *KB) AddSource(nodeID, title, url, excerpt, license string, published time.Time) (*Source, error) {
	class, reason := Classify(license, url, kb.Rules)
	if class == Excluded {
		return nil, &ErrSourceRejected{URL: url, Reason: reason}
	}
	n := kb.ensureNode(nodeID)
	s := Source{
		ID:          fmt.Sprintf("%s-s%d", nodeID, len(n.Sources)+1),
		NodeID:      nodeID,
		URL:         url,
		Title:       title,
		Excerpt:     excerpt,
		License:     license,
		Class:       class,
		PublishedAt: published,
		FetchedAt:   kb.Now(),
		Weight:      SourceWeight(url, license, published, kb.Now()),
	}
	n.Sources = append(n.Sources, s)
	return &s, nil
}

// AddMentorView 入账②层：相似度闸门不过 → 拒收（改写抄袭嫌疑）。
func (kb *KB) AddMentorView(nodeID, id, text string, anchorIDs []string) (*MentorView, error) {
	n := kb.ensureNode(nodeID)
	anchored := []Source{}
	for _, aid := range anchorIDs {
		for _, s := range n.Sources {
			if s.ID == aid {
				anchored = append(anchored, s)
			}
		}
	}
	gate := GateMentorView(text, anchored)
	v := MentorView{ID: id, Text: text, Anchors: anchorIDs, Gate: &gate}
	if !gate.Passed {
		return &v, fmt.Errorf("相似度闸门未过（Jaccard %.2f ≥ %.2f，对照 %s）——助教讲法贴近原文，须重写为原创综合",
			gate.MaxJaccard, gate.Threshold, gate.WorstURL)
	}
	n.Views = append(n.Views, v)
	return &v, nil
}

// AddCritique 入账③层批注（状态机自动派生）。
func (kb *KB) AddCritique(nodeID string, c Critique) {
	if c.At.IsZero() {
		c.At = kb.Now()
	}
	n := kb.ensureNode(nodeID)
	n.Critiques = append(n.Critiques, c)
}

// Link 前后置关系。
func (kb *KB) Link(from, to string) {
	kb.ensureNode(from).Nexts = append(kb.ensureNode(from).Nexts, to)
	kb.ensureNode(to).Prereqs = append(kb.ensureNode(to).Prereqs, from)
}

// Attribution 导出署名清单（CC-BY-SA 义务：全部 Whitelist 来源逐条署名；
// Graylist 单独成「待核清单」——宪法纪律）。
type Attribution struct {
	Whitelist []Source `json:"whitelist"`
	Graylist  []Source `json:"graylist"`
	License   string   `json:"license"`
}

func (kb *KB) Attribution() Attribution {
	out := Attribution{License: "CC-BY-SA-4.0"}
	for _, n := range kb.Nodes {
		for _, s := range n.Sources {
			switch s.Class {
			case Whitelist:
				out.Whitelist = append(out.Whitelist, s)
			case Graylist:
				out.Graylist = append(out.Graylist, s)
			}
		}
	}
	sort.Slice(out.Whitelist, func(i, j int) bool { return out.Whitelist[i].URL < out.Whitelist[j].URL })
	sort.Slice(out.Graylist, func(i, j int) bool { return out.Graylist[i].URL < out.Graylist[j].URL })
	return out
}

// ExportJSON CC-BY-SA 导出（知识库全量 + 署名清单 + 授权头）。
func (kb *KB) ExportJSON(w io.Writer) error {
	header := map[string]any{
		"license":    "CC-BY-SA-4.0",
		"notice":     "本知识库基于 CC-BY-SA-4.0 共享：使用、修改、分发须同样以 CC-BY-SA 开放（share-alike）。",
		"attribution": kb.Attribution(),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(header); err != nil {
		return err
	}
	return enc.Encode(kb.Nodes)
}

// Save/Load 极简 JSON 持久化（引擎层不碰 IO 细节，应用层决定路径）。
func (kb *KB) Save(path string) error {
	b, err := json.MarshalIndent(kb, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadKB(path string) (*KB, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	kb := New()
	if err := json.Unmarshal(b, kb); err != nil {
		return nil, err
	}
	if kb.Rules == nil {
		kb.Rules = DefaultRules()
	}
	if kb.Now == nil {
		kb.Now = time.Now
	}
	return kb, nil
}

func (kb *KB) ensureNode(id string) *KNode {
	n, ok := kb.Nodes[id]
	if !ok {
		n = &KNode{ID: id, Title: id}
		kb.Nodes[id] = n
	}
	return n
}

// SetTitle 设置节点标题。
func (kb *KB) SetTitle(nodeID, title string) { kb.ensureNode(nodeID).Title = title }
