// Package sandbox — break-fix 沙盒（宪法 §3.3）：AI 预埋教学 bug 的安全工作区。
//
// 闭环：造坑（按薄弱域定向）→ 用户修 → 确定性验收（Check 通过）→ 回流
// （evidence.Record(Used) + ability.Answer）。
//
// 安全边界（宪法红线，代码化）：
//   - 所有文件操作限制在沙盒根目录内（PathSafety 防 ../ 逃逸与绝对路径注入）
//   - 引擎不执行网络操作；v1 验收用内嵌确定性断言（外部 go test 执行器为可选注入）
//   - 工作区为一次性临时目录或用户指定目录，删即清场
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BugKind 教学性 bug 类型（难度与认知层级递进）。
type BugKind string

const (
	BugTypo       BugKind = "typo"       // 拼写/标识符错（知道级）
	BugBoundary   BugKind = "boundary"   // 边界条件（off-by-one 等）
	BugLogic      BugKind = "logic"      // 逻辑反转/条件错
	BugConcurrency BugKind = "concurrency" // 并发/时序（高阶）
)

// CheckResult 确定性验收结果。
type CheckResult struct {
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

// Checker 确定性验收器：读取工作区当前文件内容，断言修复到位。
// v1 为内嵌函数（自包含、跨平台、零依赖）；外部 go test 执行器为可选注入。
type Checker func(files map[string]string) CheckResult

// BreakCase 一个预埋教学 bug 的迷你项目。
type BreakCase struct {
	ID      string            `json:"id"`
	Title   string            `json:"title"`
	Domain  string            `json:"domain"`  // 薄弱域（联动 ability 精准喂坑）
	Kind    BugKind           `json:"kind"`
	Brief   string            `json:"brief"`   // 任务描述（不泄露 bug 位置）
	Files   map[string]string `json:"files"`   // 预埋 bug 的项目文件
	Check   Checker           `json:"-"`       // 确定性验收（不序列化——防用户从数据里读出答案）
	KnowledgePoint string     `json:"knowledgePoint"` // 修完点亮的知识点（履历联动）
	SourceIssue string        `json:"sourceIssue,omitempty"` // 真实 issue 改造时的出处（白名单来源）
}

// Session 一次沙盒会话。
type Session struct {
	Case    *BreakCase `json:"case"`
	Root    string     `json:"root"`
	Created time.Time  `json:"created"`
}

// PathSafety 路径安全：目标路径必须落在 root 内（防逃逸）。
func PathSafety(root, target string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return fmt.Errorf("路径越界（沙盒外）: %s", target)
	}
	return nil
}

// Create 开沙盒：把案例文件写入 root（已存在非空目录拒绝，防误伤）。
func Create(root string, c *BreakCase) (*Session, error) {
	if len(c.Files) == 0 {
		return nil, fmt.Errorf("案例无文件")
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("工作区非空（%s）——用新目录或先清场", root)
	}
	s := &Session{Case: c, Root: root, Created: time.Now()}
	if err := s.materialize(); err != nil {
		return nil, err
	}
	return s, nil
}

// materialize 落盘案例文件（逐个过路径安全）。
func (s *Session) materialize() error {
	for rel, content := range s.Case.Files {
		target := filepath.Join(s.Root, rel)
		if err := PathSafety(s.Root, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Verify 确定性验收：读取工作区当前文件（相对路径集合同案例），交给 Checker。
func (s *Session) Verify() (CheckResult, error) {
	current := map[string]string{}
	for rel := range s.Case.Files {
		target := filepath.Join(s.Root, rel)
		if err := PathSafety(s.Root, target); err != nil {
			return CheckResult{}, err
		}
		b, err := os.ReadFile(target)
		if err != nil {
			current[rel] = "" // 用户删了文件也是「当前状态」
			continue
		}
		current[rel] = string(b)
	}
	return s.Case.Check(current), nil
}

// VerifyWith 外部验收器（可选注入：如 go test 执行器）。
func (s *Session) VerifyWith(fn Checker) (CheckResult, error) {
	current := map[string]string{}
	for rel := range s.Case.Files {
		b, _ := os.ReadFile(filepath.Join(s.Root, rel))
		current[rel] = string(b)
	}
	return fn(current), nil
}
