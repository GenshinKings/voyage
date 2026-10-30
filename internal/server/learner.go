package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// 出题取材支持的语言：原先只认 .go，等于把非 Go 学习者（Python/前端/Java…）
// 全部挡在「真实案例出题」门外——产品最初的立意就是这么被饿死的。
var materialExts = []string{
	".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".vue", ".svelte",
	".java", ".kt", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp",
	".cs", ".rb", ".php", ".swift", ".sh", ".sql", ".lua", ".dart", ".scala",
}

// 测试文件不进材料：出题要引用真实业务代码，不是断言。
var testNameHints = []string{"_test.", ".test.", ".spec.", "_spec.", "test_", "tests."}

func isMaterialFile(name string) bool {
	lower := strings.ToLower(name)
	ext := strings.ToLower(filepath.Ext(lower))
	ok := false
	for _, e := range materialExts {
		if ext == e {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	for _, h := range testNameHints {
		if strings.Contains(lower, h) {
			return false
		}
	}
	return true
}

// learnerMaterial 第六轮-P1-2：从学习者的真实代码目录抽一份出题摘要。
// 取最近修改的 3 个源码文件（多语言；跳过测试文件；内容截断防巨型文件），
// 摘要=文件名+符号+片段（每文件最多 24 行）；读不到任何东西返回空串=出题退回通用例子。
// 安全边界：只读用户在前端沙箱流程里自己给出的目录，不触 outpost2-data。
func learnerMaterial(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type cand struct {
		name string
		mod  int64
	}
	var cs []cand
	for _, e := range entries {
		if e.IsDir() || !isMaterialFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cs = append(cs, cand{name: e.Name(), mod: info.ModTime().Unix()})
	}
	if len(cs) == 0 {
		return ""
	}
	sort.Slice(cs, func(i, j int) bool { return cs[i].mod > cs[j].mod })
	if len(cs) > 3 {
		cs = cs[:3]
	}

	// Go 风格
	fnRe := regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	clRe := regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)`)
	// 其它语言的函数/类：够用来给题面点出真实名字，不求完整解析
	genFnRe := regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:async\s+)?(?:def|function|fn|sub)\s+([A-Za-z_]\w*)`)
	genClRe := regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:public\s+|private\s+|protected\s+|internal\s+)?(?:abstract\s+)?(?:class|interface|struct|enum|trait|object)\s+([A-Za-z_]\w*)`)

	var b strings.Builder
	for _, c := range cs {
		raw, err := os.ReadFile(filepath.Join(dir, c.name))
		if err != nil {
			continue
		}
		src := string(raw)
		if len(src) > 12<<10 {
			src = src[:12<<10]
		}
		b.WriteString("◆ " + c.name + "\n")
		syms := []string{}
		seen := map[string]bool{}
		add := func(s string) {
			if s == "" || seen[s] {
				return
			}
			seen[s] = true
			syms = append(syms, s)
		}
		for _, m := range fnRe.FindAllStringSubmatch(src, -1) {
			add(m[1] + "()")
		}
		for _, m := range genFnRe.FindAllStringSubmatch(src, -1) {
			add(m[1] + "()")
		}
		for _, m := range clRe.FindAllStringSubmatch(src, -1) {
			add("type " + m[1])
		}
		for _, m := range genClRe.FindAllStringSubmatch(src, -1) {
			add(m[1])
		}
		if len(syms) > 0 {
			b.WriteString("  符号：" + strings.Join(syms, "、") + "\n")
		}
		lines := strings.Split(strings.TrimRight(src, "\n"), "\n")
		if len(lines) > 24 {
			lines = lines[:24]
		}
		b.WriteString(strings.Join(lines, "\n") + "\n")
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return ""
	}
	if len(out) > 6<<10 {
		out = out[:6<<10]
	}
	return out + fmt.Sprintf("\n（取材自动生成：%s 下最近修改的源码文件；出题引用真实文件名与函数名）", dir)
}
