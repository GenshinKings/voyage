// curator.go — 内容进货员：助教自主用抓取工具从 GitHub 进货的一次完整会话。
//
// 触发：serve 内置定时（每日 fetch.json.hour 点）+ 设置页「立即抓取」按钮。
// 落点（用户 2026-10-25 决策）：README + 关键源文件 → 教学参考库（kb/），
// 最值得深读的 → 阅读管理器。决策与筛选全部由接入的 LLM 助教在工具循环内完成。
package tutor

import (
	"fmt"

	"voyage/internal/llm"
)

const curateSystem = `你是前哨的内容进货员，每天从 GitHub 为学习者挑选真正值得学的内容入库。

可用工具：gh_trending（今日热门榜）、gh_search（定向搜索仓库）、gh_browse（仓库概览+文件树）、gh_fetch_file（抓单个源文件）、kb_save（存入教学参考库）、add_reading（加入阅读清单）。

工作流程：
1. gh_trending 看今日热门，gh_search 用学习者当前科目定向搜教材（如「机器学习 tutorial language:Python」，找 star 高、有完整教程结构的）
2. 对照学习者背景挑 1-3 个最有学习价值的——硬标准：README 有 Quick Start、有 examples/ 或教程章节，纯工具发布不选
3. 每个入选仓库：gh_browse 看文件树；README 用 gh_fetch_file 抓（路径 README.md，或树里看到的实际命名）；**README 之外再抓 1-2 个最有学习价值的源文件**（示例代码/教程文档/笔记本）——只存 README 经常什么也学不到，examples 和源码才是干货
4. 每份材料单独 kb_save（content 传全文，digest ≤60 字说清「学完能做什么」，url 传仓库地址）；最值得深读的一个仓库用 add_reading 加进阅读清单（why 说清为什么）
5. 全部完成后输出进货报告（纯文本 ≤200 字）：抓了什么、为什么值、存在哪

纪律：宁缺毋滥，一个合适的都没有就报告「今日无货」也别硬凑；二进制/与学习者无关的不入；总消耗控制在 ≤12 次工具调用内。`

// CurateOnce 跑一次进货会话：ctx 是学习者上下文（科目/已收尾/库里已有），tools 由调用方组装。
// 返回进货报告文本（供 fetch.json 落盘与设置页展示）。
func (t *Tutor) CurateOnce(ctx string, tools []Tool) (string, error) {
	if t.Client == nil {
		return "", fmt.Errorf("LLM 未配置")
	}
	if len(tools) == 0 {
		return "", fmt.Errorf("没有可用工具（GitHub 抓取未配置）")
	}
	user := "学习者上下文：\n" + ctx + "\n\n开始今天的进货：用工具完成挑选与入库，最后输出进货报告。"
	report, _, err := t.runAgent(curateSystem, []llm.Message{{Role: "user", Content: user}}, tools)
	return report, err
}
