// Package iterate — 助教自迭代的确定性护栏与闸门（AI 主导，但不信任 AI）。
//
// 职责：把「助教改代码」限制在安全范围内，并保证仓库永远处于可构建状态——
//   - SafePath：读写白名单（internal/ 下 + 根 *.go；outpost2-data/cmd/go.mod 永不可碰）
//   - RunTests/Build：改动后的确定性闸门（不过即 Restore，绝不带病上岗）
//   - Commit：只 add internal/（白名单内），只 commit 不 push
//
// 运行中的 voyage.exe 永不被触碰（构建产物是 voyage-next.exe，重启换版是人的决定）。
package iterate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MaxFileBytes 单文件写入上限（防膨胀；真要大改说明该走人工评估）。
const MaxFileBytes = 128 << 10

// SafePath 白名单校验：rel 必须落在 repo/internal/ 下（或仓库根的 .go 文件），返回绝对路径。
func SafePath(repo, rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", fmt.Errorf("路径为空")
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("越界路径 %q", rel)
	}
	allowed := strings.HasPrefix(clean, "internal/") || (filepath.Dir(clean) == "." && strings.HasSuffix(clean, ".go"))
	if !allowed {
		return "", fmt.Errorf("只允许读写 internal/ 与根目录 .go 文件，%q 不在白名单", rel)
	}
	return filepath.Join(repo, clean), nil
}

func run(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RunTests 全量测试（闸门一）。输出供报告引用。
func RunTests(repo string) (string, error) {
	out, err := run(repo, 6*time.Minute, "go", "test", "./...")
	if err != nil {
		return out, fmt.Errorf("go test 失败: %s", tail(out, 400))
	}
	return out, nil
}

// Build 构建到 voyage-next.exe（闸门二；不碰运行中的 voyage.exe）。
func Build(repo string) (string, error) {
	out, err := run(repo, 3*time.Minute, "go", "build", "-o", "voyage-next.exe", "./cmd/voyage")
	if err != nil {
		return out, fmt.Errorf("go build 失败: %s", tail(out, 400))
	}
	return out, nil
}

// Commit 提交白名单内改动（只 add internal/，只 commit 不 push）。
// 无实际改动时返回可识别错误（不是故障——助教可能只做了等价改写）。
func Commit(repo, msg string) error {
	if _, err := run(repo, time.Minute, "git", "add", "internal/"); err != nil {
		return fmt.Errorf("git add 失败: %s", tail(err.Error(), 200))
	}
	if _, err := run(repo, time.Minute, "git", "diff", "--cached", "--quiet"); err == nil {
		// --quiet 语义：无差异 exit 0 —— staged 为空 = 实现是等价改写
		return fmt.Errorf("没有实际改动（internal/ 与上次提交一致）——观察可能无需代码变更，或实现只是等价改写")
	}
	if out, err := run(repo, time.Minute, "git", "-c", "core.editor=true", "commit", "-m", msg); err != nil {
		return fmt.Errorf("git commit 失败: %s", tail(out+" | "+err.Error(), 300))
	}
	return nil
}

// Restore 还原白名单内改动（闸门失败时调用——仓库回到干净态）。
func Restore(repo string) {
	_, _ = run(repo, time.Minute, "git", "checkout", "--", "internal/")
	_, _ = run(repo, time.Minute, "git", "clean", "-fd", "internal/")
	_ = os.Remove(filepath.Join(repo, "voyage-next.exe"))
}

// tail 错误输出截尾（报告用）。
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
