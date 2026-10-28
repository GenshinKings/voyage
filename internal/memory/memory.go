// Package memory — 第七轮：记忆巩固（借脑科学的功能架构，不模拟神经元）。
//
// 三条借来的功能：工作记忆容量有限（一次只引用 1 条）、海马快写（独立文件、
// 不动 state.json）、睡眠重放（夜间巩固 ADD/UPDATE/DELETE/NOOP）。
//
// 铁律（第七轮需求 §一）：
//   - 记忆只写 dataDir/memory/memories.json，不碰 state.json
//   - 三件套 source / ts / confidence 缺一不许入库
//   - 一切 LLM 产出过 schema 校验，失败即丢弃该条并记日志——不允许「半条记忆」
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 条目类别与状态的白名单（LLM 口嗨新值直接拒收）。
const (
	KindFact         = "fact"
	KindPreference   = "preference"
	KindCommitment   = "commitment"
	KindImpression   = "impression"
	KindConclusion   = "conclusion"
	StatusActive     = "active"
	StatusCold       = "cold"       // 衰减：不删，检索默认不取
	StatusSuperseded = "superseded" // 被取代：检索永不返回
)

// History 一次 UPDATE 被替换的旧值（矛盾不并存两条——旧说法进 history 留审计）。
type History struct {
	Text   string    `json:"text"`
	Ts     time.Time `json:"ts"`
	Source string    `json:"source"`
}

// Entry 一条长期记忆。三件套：Source / Ts / Confidence。
type Entry struct {
	ID         string    `json:"id"`
	Text       string    `json:"text"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"` // 哪一场会话、哪一句（session:1026-ythono-01@2026-10-28）
	Ts         time.Time `json:"ts"`
	Confidence float64   `json:"confidence"` // 0~1；被引用上调，衰减线以下进 cold
	Hits       int       `json:"hits"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
	Status     string    `json:"status"`
	Pinned     bool      `json:"pinned,omitempty"` // 身份/长期承诺：不衰减
	History    []History `json:"history,omitempty"`
}

// Store memories.json 的文件形态。
type Store struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Path 记忆文件位置：dataDir/memory/memories.json。
func Path(dataDir string) string { return filepath.Join(dataDir, "memory", "memories.json") }

// Load 读记忆库（无文件/损坏 → 空库，不阻塞启动）。
func Load(dataDir string) *Store {
	s := &Store{Version: 1}
	b, err := os.ReadFile(Path(dataDir))
	if err == nil {
		_ = json.Unmarshal(b, s)
	}
	if s.Version == 0 {
		s.Version = 1
	}
	return s
}

// Save 落盘（memory/ 目录惰性创建）。
func (s *Store) Save(dataDir string) error {
	if err := os.MkdirAll(filepath.Dir(Path(dataDir)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dataDir), b, 0o644)
}

// ValidKind 类别白名单。
func ValidKind(k string) bool {
	switch k {
	case KindFact, KindPreference, KindCommitment, KindImpression, KindConclusion:
		return true
	}
	return false
}

// Validate 三件套硬要求 + 枚举白名单。缺一不许入库。
func Validate(e Entry) error {
	if strings.TrimSpace(e.Text) == "" {
		return fmt.Errorf("text 为空")
	}
	if strings.TrimSpace(e.Source) == "" {
		return fmt.Errorf("source 缺失（哪一场会话、哪一句）")
	}
	if e.Ts.IsZero() {
		return fmt.Errorf("ts 缺失")
	}
	if e.Confidence <= 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence %.2f 越界（0,1]", e.Confidence)
	}
	if !ValidKind(e.Kind) {
		return fmt.Errorf("kind %q 不在白名单", e.Kind)
	}
	return nil
}

// Add 校验后入库（status 恒置 active；id 为空时按时间生成）。校验失败返回错误——
// 调用方记日志丢弃，绝不写「半条记忆」。
func (s *Store) Add(e Entry, now time.Time) (Entry, error) {
	if err := Validate(e); err != nil {
		return Entry{}, err
	}
	if strings.TrimSpace(e.ID) == "" {
		e.ID = fmt.Sprintf("m-%s-%d", now.Format("20060102"), len(s.Entries)+1)
	}
	e.Text = strings.TrimSpace(e.Text)
	e.Status = StatusActive
	s.Entries = append(s.Entries, e)
	return e, nil
}

// Get 按 id 取条目指针（无则 nil）。
func (s *Store) Get(id string) *Entry {
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			return &s.Entries[i]
		}
	}
	return nil
}

// score 单条与检索词的相关分：词命中数 + 新近加成（time decay：越久越靠后）。
func score(e Entry, terms []string, now time.Time) float64 {
	n := 0.0
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.Contains(e.Text, t) {
			n++
		}
	}
	if n == 0 {
		return 0 // 无关条目绝不入选——引用必须「与该知识点相关」
	}
	age := now.Sub(e.Ts)
	switch {
	case age <= 7*24*time.Hour:
		n += 0.5
	case age <= 30*24*time.Hour:
		n += 0.25
	}
	return n
}

// Relevant 检索：与 terms 相关的 active 条目，按相关分降序（同分新者先）。
// cold/superseded 永不返回；limit 上限（开场引用传 1）。
func (s *Store) Relevant(terms []string, now time.Time, limit int) []*Entry {
	type hit struct {
		e *Entry
		v float64
	}
	var hits []hit
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status != StatusActive {
			continue
		}
		if v := score(*e, terms, now); v > 0 {
			hits = append(hits, hit{e, v})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].v != hits[j].v {
			return hits[i].v > hits[j].v
		}
		return hits[i].e.Ts.After(hits[j].e.Ts)
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]*Entry, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.e)
	}
	return out
}

// Cite 引用入账：hits+1、lastUsedAt=now、confidence +0.05（上限 0.99）。
func (s *Store) Cite(id string, now time.Time) bool {
	e := s.Get(id)
	if e == nil {
		return false
	}
	e.Hits++
	e.LastUsedAt = now
	e.Confidence += 0.05
	if e.Confidence > 0.99 {
		e.Confidence = 0.99
	}
	return true
}

// Decay 衰减与遗忘（第七轮需求 §四）：30 天未被引用 且 confidence < 0.5 → cold；
// pinned 不衰减。返回本次转 cold 的条数。
func (s *Store) Decay(now time.Time, unusedFor time.Duration, below float64) int {
	n := 0
	for i := range s.Entries {
		e := &s.Entries[i]
		if e.Status != StatusActive || e.Pinned {
			continue
		}
		lastUsed := e.Ts
		if !e.LastUsedAt.IsZero() {
			lastUsed = e.LastUsedAt
		}
		if now.Sub(lastUsed) >= unusedFor && e.Confidence < below {
			e.Status = StatusCold
			n++
		}
	}
	return n
}

// Supersede 标记被取代（检索永不返回）。
func (s *Store) Supersede(id string) bool {
	e := s.Get(id)
	if e == nil {
		return false
	}
	e.Status = StatusSuperseded
	return true
}
