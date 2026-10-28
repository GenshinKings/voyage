package naming

// naming_test.go — 第八轮-P1-3 验收：除白名单外，源码里出现旧名字面量（见 doc.go 拼写）即失败。
// 白名单：outpost2-data（数据目录历史名）与 docs/（历史评审档案）。
// CI：go test ./... 即本检查；.github/workflows/ci.yml 另有同口径 grep 步骤双保险。
import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	// 本包位于 internal/naming，仓库根 = 上两级
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// oldName 旧名的运行时拼装——测试自身不出现该字面量（否则自抓）。
func oldName() string { return "outpost" + "2" }

func TestNoOutpost2LiteralsOutsideWhitelist(t *testing.T) {
	root := repoRoot(t)
	var bad []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "outpost2-data" {
				return filepath.SkipDir
			}
			if rel == "docs" {
				return filepath.SkipDir // 历史评审档案：不改写
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".go" && ext != ".md" && ext != ".cmd" {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, oldName()) && !strings.Contains(line, "outpost2-data") {
				bad = append(bad, rel+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		t.Fatalf("源码出现 outpost2 字面量（白名单：outpost2-data / docs/），共 %d 处：\n%s",
			len(bad), strings.Join(bad, "\n"))
	}
}
