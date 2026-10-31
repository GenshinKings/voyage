// Package weakpoints —— 记录「哪个知识点的理解有问题」。
//
// 为什么需要它：副本要「对症下药」，就得先知道病灶在哪。
// 助教批注笔记时发现的「理解错了 / 说不清 / 漏了前提」，逐条落到这里；
// 构建副本时读出来，专门在那一处挖坑，而不是泛泛出题。
package weakpoints

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Item 一条薄弱点。
type Item struct {
	Topic  string `json:"topic"`  // 知识点（尽量短，能当标签用）
	Detail string `json:"detail"` // 具体哪里不对（批注原文或一句话）
	Source string `json:"source"` // 来源：note / inkpal / retro
	At     string `json:"at"`
}

// Store 落盘到 weakpoints.json。
type Store struct {
	mu    sync.Mutex
	path  string
	items []Item
}

// New 打开（或新建）。
func New(dir string) *Store {
	s := &Store{path: filepath.Join(dir, "weakpoints.json")}
	s.Load()
	return s
}

// Load 读盘。
func (s *Store) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return // 还没记过任何薄弱点：空着是正常状态
	}
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return
	}
	s.items = items
}

// Add 记一条；同一个知识点只留最新一条，避免清单一长就没人看。
func (s *Store) Add(topic, detail, source string) Item {
	topic = strings.TrimSpace(topic)
	detail = strings.TrimSpace(detail)
	if topic == "" && detail == "" {
		return Item{}
	}
	if topic == "" {
		topic = "（未命名知识点）"
	}
	it := Item{Topic: topic, Detail: detail, Source: source, At: time.Now().Format(time.RFC3339)}

	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	for _, old := range s.items {
		if strings.EqualFold(old.Topic, topic) {
			continue // 更新掉旧的那条
		}
		kept = append(kept, old)
	}
	s.items = append(kept, it)
	s.persistLocked()
	return it
}

// List 最近的在前。
func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Item, len(s.items))
	copy(out, s.items)
	// 新增的排在后面，倒过来就是最近的在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Recent 取最近 n 条（给副本构建用）。
func (s *Store) Recent(n int) []Item {
	all := s.List()
	if n > 0 && len(all) > n {
		return all[:n]
	}
	return all
}

func (s *Store) persistLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(s.items, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(s.path, raw, 0o644); err != nil {
		return
	}
}
