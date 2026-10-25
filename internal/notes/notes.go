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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, File{Name: e.Name(), Size: int(info.Size()), ModTime: info.ModTime().Format("2006-01-02T15:04:05")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out
}

// Read 读一篇笔记全文（name 需在 List 结果内——防路径逃逸）。
func Read(dir, name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || !strings.HasSuffix(strings.ToLower(name), ".md") {
		return "", os.ErrInvalid
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.Base(name)))
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
