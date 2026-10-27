// projectbrief.go — 助教的项目导读：深入 GitHub 项目源码后产出结构化消化。
//
// 用户 2026-10-26 指令：阅读材料常只有 README 可看——助教要深入整个项目
// （browse 文件树 → fetch 关键源文件），总结内容并指导怎么用/复现。
// 复用进货的抓取工具（gh_browse/gh_fetch_file），输出落到 Reading.Brief。
package tutor

import (
	"fmt"

	"voyage/internal/llm"
)

const briefSystem = `你是前哨的 AI 助教，任务：深入调研一个 GitHub 项目，为学习者产出一份「项目导读」。
学习者只想知道：这是什么、值得学什么、怎么上手跑起来、怎么复现一个最小版本。

工作流程：
1. gh_browse 看仓库概览与文件树——判断项目结构（文档在哪、示例在哪、核心代码在哪）
2. gh_fetch_file 抓 README 与 2-4 个最有信息量的文件（examples/主入口/核心模块——README 之外才是项目的真话）
3. 输出导读（Markdown，≤500 字），结构固定：
   ## 这是什么
   一句话说清项目解决什么问题、用什么方案（≤2 句）
   ## 值得看的 2-3 处
   具体到文件路径——每处一句话说为什么值得看（结合学习者的在学内容点出衔接）
   ## 怎么上手
   从 README 提炼的最短路径（安装→最小运行），命令原样给出；标注需要什么前置（key/环境）
   ## 复现路径
   如果学习者要自己做一个最小同类版：先做哪一步、核心难点是什么（结合他的水平，≤3 步）

纪律：只说你从源码里看到的，不编造；README 没写的信息如果源码能看出来就标注「（从 xxx 推断）」；抓不到的文件如实说明。`

// ProjectBrief 深入项目源码产出导读。
func (t *Tutor) ProjectBrief(ctx, repo string, tools []Tool) (string, error) {
	if t.Client == nil {
		return "", fmt.Errorf("LLM 未配置")
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("抓取工具未就绪（需要 GitHub 抓取工具组）")
	}
	user := "学习者上下文：\n" + ctx + "\n\n目标仓库：" + repo + "\n\n开始调研并输出导读。"
	report, _, err := t.runAgent(briefSystem, []llm.Message{{Role: "user", Content: user}}, tools)
	return report, err
}
