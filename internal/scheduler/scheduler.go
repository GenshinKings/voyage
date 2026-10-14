// Package scheduler — 前哨 2.0 调度引擎 v1（宪法 §3.1）。
//
// 职责：回答「今天丢什么」。输入产出物（deadline+进度）、FSRS 到期卡、精力自报、
// 加餐兴趣池；输出今日计划：推进任务切片（≤25 分钟/片）、侧链复习（容量封顶）、
// 可选加餐（交错域、永不强制），并生成人话调度理由（透明度——对抗黑箱不信任）。
//
// 设计纪律（宪法）：
//   - 规则式 v1，确定性输出，可测试可复盘；不搞 ML（学会了吗的证据回来之前不配ML）
//   - 精力自报 Low：推进任务清零，只留复习+加餐——「今天够了」是被宪法保护的合法答案
//   - 加餐永远可选、与主任务不同域（交错原则）；永不以损失厌恶驱动
//   - 最优间隔听 Cepeda 规律（复习容量按时间预算封顶），不按个人勇气个体化
package scheduler

import (
	"fmt"
	"sort"
	"time"
)

type Deliverable struct {
	ID       string
	Title    string
	Deadline time.Time
	Progress float64 // 0-1，验收清单完成比例
	Actions  []string // 下一步最小动作池（每个≤25 分钟；由产出物的验收清单分解而来）
}

type ReviewCard struct {
	ID    string
	Due   time.Time
	Title string
}

type Energy int

const (
	EnergyLow Energy = iota
	EnergyNormal
	EnergyHigh
)

type Extra struct {
	ID     string
	Title  string
	Domain string // 域标签，用于与主任务交错
}

type TaskSlice struct {
	DeliverableID string
	Title         string
	Minutes       int
}

type TodayPlan struct {
	Date      time.Time
	Main      []TaskSlice // 推进任务 0-3 片
	Reviews   []ReviewCard
	Extras    []Extra // 可选加餐 0-2 个
	Note      string  // 调度理由（人话，可解释）
}

const (
	sliceCap      = 25 // 单片分钟上限（注意力切片，47 秒时代的反例如下：切片是会话颗粒不是注意力颗粒）
	reviewMinutes = 10 // 侧链复习时间预算
	reviewPerCard = 75 * time.Second / time.Minute
)

// Plan 生成今日计划。now 传入当前时间（测试注入）。
func Plan(now time.Time, dels []Deliverable, due []ReviewCard, energy Energy, extras []Extra) TodayPlan {
	plan := TodayPlan{Date: now, Note: ""}
	notes := []string{}

	// 1. 选主产出物：未完成且 deadline 最近；并列时进度低者优先（落后者先救）
	var main *Deliverable
	for i := range dels {
		d := &dels[i]
		if d.Progress >= 1 || len(d.Actions) == 0 {
			continue
		}
		if main == nil ||
			d.Deadline.Before(main.Deadline) ||
			(d.Deadline.Equal(main.Deadline) && d.Progress < main.Progress) {
			main = d
		}
	}

	// 2. 推进任务切片（精力配额）
	sliceQuota := map[Energy]int{EnergyLow: 0, EnergyNormal: 2, EnergyHigh: 3}[energy]
	if main != nil && sliceQuota > 0 {
		for i := 0; i < len(main.Actions) && len(plan.Main) < sliceQuota; i++ {
			plan.Main = append(plan.Main, TaskSlice{
				DeliverableID: main.ID,
				Title:         main.Actions[i],
				Minutes:       sliceCap,
			})
		}
		notes = append(notes, fmt.Sprintf("主推进：%s（截止 %s，进度 %.0f%%）",
			main.Title, main.Deadline.Format("01-02"), main.Progress*100))
	}
	if energy == EnergyLow {
		notes = append(notes, "精力自报低——今天不推进，只保养。这是合法答案。")
	}

	// 3. 侧链复习：due 升序装填，容量按时间预算封顶；Low 精力减半
	sort.Slice(due, func(i, j int) bool { return due[i].Due.Before(due[j].Due) })
	cap := reviewMinutes
	if energy == EnergyLow {
		cap = reviewMinutes / 2
	}
	used := 0
	for _, c := range due {
		if used+1 > cap { // 每张卡约 1 分钟预算粒度
			break
		}
		plan.Reviews = append(plan.Reviews, c)
		used++
	}
	if len(plan.Reviews) > 0 {
		notes = append(notes, fmt.Sprintf("侧链复习 %d 张（约 %d 分钟）", len(plan.Reviews), cap))
	} else {
		notes = append(notes, "今日无到期复习")
	}

	// 4. 可选加餐：与主产出物不同域优先（交错原则），最多 2 个，永不强制
	picked := 0
	for _, e := range extras {
		if picked >= 2 {
			break
		}
		if main != nil && e.Domain == main.ID {
			continue // 加餐与主任务异域（同域留给主任务的后续切片）
		}
		plan.Extras = append(plan.Extras, e)
		picked++
	}
	if len(plan.Extras) > 0 {
		notes = append(notes, fmt.Sprintf("可选加餐 %d 项（不做也完全合法）", len(plan.Extras)))
	}

	plan.Note = joinNotes(notes)
	return plan
}

func joinNotes(ns []string) string {
	out := ""
	for i, n := range ns {
		if i > 0 {
			out += "；"
		}
		out += n
	}
	return out
}
