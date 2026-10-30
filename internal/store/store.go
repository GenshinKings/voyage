// Package store 管落盘的原子性与「读-改-写」的互斥。
//
// 架构体检 2026-10-30：之前这里挂的是两个**包级全局 sync.Mutex**，
// 所有 Server 实例共用一把锁——测试并行、多实例运行时互相阻塞，
// 和我们刚拆掉的包级状态缓存是同一类病。
// 现在改成 Store 结构体：谁 new 谁持有，锁跟着实例走。
package store

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// Store 一个实例一份锁。零值不可用，请用 New。
type Store struct {
	// State 保住 state 的「读-改-写」临界区（并发保存互相覆盖的根因治理）。
	State sync.Mutex
	// Write 串行化本实例的全部文件落盘。
	write sync.Mutex
}

// New 造一个 Store。
func New() *Store { return &Store{} }

// Load 读 JSON；解析失败自动回退同名 .bak（上层负责 log 提示语义）。
func (s *Store) Load(dir, name string, v any) bool {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	if err := json.Unmarshal(b, v); err == nil {
		return true
	}
	backup := filepath.Join(dir, name+".bak")
	if bb, err2 := os.ReadFile(backup); err2 == nil {
		if json.Unmarshal(bb, v) == nil {
			log.Printf("[恢复] %s 解析失败，已自动回退到上次备份", name)
			return true
		}
	}
	log.Printf("[恢复失败] %s 无可用备份", name)
	return false
}

// Save 原子写：.tmp → rename 覆盖（Windows rename 失败退化处理）→ 成功后同步留 .bak。
func (s *Store) Save(dir, name string, v any) {
	if s == nil {
		s = New() // 兜底：调用方偷懒没初始化时不至于 panic
	}
	s.write.Lock()
	defer s.write.Unlock()
	_ = os.MkdirAll(dir, 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	final := filepath.Join(dir, name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(final)
		if os.Rename(tmp, final) != nil {
			return
		}
	}
	_ = os.WriteFile(final+".bak", b, 0o644)
}
