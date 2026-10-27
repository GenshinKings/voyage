// reflect.go — AI 助教的每日教学反思：从今天的教学过程中发现项目缺漏。
//
// 用户 2026-10-25 提议：助教每天教学时一定会看到项目的缺漏——让它在每日例行
// （进货→笔记巡检）之后做一次反思，输出结构化的改进观察，落 backlog.json；
// 低峰时段由 ZCode（工程侧）接单执行——产品不自改自身代码（安全红线），
// 每条观察都要经过「实现+测试+构建+提交」的完整工程流程才落地。
package tutor

import (
	"encoding/json"
	"fmt"
	"strings"

	"voyage/internal/llm"
)

// Observation 一条改进观察（backlog 的原子：助教提 → 低峰自迭代做 → 报告回流）。
type Observation struct {
	ID          string `json:"id"`               // obs-日期-序号
	Observation string `json:"observation"`      // 教学中看到了什么（现象）
	Gap         string `json:"gap"`              // 项目缺什么/哪里不顺
	Suggestion  string `json:"suggestion"`       // 建议怎么改
	Priority    string `json:"priority"`         // high|mid|low
	At          string `json:"at,omitempty"`     // 提出日期
	Status      string `json:"status,omitempty"` // open|done|failed
	Report      string `json:"report,omitempty"` // 迭代报告（done/failed 时回填）
}

const reflectSystem = `你是前哨的 AI 助教，今天例行工作的最后一步：反思今天的教学过程，找出项目本身的缺漏。
输入是「今天的教学生涯数据」。找出：哪些概念讲不顺（缺好例子/缺铺垫）、学习者在哪里反复卡壳（出题/讲解没覆盖到）、工具或材料哪里不好用、流程哪里断了。
只输出你从数据里能支撑的观察，宁缺毋滥（0 条也正常）；每条与已有清单重复的不提。

输出严格 JSON（无围栏）：
{"observations":[{"observation":"教学中的现象（≤50字）","gap":"项目缺什么（≤50字）","suggestion":"建议怎么改（≤60字）","priority":"high|mid|low"}]}
最多 3 条，按重要性排序。`

// Reflect 每日教学反思：today 数据 → 结构化观察清单（已有清单用于去重）。
func (t *Tutor) Reflect(today, existing string) ([]Observation, error) {
	if t.Client == nil {
		return nil, fmt.Errorf("LLM 未配置")
	}
	user := "已有观察清单（不要重复提）：\n" + existing + "\n\n今天的教学生涯数据：\n" + today + "\n\n开始反思。"
	raw, err := t.Client.ChatJSON(t.withAgents(reflectSystem), []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("反思 JSON 解析失败: %w", err)
	}
	valid := out.Observations[:0]
	for _, o := range out.Observations {
		o.Observation = strings.TrimSpace(o.Observation)
		if o.Observation == "" {
			continue
		}
		switch o.Priority {
		case "high", "mid", "low":
		default:
			o.Priority = "mid"
		}
		valid = append(valid, o)
	}
	return valid, nil
}

// iterateSystem 自迭代工作台的开发助教纪律（SelfIterate 用）。
const iterateSystem = `你是前哨项目的开发助教，正在低峰时段自主迭代项目：按一条改进观察完成一次「小步实现」。

工作流程（严格小步）：
1. 先读相关源码（read_file / list_dir），理解现状——改动永远最小化，只实现观察里建议的那一件事
2. 用 write_file 落地修改（含必要的测试修改；新功能尽量带测试）
3. 用 run_tests 跑全量测试，红了就修到绿；用 go_build 确认可编译
4. 输出实现报告（纯文本 ≤200 字）：改了什么文件、为什么、测试结果

红线（工具层也会强制）：
- 只改 internal/ 下的 Go 源码与静态资源——不碰数据目录、cmd/、go.mod、仓库其他文件
- 不引入第三方依赖；不删测试；不过度设计（一次只做一件事）
- 改动若需要大重构（>100 行），放弃并说明「需要人工评估」`

// SelfIterate 低峰自迭代：一条观察 → 开发助教带工具自主实现 → 实现报告。
// 工具由 server 组装（白名单读写/测试/构建/看 diff）；确定性闸门在 server 侧（测试+构建不过即还原）。
func (t *Tutor) SelfIterate(obs Observation, tools []Tool) (string, []llm.Message, error) {
	if t.Client == nil {
		return "", nil, fmt.Errorf("LLM 未配置")
	}
	if len(tools) == 0 {
		return "", nil, fmt.Errorf("没有可用工具（迭代工具未就绪）")
	}
	user := "改进观察：\n" +
		"- 现象：" + obs.Observation + "\n" +
		"- 缺口：" + obs.Gap + "\n" +
		"- 建议：" + obs.Suggestion + "\n\n" +
		"按工作流程实现这一条改进，最后输出实现报告。"
	return t.runAgent(iterateSystem, []llm.Message{{Role: "user", Content: user}}, tools)
}
