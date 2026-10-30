package arch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 允许忽略的错误（三档，见 docs/eng/error-policy.md）：
//
//  1. 必须上报——涉及数据一致性、用户可见状态、外部副作用
//  2. 允许尽力而为——写备份、日志、清理临时文件、编码失败退化
//  3. 禁止忽略——安全相关（密钥、权限），必须现场处理
//
// 这里只做「不许变多」的棘轮：把当前基线钉住，新代码想忽略就改这条预算，
// 改预算就意味着必须在 PR 里说清理由——这就是约束的来源。
const ignoredErrorBudget = 74

// TestIgnoredErrorBudget 统计 `_ =` 形式的忽略点，只许减不许增。
func TestIgnoredErrorBudget(t *testing.T) {
	root := repoRoot(t)
	total := 0
	var hot []string

	for _, dir := range []string{"internal", "cmd"} {
		base := filepath.Join(root, dir)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			n := 0
			for _, line := range strings.Split(string(b), "\n") {
				trimmed := strings.TrimSpace(line)
				// 只认「开头就是 _ =」的显式忽略；函数签名里的 _ 不算
				if strings.HasPrefix(trimmed, "_ = ") || strings.HasPrefix(trimmed, "_, _ = ") {
					n++
				}
			}
			if n > 0 {
				rel, _ := filepath.Rel(root, path)
				hot = append(hot, rel)
				total += n
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if total > ignoredErrorBudget {
		t.Fatalf("被忽略的错误涨到 %d 处（预算 %d）。\n"+
			"要么现场处理，要么改预算并在说明里写清为什么这次可以忽略。\n涉及文件：%s",
			total, ignoredErrorBudget, strings.Join(hot, ", "))
	}
	t.Logf("被忽略的错误 %d 处（预算 %d）", total, ignoredErrorBudget)
}
