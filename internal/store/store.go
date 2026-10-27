// Package store — 小穗四轮·架构 P1-1：把「文件读写的原子性/锁/损坏自愈」收敛成独立存储层，
// Server 只做薄委托；后续所有 JSON 落盘逻辑统一走这里（.tmp+rename+.bak+解析失败回退）。
package store

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

var (
	// WriteMutex 串行化全部文件落盘（tmp+rename+race 一并消掉）。
	WriteMutex sync.Mutex
	// StateMutex 保住 state 的「读-改-写」临界区（并发保存互相覆盖的根因治理）。
	StateMutex sync.Mutex
)

// Load 读 JSON；解析失败自动回退同名 .bak（server 上层负责 log 提示语义）。
func Load(dir, name string, v any) bool {
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
func Save(dir, name string, v any) {
	WriteMutex.Lock()
	defer WriteMutex.Unlock()
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
