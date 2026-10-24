// Package reflib — 教学参考库：教务策展抓取的原文资料（项目 README / 教程文档），
// 统一存放在数据目录的 kb/ 子目录：一个清单 index.json + 每篇一个 md 文件。
//
// 用途（用户 2026-10-24 指令）：每次抓的项目都放同一个文件夹，且作为 AI 助教
// 教学的参考——教学调用注入清单摘要，讲解优先依据原文（AI 教材必须最准确，
// 防脱轨）。升级路径（README 路线图「知识库三层」）：按知识点锚定原文段落 →
// 检索式注入（internal/kb 三层引擎届时接管）。
package reflib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DirName 数据目录下的参考库子目录名。
const DirName = "kb"

// IndexFile 清单文件名（位于 kb/ 内）。
const IndexFile = "index.json"

// Doc 一篇参考资料的清单条目。
type Doc struct {
	ID     string `json:"id"`            // 如 20261024-security-audit-skill
	Title  string `json:"title"`         // 人读标题（注入用）
	URL    string `json:"url,omitempty"` // 原文来源
	Domain string `json:"domain,omitempty"`
	Date   string `json:"date,omitempty"`   // 抓取日期 2026-10-24
	Digest string `json:"digest,omitempty"` // 一句话摘要（≤60 字，注入用）
	File   string `json:"file"`             // kb/ 内的文件名
}

// Index 参考库清单。
type Index struct {
	Docs []Doc `json:"docs"`
}

// Ensure 确保 kb 目录存在（返回路径；失败返回空串）。
func Ensure(dataDir string) string {
	dir := filepath.Join(dataDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

// LoadIndex 读清单（缺失/损坏返回空清单，不阻塞启动）。
func LoadIndex(dir string) Index {
	var idx Index
	if dir == "" {
		return idx
	}
	if b, err := os.ReadFile(filepath.Join(dir, IndexFile)); err == nil {
		_ = json.Unmarshal(b, &idx)
	}
	return idx
}

// SaveIndex 原子写清单（.tmp → rename）。
func SaveIndex(dir string, idx Index) error {
	b, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, IndexFile+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, IndexFile))
}

// Add 入库一篇原文：写 md 文件 + 清单幂等追加/更新（按 ID 去重，重复入库=覆盖）。
func Add(dir string, d Doc, content string) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Title) == "" {
		return os.ErrInvalid
	}
	if d.File == "" {
		d.File = SanitizeID(d.ID) + ".md"
	}
	if err := os.WriteFile(filepath.Join(dir, d.File), []byte(content), 0o644); err != nil {
		return err
	}
	idx := LoadIndex(dir)
	for i := range idx.Docs {
		if idx.Docs[i].ID == d.ID {
			idx.Docs[i] = d
			return SaveIndex(dir, idx)
		}
	}
	idx.Docs = append(idx.Docs, d)
	return SaveIndex(dir, idx)
}

// Digest 清单的注入文本（教学 system 用）：每篇一行「- 《标题》：摘要」。
// max 条数上限（防上下文膨胀）；空清单返回空串（不注入）。
func Digest(dir string, max int) string {
	var b strings.Builder
	n := 0
	for _, d := range LoadIndex(dir).Docs {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		if n >= max {
			break
		}
		line := "- 《" + d.Title + "》"
		if d.Digest != "" {
			line += "：" + d.Digest
		}
		b.WriteString(line + "\n")
		n++
	}
	return strings.TrimSpace(b.String())
}

// SanitizeID 清洗 id 为安全文件名成分（字母/数字/-/_，其余丢弃）。
func SanitizeID(s string) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		}
	}
	if b.Len() == 0 {
		return "doc"
	}
	return b.String()
}
