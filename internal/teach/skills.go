// Package teach 把「助教怎么教」从提示词里搬出来，变成可独立修改、可测试的
// 技能包（Skill）。格式遵循 Agent Skills 规范：
//
//	skill-name/
//	├── SKILL.md          # 必需：YAML frontmatter + 正文
//	├── scripts/          # 可选
//	└── references/       # 可选
//
// 设计取舍（Go 侧）：
//   - 用嵌入文件系统打包，二进制自带技能，部署时不依赖工作目录
//   - 只解析 frontmatter 里 name/description 两个必需字段；其余字段保留在 Raw 里，
//     不提前抽象（YAGNI），等真有消费者再加
//   - 按 Agent Skills 的「渐进披露」原则：Metadata 列表很小（name+description），
//     正文只在真正要用时才取 Body
package teach

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed skills/*/SKILL.md
var skillFS embed.FS

// Skill 一条教学技能。
type Skill struct {
	Name        string `json:"name"`        // 与目录同名，小写字母/数字/连字符
	Description string `json:"description"` // 做什么 + 什么时候用
	Body        string `json:"body"`        // 正文（按需加载，列表接口不返回）
}

// Meta 只含元信息，用于列表接口（渐进披露第一层）。
type Meta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

var (
	once   sync.Once
	cached []Skill
	err    error
)

// All 返回全部技能（按名字排序）。结果只解析一次，之后走缓存。
func All() ([]Skill, error) {
	once.Do(func() { cached, err = load() })
	if err != nil {
		return nil, err
	}
	out := make([]Skill, len(cached))
	copy(out, cached)
	return out, nil
}

// Metas 返回全部技能的元信息（不含正文）。
func Metas() ([]Meta, error) {
	skills, e := All()
	if e != nil {
		return nil, e
	}
	out := make([]Meta, 0, len(skills))
	for _, s := range skills {
		out = append(out, Meta{Name: s.Name, Description: s.Description})
	}
	return out, nil
}

// Get 按名字取一条技能。
func Get(name string) (Skill, bool, error) {
	skills, e := All()
	if e != nil {
		return Skill{}, false, e
	}
	for _, s := range skills {
		if s.Name == name {
			return s, true, nil
		}
	}
	return Skill{}, false, nil
}

// load 解析嵌入的技能目录。
func load() ([]Skill, error) {
	entries, e := fs.ReadDir(skillFS, "skills")
	if e != nil {
		return nil, fmt.Errorf("读技能目录: %w", e)
	}
	var out []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := entry.Name()
		raw, e := skillFS.ReadFile(path.Join("skills", dir, "SKILL.md"))
		if e != nil {
			return nil, fmt.Errorf("读 %s/SKILL.md: %w", dir, e)
		}
		s, e := parse(dir, string(raw))
		if e != nil {
			return nil, fmt.Errorf("%s: %w", dir, e)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有任何技能")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parse 解析 SKILL.md：必须是 --- 包起来的 frontmatter，取 name/description，
// 剩下的是正文。name 必须与目录名一致（规范要求）。
func parse(dir, raw string) (Skill, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return Skill{}, fmt.Errorf("缺少 frontmatter（文件必须以 --- 开头）")
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Skill{}, fmt.Errorf("frontmatter 没有闭合的 ---")
	}
	front, body := rest[:end], rest[end+len("\n---"):]
	body = strings.TrimLeft(body, "\n")

	var s Skill
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.TrimSpace(key) {
		case "name":
			s.Name = val
		case "description":
			s.Description = val
		}
	}
	if s.Name == "" {
		return Skill{}, fmt.Errorf("frontmatter 缺 name")
	}
	if s.Name != dir {
		return Skill{}, fmt.Errorf("name %q 与目录名 %q 不一致（规范要求同名）", s.Name, dir)
	}
	if s.Description == "" {
		return Skill{}, fmt.Errorf("frontmatter 缺 description")
	}
	if len([]rune(s.Description)) > 1024 {
		return Skill{}, fmt.Errorf("description 超过 1024 字")
	}
	if strings.TrimSpace(body) == "" {
		return Skill{}, fmt.Errorf("正文为空")
	}
	s.Body = body
	return s, nil
}
