package server

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// learnerMaterial 第六轮-P1-2：从学习者的真实代码目录抽一份出题摘要。
// 只取最近修改的 3 个 .go 文件（先收窄到沙箱记忆目录；内容截断防巨型文件），
// 摘要=文件名+函数/类型名+片段（每文件最多 24 行）；读不到任何东西返回空串=出题退回通用例子。
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
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
	fnRe := regexp.MustCompile(`(?m)^func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)`)
	clRe := regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)`)
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
		for _, m := range fnRe.FindAllStringSubmatch(src, -1) {
			syms = append(syms, m[1]+"()")
		}
		for _, m := range clRe.FindAllStringSubmatch(src, -1) {
			syms = append(syms, "type "+m[1])
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
	return out + fmt.Sprintf("\n（取材自动生成：%s 下最近修改的 Go 文件；出题引用真实文件名与函数名）", dir)
}
