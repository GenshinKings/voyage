// Package run —— 「从零带人做出一个开源项目」的最小骨架。
//
// 设计（主人 2026-10-30 定的方向）：
//
//	重点不是改别人的仓库，而是从零做出「一个又一个」有用的开源项目。
//	所以这里有两条轴：
//	  纵向：四档梯度（T1 单文件工具 → T2 带存储 → T3 接外部 API → T4 有人用）
//	  横向：每一档都走同一串关卡（立骨架 → 最小闭环 → 第一条测试 → README → 发 v0.1 → 三天后还用吗）
//
// 教学原则：一次只给当前关卡的「目标 + 验收 + 卡住时的下一步」，
// 过不了不解锁下一关——这是它和「看视频学」的根本区别。
package run

import (
	"fmt"
	"strings"
	"time"
)

// Tier 项目难度档。
type Tier int

const (
	TierTool  Tier = 1 // 单文件小工具：只解决自己一个具体麻烦
	TierStore Tier = 2 // 带存储：数据留得下来
	TierAPI   Tier = 3 // 接外部接口：和真实世界交互
	TierUsers Tier = 4 // 有人真的在用：有外部反馈
)

// TierName 给界面用的中文档名。
func TierName(t Tier) string {
	switch t {
	case TierTool:
		return "T1 单文件小工具"
	case TierStore:
		return "T2 带存储"
	case TierAPI:
		return "T3 接外部接口"
	case TierUsers:
		return "T4 有人真的在用"
	}
	return "未知档"
}

// Stage 一关：目标 + 验收 + 卡住时的下一步。
type Stage struct {
	ID     string   `json:"id"`
	Goal   string   `json:"goal"`             // 一句话目标（可观察）
	Check  string   `json:"check"`            // 一条能跑的验收（命令/操作 + 期望）
	Hint   string   `json:"hint"`             // 卡住时只给下一步，不给答案
	Expect []string `json:"expect,omitempty"` // 验收时必须出现的片段（命中任意一个即算过；空=需人工/模型判）
}

// Project 一个可从零开始的项目模板。
type Project struct {
	ID     string  `json:"id"`
	Tier   Tier    `json:"tier"`
	Title  string  `json:"title"`
	Why    string  `json:"why"` // 解决谁的什么麻烦（判断「有用」的依据）
	Stages []Stage `json:"stages"`
}

// Run 一次实际闯关。
type Run struct {
	ProjectID string            `json:"projectId"`
	Title     string            `json:"title"`
	Tier      Tier              `json:"tier"`
	Stages    []Stage           `json:"stages"`
	Current   int               `json:"current"` // 当前关卡下标
	Results   map[string]string `json:"results"` // 关卡 ID -> 学员贴的输出
	Started   string            `json:"started"`
	Finished  bool              `json:"finished"`
}

// Start 开一个新 Run。
func Start(p Project) (*Run, error) {
	if len(p.Stages) == 0 {
		return nil, fmt.Errorf("这个项目还没有关卡，先用模板生成")
	}
	return &Run{
		ProjectID: p.ID,
		Title:     p.Title,
		Tier:      p.Tier,
		Stages:    p.Stages,
		Results:   map[string]string{},
		Started:   time.Now().Format("2006-01-02"),
	}, nil
}

// CurrentStage 当前该做的关；全部做完返回 nil。
func (r *Run) CurrentStage() *Stage {
	if r == nil || r.Finished || r.Current < 0 || r.Current >= len(r.Stages) {
		return nil
	}
	return &r.Stages[r.Current]
}

// Progress 完成度，例如 "3/6"。
func (r *Run) Progress() string {
	if r == nil {
		return "0/0"
	}
	done := r.Current
	if r.Finished {
		done = len(r.Stages)
	}
	return fmt.Sprintf("%d/%d", done, len(r.Stages))
}

// Submit 提交当前关卡的产物。ok=false 时给出原因，且不解锁下一关。
func (r *Run) Submit(output string) (ok bool, reason string) {
	st := r.CurrentStage()
	if st == nil {
		return false, "已经全部过关了"
	}
	out := strings.TrimSpace(output)
	if out == "" {
		return false, "还没贴东西——把这一步跑出来的结果发我"
	}
	// Expect 为空表示「这条得靠模型或人判」，这里只做非空校验；
	// 非空时只要命中任意一个就算过（不同语言的测试输出长得不一样）。
	if len(st.Expect) > 0 {
		hit := false
		for _, want := range st.Expect {
			if strings.Contains(out, want) {
				hit = true
				break
			}
		}
		if !hit {
			return false, fmt.Sprintf("还差一点：输出里没看到这些里的任何一个 %v。%s", st.Expect, st.Hint)
		}
	}
	r.Results[st.ID] = out
	r.Current++
	if r.Current >= len(r.Stages) {
		r.Finished = true
	}
	return true, ""
}

// Deliverable 全部过关后的「交付证明」——直接能贴进简历/README。
func (r *Run) Deliverable() string {
	if r == nil || !r.Finished {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s（%s）\n", r.Title, TierName(r.Tier))
	fmt.Fprintf(&b, "· 从零搭骨架到发布 v0.1，共 %d 步，每步都有可跑的验收\n", len(r.Stages))
	fmt.Fprintf(&b, "· 起点：%s\n", r.Stages[0].Goal)
	fmt.Fprintf(&b, "· 终点：%s\n", r.Stages[len(r.Stages)-1].Goal)
	b.WriteString("· 证据：仓库链接 + 运行截图 + 这一段描述")
	return b.String()
}
