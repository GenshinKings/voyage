// Package notes — 学习者的笔记工作区（outpost2-data/notes/*.md）。
//
// 用户 2026-10-25 指令：一开始就设好笔记文件夹；AI 助教每天根据教学内容
// 检查笔记是否有问题、可如何改进（tutor.NoteReview 巡检，诊断结果主动可见）。
// 设计约束：笔记是学习者的资产——本包只读不写（诊断报告不落在 notes/ 内，
// 而是放 fetch.json，绝不污染用户的笔记文件夹）。
package notes

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirName 数据目录下的笔记子目录名。
const DirName = "notes"

// File 一篇笔记的元信息。
type File struct {
	Name    string `json:"name"` // 文件名（含 .md）
	Size    int    `json:"size"`
	ModTime string `json:"modTime"` // RFC3339——诊断时判断新旧
}

// Ensure 确保 notes 目录存在（返回路径；失败返回空串）。
// ValidDir 判断一个路径是否是可用的笔记目录（给「切换笔记仓库」用）。
// 放在本包而不是 server：server 的直接文件操作有预算闸门。
func ValidDir(dir string) bool {
	st, err := os.Stat(dir)
	return err == nil && st.IsDir()
}
func Ensure(dataDir string) string {
	dir := filepath.Join(dataDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// List 列出全部笔记（.md，按修改时间新→旧；首启为空列表=正常态）。
func List(dir string) []File {
	out := []File{}
	root, err := filepath.Abs(dir)
	if err != nil {
		return out
	}
	// 递归：Obsidian 库的笔记通常在子目录里；跳过 Obsidian 自己的元数据目录
	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".obsidian", ".trash", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = name
		}
		out = append(out, File{Name: filepath.ToSlash(rel), Size: int(info.Size()), ModTime: info.ModTime().Format("2006-01-02T15:04:05")})
		return nil
	})
	if walkErr != nil && len(out) == 0 {
		return out // 整个目录都读不了才放弃；部分失败时保留已读到的笔记
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out
}

// Read 读一篇笔记全文（name 需在 List 结果内——防路径逃逸）。
func Read(dir, name string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		return "", os.ErrInvalid
	}
	root, absErr := filepath.Abs(dir)
	if absErr != nil {
		return "", absErr
	}
	full, joinErr := filepath.Abs(filepath.Join(root, filepath.FromSlash(name)))
	if joinErr != nil {
		return "", joinErr
	}
	// 防路径逃逸：解析后必须仍在根目录内
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", os.ErrInvalid
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Transcript 全部笔记拼成诊断输入（每篇带文件名分隔；总量截断防上下文膨胀）。
func Transcript(dir string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = 24000
	}
	var b strings.Builder
	for _, f := range List(dir) {
		content, err := Read(dir, f.Name)
		if err != nil {
			continue
		}
		if b.Len()+len(content) > maxBytes {
			b.WriteString("\n\n## （更多笔记已截断）\n")
			break
		}
		b.WriteString("\n\n## 笔记：" + f.Name + "（最后修改 " + f.ModTime + "）\n\n")
		b.WriteString(content)
	}
	return strings.TrimSpace(b.String())
}
