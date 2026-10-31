// Package inbox —— 麻烦收件箱：把「今天真烦的一件事」变成一张可执行的任务卡。
//
// 为什么需要它：真实学习不是从课程第一课开始的，是从「这件事烦到我了」开始的。
// 收件箱只做两件事：接住一句话，然后推进成一张带验收标准的任务卡。
//
// 设计约束：不自动开任务。收一条是低成本的（随手记），开任务是高成本的（要写验收），
// 所以中间留一个人工确认的 promote 步骤。
package inbox

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"voyage/internal/store"
)

// Status 一条收件箱记录的状态。
type Status string

const (
	StatusOpen    Status = "open"    // 只是记下来了
	StatusPlanned Status = "planned" // 已经变成任务卡
	StatusDropped Status = "dropped" // 想过了，不做
)

// Item 一句话麻烦。
type Item struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Status    Status `json:"status"`
	CreatedAt string `json:"createdAt"`
	// Promote 之后回填：这张任务卡长什么样
	Goal  string `json:"goal,omitempty"`
	Check string `json:"check,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

// Box 收件箱 + 落盘。
type Box struct {
	mu    sync.Mutex
	dir   string
	items []Item
	st    *store.Store
}

const fileName = "inbox.json"

// New 造一个收件箱（不读盘）。
func New(dir string) *Box {
	return &Box{dir: dir, st: store.New()}
}

// Load 读回；文件缺失或坏掉时保持空箱（不阻塞启动）。
func (b *Box) Load() {
	b.mu.Lock()
	defer b.mu.Unlock()
	var items []Item
	if b.st.Load(b.dir, fileName, &items) {
		b.items = items
	}
}

// Add 收一条。空白文本直接拒绝。
func (b *Box) Add(text string) (Item, error) {
	t := trimSpace(text)
	if t == "" {
		return Item{}, fmt.Errorf("写点什么再收：一句话说清这事烦在哪")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	it := Item{ID: newID(), Text: t, Status: StatusOpen, CreatedAt: now()}
	b.items = append(b.items, it)
	b.persistLocked()
	return it, nil
}

// List 全部记录（新的在前）。
func (b *Box) List() []Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Item, 0, len(b.items))
	for i := len(b.items) - 1; i >= 0; i-- {
		out = append(out, b.items[i])
	}
	return out
}

// Open 还没处理的条目（给「今天挑一件」用）。
func (b *Box) Open() []Item {
	out := []Item{}
	for _, it := range b.List() {
		if it.Status == StatusOpen {
			out = append(out, it)
		}
	}
	return out
}

// Promote 把一条变成任务卡（回填 goal/check/hint 并改状态）。
func (b *Box) Promote(id, goal, check, hint string) (Item, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.items {
		if b.items[i].ID != id {
			continue
		}
		if b.items[i].Status != StatusOpen {
			return Item{}, false
		}
		b.items[i].Status = StatusPlanned
		b.items[i].Goal = trimSpace(goal)
		b.items[i].Check = trimSpace(check)
		b.items[i].Hint = trimSpace(hint)
		b.persistLocked()
		return b.items[i], true
	}
	return Item{}, false
}

// Drop 决定不做（保留记录，别删——「想过但不做」也是信息）。
func (b *Box) Drop(id string) (Item, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.items {
		if b.items[i].ID == id && b.items[i].Status == StatusOpen {
			b.items[i].Status = StatusDropped
			b.persistLocked()
			return b.items[i], true
		}
	}
	return Item{}, false
}

func (b *Box) persistLocked() {
	if b.dir == "" {
		return
	}
	b.st.Save(b.dir, fileName, b.items)
}

func newID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("i%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func now() string { return time.Now().Format(time.RFC3339) }

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
