// Package arch 放「架构约束」的自动检查：这些规则靠自觉守不住，靠测试守。
//
// 两条规矩：
//  1. 谁都不许反向依赖 internal/server —— 它是传输层，不许被别人 import。
//  2. internal/server 里直接碰文件系统的次数只能降不能升（棘轮预算）。
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "voyage/"

// repoRoot 从当前测试目录往上找到含 go.mod 的目录。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("找不到 go.mod")
	return ""
}

// TestNoBackwardsImportToServer：internal 下的包不许 import internal/server。
func TestNoBackwardsImportToServer(t *testing.T) {
	root := repoRoot(t)
	internal := filepath.Join(root, "internal")
	fset := token.NewFileSet()

	var offenders []string
	err := filepath.Walk(internal, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(internal, path)
		pkg := strings.Split(filepath.ToSlash(rel), "/")[0]
		if pkg == "server" {
			return nil // server 自己可以引用自己
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == modulePath+"internal/server" || strings.HasPrefix(p, modulePath+"internal/server/") {
				offenders = append(offenders, rel+" -> "+p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("有包反向依赖了传输层 server（会让依赖图变成环）：\n%s", strings.Join(offenders, "\n"))
	}
}

// fileOpsBudget 是当前基线。每次降下来就把数字改小，不许改大。
const fileOpsBudget = 14

var fileOps = []string{
	"os.ReadFile(", "os.WriteFile(", "os.ReadDir(", "os.Rename(",
	"os.Remove(", "os.Stat(", "os.MkdirAll(",
}

// TestServerFileOpsBudget：server 里直接操作文件系统的地方只能减少。
func TestServerFileOpsBudget(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "server")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	total := 0
	var detail []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		n := 0
		for _, fn := range fileOps {
			n += strings.Count(src, fn)
		}
		if n > 0 {
			detail = append(detail, name+": "+strconv.Itoa(n))
			total += n
		}
	}

	if total > fileOpsBudget {
		t.Fatalf("server 直接操作文件系统的次数涨到 %d（基线 %d），文件明细：%s\n"+
			"新代码请走 internal/store；如果这次是刻意增加，先说明理由再把预算改大。",
			total, fileOpsBudget, strings.Join(detail, ", "))
	}
	t.Logf("server 直接文件操作 %d 次（预算 %d）", total, fileOpsBudget)
}

// TestSingleAStmtPerLine 不是风格洁癖：多语句一行会让 gofmt -l 之外的检查漏掉遮蔽。
func TestNoDotImport(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	var offenders []string
	_ = filepath.Walk(filepath.Join(root, "internal"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		for _, imp := range f.Imports {
			if imp.Name != nil && imp.Name.Name == "." {
				offenders = append(offenders, path)
			}
		}
		return nil
	})
	if len(offenders) > 0 {
		t.Fatalf("发现 dot import（会污染命名空间）：%s", strings.Join(offenders, ", "))
	}
}
