// Package profile — 学习者画像：持久事实文件化（DeepTutor 可审计记忆的 L2 思想）。
//
// 数据：outpost2-data/memory/profile.md——人工可读可编辑；
// 「方向讨论 → 同步到学习」时由 LLM 从对话中提取持久事实合并落盘（tutor.DistillProfile）；
// 每次教学/出题调用热读注入（Block）——背景 / 时间 / 目标说完不再丢，个性化跨会话连续。
package profile

import (
	"os"
	"path/filepath"
	"strings"
)

// FileName 画像文件名（位于数据目录的 memory/ 子目录）。
const FileName = "profile.md"

// Title 画像正文标题（模板与蒸馏输出共用，Block 据此判断正文是否为空）。
const Title = "# 学习者画像"

// Template 首启写入的画像模板：正文为空（只有注释说明），注释不注入 LLM。
const Template = Title + `

<!--
  这份画像由「方向讨论 → 同步到学习」自动沉淀：从你和助教的讨论中提取
  背景 / 水平 / 每周时间 / 目标 / 偏好等持久事实。
  每次教学与出题都会注入给 AI 助教；也可以直接手编，保存即生效。
-->
`

// Ensure 确保 dataDir/memory/profile.md 存在（缺失写模板），返回文件路径；失败返回空串。
func Ensure(dataDir string) string {
	dir := filepath.Join(dataDir, "memory")
	p := filepath.Join(dir, FileName)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(p, []byte(Template), 0o644); err != nil {
		return ""
	}
	return p
}

// Load 读取画像原文（缺失/失败返回空）。
func Load(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Save 保存画像原文。
func Save(path, content string) error {
	if path == "" {
		return os.ErrInvalid
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Block 把画像格式化为 system 注入块；空画像（模板未沉淀）返回空串。
func Block(path string) string {
	body := stripComments(Load(path))
	body = strings.TrimSpace(strings.ReplaceAll(body, Title, ""))
	if body == "" {
		return ""
	}
	return "【学习者画像（从方向讨论自动沉淀，可在设置页编辑：讲解按他的背景打比方、出题贴合他的目标场景）】\n" + body
}

// stripComments 去掉 HTML 注释段（模板里的使用说明不注入 LLM）。
func stripComments(s string) string {
	for {
		i := strings.Index(s, "<!--")
		if i < 0 {
			return s
		}
		rest := s[i:]
		j := strings.Index(rest, "-->")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + rest[j+3:]
	}
}
