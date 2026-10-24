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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirName 数据目录下的参考库子目录名。
const DirName = "kb"

// IndexFile 清单文件名（位于 kb/ 内）。
const IndexFile = "index.json"

// NavFile 导航文件名（位于 kb/ 内，人读的库存总览，每次入库自动更新）。
const NavFile = "导航.md"

// Doc 一篇参考资料的清单条目。
type Doc struct {
	ID     string `json:"id"`            // 如 20261024-security-audit-skill
	Title  string `json:"title"`         // 人读标题（注入用）
	URL    string `json:"url,omitempty"` // 原文来源
	Domain string `json:"domain,omitempty"`
	Date   string `json:"date,omitempty"`   // 抓取日期 2026-10-24
	Digest string `json:"digest,omitempty"` // 一句话摘要（≤60 字，注入用）
	File   string `json:"file,omitempty"`   // kb/ 内的文件名（库内文档）
	Path   string `json:"path,omitempty"`   // 本地磁盘路径（外部资料引用，原文不拷贝）
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

// Add 入库一条参考资料（按 ID 幂等，重复入库=覆盖）：
//   - 库内文档：d.Path 为空 → 写 md 文件到 kb/（content 为原文）；
//   - 外部资料：d.Path 给本地路径 → 只登记引用，原文留在原地不拷贝。
//
// 两种方式都会更新清单并自动重生成导航.md（用户 2026-10-24 指令：加新东西导航自动更新）。
func Add(dir string, d Doc, content string) error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Title) == "" {
		return os.ErrInvalid
	}
	if d.Path == "" {
		if d.File == "" {
			d.File = SanitizeID(d.ID) + ".md"
		}
		if err := os.WriteFile(filepath.Join(dir, d.File), []byte(content), 0o644); err != nil {
			return err
		}
	}
	idx := LoadIndex(dir)
	updated := false
	for i := range idx.Docs {
		if idx.Docs[i].ID == d.ID {
			idx.Docs[i] = d
			updated = true
			break
		}
	}
	if !updated {
		idx.Docs = append(idx.Docs, d)
	}
	if err := SaveIndex(dir, idx); err != nil {
		return err
	}
	return SyncNav(dir)
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

// SyncNav 从清单重新生成 导航.md（首次使用=建骨架；每次入库后自动更新）。
// 人读总览：库内文档与外部资料分开列，含摘要/日期/域/路径——AI 助教的「可教知识」
// 全景在这里，教学注入用的清单摘要（Digest）与其同源（index.json）。
func SyncNav(dir string) error {
	if dir == "" {
		return os.ErrInvalid
	}
	idx := LoadIndex(dir)
	internal, external := 0, 0
	for _, d := range idx.Docs {
		if strings.TrimSpace(d.Title) == "" {
			continue
		}
		if d.Path != "" {
			external++
		} else {
			internal++
		}
	}
	var b strings.Builder
	b.WriteString("# 教学参考库导航\n\n")
	b.WriteString("> 自动生成：每次入库自动更新，勿手编（数据源 index.json；入库用 tools/kb_save.py）。\n")
	b.WriteString("> AI 助教每次教学注入本库清单（标题+摘要）——随时知道「我们有哪些可教的知识」。\n\n")
	b.WriteString(fmt.Sprintf("库内文档 %d 篇 · 外部资料 %d 处\n\n", internal, external))
	if internal+external == 0 {
		b.WriteString("（暂无条目——第一篇用 `python tools/kb_save.py` 入库，或登记 G 盘等本地现成资料）\n")
	}
	if internal > 0 {
		b.WriteString("## 库内文档（kb/*.md）\n\n")
		for _, d := range idx.Docs {
			if d.Path != "" || strings.TrimSpace(d.Title) == "" {
				continue
			}
			writeNavEntry(&b, d)
			b.WriteString("  原文：`" + d.File + "`")
			if d.URL != "" {
				b.WriteString(" ｜ 来源：" + d.URL)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if external > 0 {
		b.WriteString("## 外部资料（路径引用，原文在本地磁盘）\n\n")
		for _, d := range idx.Docs {
			if d.Path == "" || strings.TrimSpace(d.Title) == "" {
				continue
			}
			writeNavEntry(&b, d)
			b.WriteString("  路径：`" + d.Path + "`\n")
		}
		b.WriteString("\n")
	}
	tmp := filepath.Join(dir, NavFile+".tmp")
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, NavFile))
}

// writeNavEntry 导航条目公共部分：「- 《标题》— 摘要（日期 · 域 X）」。
func writeNavEntry(b *strings.Builder, d Doc) {
	b.WriteString("- 《" + d.Title + "》")
	if d.Digest != "" {
		b.WriteString(" — " + d.Digest)
	}
	var meta []string
	if d.Date != "" {
		meta = append(meta, d.Date)
	}
	if d.Domain != "" {
		meta = append(meta, "域 "+d.Domain)
	}
	if len(meta) > 0 {
		b.WriteString("（" + strings.Join(meta, " · ") + "）")
	}
	b.WriteString("\n")
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
