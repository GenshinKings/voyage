package notes

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Index 笔记索引：一次扫描，之后按索引取用，不用每次全库重读。
//
// 为什么要索引：Obsidian 库动辄上千篇，每次巡检都全翻一遍既慢又贵，
// 而且没法回答「什么笔记在什么位置」。索引把「目录结构」先固化下来，
// 巡检/提问时再按需挑几篇细读。

// Entry 一篇笔记在索引里的样子。
type Entry struct {
	Path    string `json:"path"`    // 相对仓库根，斜杠分隔
	Dir     string `json:"dir"`     // 所在目录（根为空串）
	Title   string `json:"title"`   // 文件名（不含 .md）
	Size    int    `json:"size"`    // 字节
	ModTime string `json:"modTime"` // RFC3339（便于「最近改过的」）
	First   string `json:"first"`   // 首行非标题文本，便于粗判主题
}

// Index 一份索引。
type Index struct {
	Root    string              `json:"root"`
	BuiltAt string              `json:"builtAt"`
	Entries []Entry             `json:"entries"`
	ByDir   map[string][]string `json:"byDir"` // 目录 → 笔记路径列表
}

// Build 扫描仓库生成索引（跳过 Obsidian 元数据目录）。
func Build(dir string) Index {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Index{Entries: []Entry{}, ByDir: map[string][]string{}}
	}
	idx := Index{Root: root, Entries: []Entry{}, ByDir: map[string][]string{}}
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".obsidian", ".trash", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = name
		}
		rel = filepath.ToSlash(rel)
		dirPart := filepath.ToSlash(filepath.Dir(rel))
		if dirPart == "." {
			dirPart = ""
		}
		e := Entry{
			Path:    rel,
			Dir:     dirPart,
			Title:   strings.TrimSuffix(name, filepath.Ext(name)),
			Size:    int(info.Size()),
			ModTime: info.ModTime().Format("2006-01-02T15:04:05"),
			First:   firstTextLine(path),
		}
		idx.Entries = append(idx.Entries, e)
		idx.ByDir[dirPart] = append(idx.ByDir[dirPart], rel)
		return nil
	})
	if walkErr != nil && len(idx.Entries) == 0 {
		return idx // 整库都读不了才放弃；部分失败保留已扫到的
	}
	sort.Slice(idx.Entries, func(i, j int) bool { return idx.Entries[i].ModTime > idx.Entries[j].ModTime })
	return idx
}

// firstTextLine 取正文第一行有效文本（够粗判主题就行）。
func firstTextLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			continue
		}
		r := []rune(line)
		if len(r) > 80 {
			line = string(r[:80])
		}
		return line
	}
	return ""
}
