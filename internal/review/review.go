// Package review —— 第三方判定的「待判队列」。
//
// 产品形态先按单人本地版落地：没有可用模型时，产物不会静默判过，而是进队列，
// 由人（当前就是用户自己）决定过不过。等形态定了（导师版 / 社区版），
// 只需换掉「谁来判」，队列与接口不动。
package review

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"voyage/internal/store"
)

// Status 一条待判记录的状态。
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
)

// Item 一条待判产物。
type Item struct {
	ID        string `json:"id"`
	LessonID  string `json:"lessonId,omitempty"` // 关卡所属知识点（若来自学习流程）
	StageID   string `json:"stageId,omitempty"`  // 闯关的哪一关
	Target    string `json:"target"`             // 判的是什么（关卡目标/题目摘要）
	Output    string `json:"output"`             // 学习者交的产物
	Status    Status `json:"status"`
	Reason    string `json:"reason,omitempty"` // 为什么转人工（给判卷人看）
	Where     string `json:"where,omitempty"`  // 判卷人给的「差在哪」
	Action    string `json:"action,omitempty"` // 判卷人给的「下一步」
	CreatedAt string `json:"createdAt"`
	DecidedAt string `json:"decidedAt,omitempty"`
	DecidedBy string `json:"decidedBy,omitempty"`
}

// Manager 队列 + 落盘。实例持有锁，不用包级全局（与 store 的口径一致）。
type Manager struct {
	mu    sync.Mutex
	dir   string
	items []Item
	st    *store.Store
}

// New 造一个队列（不读盘；调用方按需 Load）。
func New(dir string) *Manager {
	return &Manager{dir: dir, st: store.New()}
}

const fileName = "reviews.json"

// Load 从 dir/reviews.json 读回队列；文件缺失或损坏时保持空队列（不阻塞启动）。
func (m *Manager) Load() {
	m.mu.Lock()
	defer m.mu.Unlock()
	var items []Item
	if m.st.Load(m.dir, fileName, &items) {
		m.items = items
	}
}

// Enqueue 入队一条待判产物，返回带 ID 的记录。
func (m *Manager) Enqueue(it Item) Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it.ID == "" {
		it.ID = newID()
	}
	if it.Status == "" {
		it.Status = StatusPending
	}
	if it.CreatedAt == "" {
		it.CreatedAt = time.Now().Format(time.RFC3339)
	}
	m.items = append(m.items, it)
	m.persistLocked()
	return it
}

// Pending 待判列表（按入队顺序）。
func (m *Manager) Pending() []Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Item, 0, len(m.items))
	for _, it := range m.items {
		if it.Status == StatusPending {
			out = append(out, it)
		}
	}
	return out
}

// Decide 给一条待判记录下结论。by 记录是谁判的（当前会是 "self"）。
func (m *Manager) Decide(id string, pass bool, where, action, by string) (Item, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.items {
		if m.items[i].ID != id || m.items[i].Status != StatusPending {
			continue
		}
		if pass {
			m.items[i].Status = StatusApproved
		} else {
			m.items[i].Status = StatusRejected
		}
		m.items[i].Where = where
		m.items[i].Action = action
		m.items[i].DecidedAt = time.Now().Format(time.RFC3339)
		m.items[i].DecidedBy = by
		m.persistLocked()
		return m.items[i], true
	}
	return Item{}, false
}

// All 全部记录（诊断用）。
func (m *Manager) All() []Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Item, len(m.items))
	copy(out, m.items)
	return out
}

// persistLocked 落盘。调用方需持锁。
func (m *Manager) persistLocked() {
	if m.dir == "" {
		return
	}
	m.st.Save(m.dir, fileName, m.items)
}

// newID 生成短 ID（16 位十六进制）。
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
