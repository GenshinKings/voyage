package project

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// 「副本该由助教按目标 + 当前状态定，不是摆一排让人挑」（主人 2026-11-03 23:57）。
//
// 这里只做决策：下一步该练什么，一句话，并说明为什么现在是这个。
// 出题由 Generate 负责，本文件不管题目细节。

// State 助教判断时能看到的「他现在什么样」。
type State struct {
	Goal       string   `json:"goal"`       // 学习目标（学科/方向）
	CurrentLsn string   `json:"currentLsn"` // 当前课讲到哪
	Checklist  []string `json:"checklist"`  // 当前课的必须掌握清单
	Weak       []string `json:"weak"`       // 卡过的点
	Passed     []string `json:"passed"`     // 已经过关的副本题（避免重复）
	Failed     []string `json:"failed"`     // 没过关的副本题
}

// Plan 助教的安排。
type Plan struct {
	Brief string `json:"brief"` // 下一步练什么（一句话，给 Generate 当输入）
	Why   string `json:"why"`   // 为什么现在练这个（对学生可见）
}

const plannerSystem = `你是学习教练。根据学习者的情况，决定他**下一步该做哪个小副本**。

规则：
1. 只练他已经学过的东西——用「当前课清单」和「卡过的点」，不要引入没学过的内容。
2. 优先补「卡过的点」和「清单里还没验证过的条目」。
3. 不要重复「已经过关的副本题」。
4. 项目要小：30~90 分钟能做完，2~4 个文件。
5. brief 写给学习者看，一句话，用他要做的事描述（例：「做一个能暂停的番茄钟，
   重点验证暂停后时间不会跳」），不要出现他没学过的术语。
6. why 一句话说明为什么现在练这个（例：「你上次在暂停后时间会跳这里卡过」）。

只输出 JSON：{"brief":"…","why":"…"}`

// PlanNext 让模型决定下一步练什么。
func PlanNext(ctx context.Context, chat ChatFunc, st State) (Plan, error) {
	b, err := json.Marshal(st)
	if err != nil {
		return Plan{}, err
	}
	out, err := chat(ctx, plannerSystem, string(b))
	if err != nil {
		return Plan{}, err
	}
	return ParsePlan(out)
}

// ParsePlan 抠 JSON 并校验。
func ParsePlan(raw string) (Plan, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Plan{}, fmt.Errorf("教练没给出可解析的安排")
	}
	var p Plan
	if err := json.Unmarshal([]byte(raw[start:end+1]), &p); err != nil {
		return Plan{}, fmt.Errorf("安排 JSON 坏了：%w", err)
	}
	if strings.TrimSpace(p.Brief) == "" {
		return Plan{}, fmt.Errorf("安排里没说练什么")
	}
	if strings.TrimSpace(p.Why) == "" {
		p.Why = "按你现在的进度安排的"
	}
	return p, nil
}
