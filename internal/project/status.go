package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// 副本的生命周期（主人 2026-11-03 23:29 定的）：
//
//	讲解完 → 建文件夹(ready) → 他做(working) → 点提交(submitted)
//	→ 助教检查 → 过关(passed) / 没过退回(working，留在这一关)
//
// 关键：**没过不许开下一个副本**——「过关则副本完成」不是装饰。
const (
	StatusReady     = "ready"     // 建好了，还没开始
	StatusWorking   = "working"   // 在做
	StatusSubmitted = "submitted" // 提交了，等检查
	StatusPassed    = "passed"    // 过关
)

// StatusFile 每个项目目录里一份状态。
const StatusFile = ".status.json"

// Status 一个副本的状态。
type Status struct {
	Dir        string    `json:"dir"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	Submits    int       `json:"submits"`    // 点了几次提交（含没过的）
	LastCheck  string    `json:"lastCheck"`  // 上次检查摘要（几条过/共几条）
	LastFailed []string  `json:"lastFailed"` // 上次没过的是哪几条
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Path 状态文件路径。
func statusPath(dir string) string { return filepath.Join(dir, StatusFile) }

// LoadStatus 读状态；没有就当 ready。
func LoadStatus(dir string) Status {
	b, err := os.ReadFile(statusPath(dir))
	if err != nil {
		return Status{Dir: dir, State: StatusReady}
	}
	var s Status
	if json.Unmarshal(b, &s) != nil {
		return Status{Dir: dir, State: StatusReady}
	}
	if s.State == "" {
		s.State = StatusReady
	}
	s.Dir = dir
	return s
}

// SaveStatus 落盘。
func SaveStatus(s Status) error {
	if s.Dir == "" {
		return fmt.Errorf("状态没有目录")
	}
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statusPath(s.Dir), b, 0o644)
}

// ListDirs 列出练习根下的所有项目目录（含状态），新的在前。
func ListDirs(root string) []Status {
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []Status
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(dir, SpecFile)); err != nil {
			continue // 不是我们建的项目
		}
		out = append(out, LoadStatus(dir))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Blocking 有没有「没过的副本」挡在前面。
// 有的话：不许开新副本，先把这一关过了。
func Blocking(root string) (Status, bool) {
	for _, s := range ListDirs(root) {
		if s.State != StatusPassed {
			return s, true
		}
	}
	return Status{}, false
}

// Submit 提交并记录检查结果。
// passed=false 时状态退回 working：留在这一关，改完再提交。
func Submit(dir string, passed bool, failed []string, summary string) (Status, error) {
	s := LoadStatus(dir)
	if s.State == StatusPassed {
		return s, fmt.Errorf("这个副本已经过关了")
	}
	s.Submits++
	s.LastCheck = summary
	s.LastFailed = failed
	if passed {
		s.State = StatusPassed
	} else {
		s.State = StatusWorking
	}
	if err := SaveStatus(s); err != nil {
		return s, err
	}
	return s, nil
}
