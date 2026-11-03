// Package project —— 「给一个项目 + 一份 README，我自己做，助教对照验收」。
//
// 由来（主人 2026-11-03 23:03）：
// 「我需要的是助教给我在某个合适的地方创一个文件夹，给我一个需求的 readme 文件，
//
//	我按照 readme 来完成项目后，助教检查完成情况。」
//
// 设计要点：README 里的验收项必须**可执行**——「跑什么命令 → 该看到什么」。
// 否则「检查完成情况」就变成助教说了算，等于没验。
package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// CheckItem 一条验收项。
type CheckItem struct {
	Cmd  string `json:"cmd"`  // 跑什么（白名单内的命令）
	Want string `json:"want"` // 输出里该出现什么
	Note string `json:"note"` // 人话说明这一条在验什么
}

// README 是写进项目里的那份。
type README struct {
	Title  string      `json:"title"`
	Goal   string      `json:"goal"`   // 一句话：做完能得到什么
	Steps  []string    `json:"steps"`  // 自己动手的步骤（不含答案）
	Checks []CheckItem `json:"checks"` // 可执行验收
	Terms  []string    `json:"terms"`  // 会用到但没解释的术语（空白更好）
}

// Markdown 渲染成 README.md。
func (r README) Markdown(dir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", r.Title)
	fmt.Fprintf(&b, "> 这个项目放在 `%s`。你自己做，做完让助教按下面的验收逐条检查。\n\n", dir)
	fmt.Fprintf(&b, "## 做完能得到什么\n\n%s\n\n", r.Goal)
	b.WriteString("## 你要做的\n\n")
	for i, s := range r.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	b.WriteString("\n## 怎么算做完（助教会逐条跑）\n\n")
	for i, c := range r.Checks {
		fmt.Fprintf(&b, "- [ ] **%d. %s**\n  - 跑：`%s`\n  - 该看到：`%s`\n", i+1, c.Note, c.Cmd, c.Want)
	}
	b.WriteString("\n## 规则\n\n")
	b.WriteString("- 卡住超过 15 分钟再问助教；问之前先说「我试过什么」。\n")
	b.WriteString("- 不许让助教直接写代码；可以让他指出「下一步看哪一块」。\n")
	b.WriteString("- 验收没过就留在这一条，不往下走。\n")
	return b.String()
}

const readmeSystem = `你是项目出题人。给一个完全新手出一个「小项目」，让他自己动手做完。

硬规则：
1. 项目要小：2~4 个文件能完成，30~90 分钟做得完。
2. 只给需求，不给答案：步骤写「做什么」，不写代码。
3. 验收必须可执行：每条给一个命令（go run main.go / go test ./... / ls 之类）
   和「输出里该出现什么」。**want 必须是字面字符串**——就是程序运行时屏幕上真会出现的
   那几个字符（如 "25:00"、"时间到"、"PASS"），不许写「出现 25:00 这样的起始时间」
   这种描述句，否则没法自动比对。挑一个最短又能证明这一条的字面量。
4. 不许出现他没学过的术语；必须用的名词，在 goal 里先用一句大白话解释。
5. 3~5 条验收，覆盖「能跑起来 / 核心行为对 / 边界对」三类。

只输出 JSON：
{"title":"项目名","goal":"一句话","steps":["步骤1","步骤2"],"checks":[{"cmd":"go run main.go","want":"倒计时 05:00","note":"能跑起来并显示初始时间"}],"terms":["术语"]}`

// ChatFunc 便于测试注入。
type ChatFunc func(ctx context.Context, system, user string) (string, error)

// Generate 让模型写出题目。root 是练习根目录。
func Generate(ctx context.Context, chat ChatFunc, brief string) (README, error) {
	user := "学习者想做：\n" + brief + "\n\n请出题。"
	var lastErr error
	// 不合格就带着具体原因重出一次——出题人也需要被告知哪里不合格，
	// 直接退回给界面等于把难题甩给学习者。
	for attempt := 0; attempt < 2; attempt++ {
		out, err := chat(ctx, readmeSystem, user)
		if err != nil {
			return README{}, err
		}
		r, perr := Parse(out)
		if perr != nil {
			lastErr = perr
			user += "\n\n上一次输出不是合法 JSON，请只输出 JSON。"
			continue
		}
		if verr := Validate(r); verr != nil {
			lastErr = verr
			user += "\n\n上一次不合格：" + verr.Error() + "\n请重出，特别注意「该看到」只给程序真会打出来的那一小段（不要前缀、不要空格）。"
			continue
		}
		return r, nil
	}
	return README{}, lastErr
}

// Parse 抠 JSON。
func Parse(raw string) (README, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return README{}, fmt.Errorf("出题人没给出可解析的 JSON")
	}
	var r README
	if err := json.Unmarshal([]byte(raw[start:end+1]), &r); err != nil {
		return README{}, fmt.Errorf("出题 JSON 坏了：%w", err)
	}
	return r, nil
}

// Validate 出题质量闸门——不合格就退回去重出，不许把烂题给学生。
func Validate(r README) error {
	if strings.TrimSpace(r.Title) == "" || strings.TrimSpace(r.Goal) == "" {
		return fmt.Errorf("题目缺标题或目标")
	}
	if len(r.Steps) == 0 {
		return fmt.Errorf("题目没有步骤")
	}
	if len(r.Checks) < 3 {
		return fmt.Errorf("验收少于 3 条（要有：能跑起来 / 核心行为 / 边界）")
	}
	for i, c := range r.Checks {
		if strings.TrimSpace(c.Cmd) == "" || strings.TrimSpace(c.Want) == "" {
			return fmt.Errorf("第 %d 条验收缺命令或期望输出——那样就不是可执行验收", i+1)
		}
		if descriptive(c.Want) {
			return fmt.Errorf("第 %d 条验收的「该看到」写成了描述句（%q），要改成程序真会打出来的字面量", i+1, c.Want)
		}
		// 带空格的「期望」几乎都是「描述性前缀 + 真值」（如 "倒计时 25:00"），
		// 程序不会原样打出这串字，比对必然失败。只接受最短的那一段。
		if strings.ContainsAny(c.Want, " \t") {
			return fmt.Errorf("第 %d 条验收的「该看到」里带了空格（%q）——只留程序真会打出来的那一小段", i+1, c.Want)
		}
	}
	return nil
}

// PickRoot 从候选里挑第一个「父目录存在」的作为练习根；都没有就用最后一个。
// 放这里而不是 server：server 有「不许直接操作文件系统」的架构闸门。
func PickRoot(candidates []string) string {
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Dir(c)); err == nil {
			return c
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[len(candidates)-1]
}

// EnsureRoot 建练习根目录。
func EnsureRoot(root string) error {
	if root == "" {
		return fmt.Errorf("练习目录是空路径")
	}
	return os.MkdirAll(root, 0o755)
}

// descriptive 判断「该看到」是不是写成了描述句而不是字面量。
// 描述句没法自动比对，会让验收永远 0 条过——看起来像学生全错，其实是题写坏了。
func descriptive(want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return true
	}
	for _, bad := range []string{"出现", "类似", "之类", "看起来", "这样的", "例如", "应该会", "正常", "显示"} {
		if strings.Contains(want, bad) {
			return true
		}
	}
	return false
}

// Create 建文件夹 + 写 README + 放一个空骨架占位。
func Create(root string, r README) (string, error) {
	slug := slugify(r.Title)
	if slug == "" {
		return "", fmt.Errorf("项目名不能为空")
	}
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	readme := r.Markdown(dir)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
		return "", err
	}
	// 机器可读的一份：检查完成情况时读它，不去解析 Markdown（解析文本太脆）。
	if b, err := json.MarshalIndent(r, "", "  "); err == nil {
		if err := os.WriteFile(filepath.Join(dir, SpecFile), b, 0o644); err != nil {
			return "", err
		}
	}
	// 只放一个空占位，不替他写代码——空文件也要说明「这是你要填的」。
	hint := "package main\n\n// TODO: 按 README 自己写。写不出来先看 README 的「你要做的」。\nfunc main() {\n}\n"
	p := filepath.Join(dir, "main.go")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.WriteFile(p, []byte(hint), 0o644); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// SpecFile 题目机器可读版，和 README.md 并排放。
const SpecFile = ".practice.json"

// Load 读回题目；没有 spec 文件就报错（说明这个目录不是我们建的）。
func Load(dir string) (README, error) {
	b, err := os.ReadFile(filepath.Join(dir, SpecFile))
	if err != nil {
		return README{}, fmt.Errorf("这个目录里没有 %s，不是练习项目目录", SpecFile)
	}
	var r README
	if err := json.Unmarshal(b, &r); err != nil {
		return README{}, fmt.Errorf("%s 坏了：%w", SpecFile, err)
	}
	return r, nil
}

// Result 一条验收的检查结果。
type Result struct {
	Note  string `json:"note"`
	Cmd   string `json:"cmd"`
	Want  string `json:"want"`
	Got   string `json:"got"`
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
}

// 只允许这些命令前缀：白名单，避免"验收"变成任意命令执行。
var allowPrefix = []string{"go run", "go test", "go build", "go vet", "ls", "dir", "cat", "type", "sleep", "timeout"}

func allowed(cmd string) bool {
	// 允许 a && b 串联，但每一段都必须在白名单里——
	// 「sleep 3 && go run main.go」这种验收是合理的（等三秒看数字变了）。
	for _, seg := range strings.Split(cmd, "&&") {
		c := strings.TrimSpace(seg)
		if c == "" {
			return false
		}
		ok := false
		for _, p := range allowPrefix {
			if strings.HasPrefix(c, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// Check 逐条跑验收。不通过的不判「失败」而是判「还没做完」。
func Run(dir string, r README) []Result {
	var out []Result
	for _, c := range r.Checks {
		res := Result{Note: c.Note, Cmd: c.Cmd, Want: c.Want}
		if !allowed(c.Cmd) {
			res.Error = "这条命令不在白名单里（只能跑 go run / go test / ls 这类）"
			out = append(out, res)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, execShell(), execFlag(), platformCmd(c.Cmd))
		cmd.Dir = dir
		b, err := cmd.CombinedOutput()
		cancel()
		res.Got = string(b)
		if err != nil && len(b) == 0 {
			res.Error = err.Error()
		}
		res.Pass = strings.Contains(res.Got, c.Want)
		out = append(out, res)
	}
	return out
}

// platformCmd 把通用命令名换成当前系统真有的：
// cmd 里没有 ls/cat，硬跑会说「不是内部或外部命令」，看起来像学生没做。
func platformCmd(c string) string {
	if runtime.GOOS != "windows" {
		return c
	}
	c = strings.TrimSpace(c)
	switch {
	case strings.HasPrefix(c, "ls"):
		return "dir /b" + strings.TrimPrefix(c, "ls")
	case strings.HasPrefix(c, "cat "):
		return "type " + strings.TrimPrefix(c, "cat ")
	}
	return c
}

// Windows 用 cmd，其他用 sh。
func execShell() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

func execFlag() string {
	if runtime.GOOS == "windows" {
		return "/c"
	}
	return "-c"
}
func slugify(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		// 中文标题兜底：用时间戳，避免空目录名
		return "project-" + time.Now().Format("0102-1504")
	}
	return out
}
