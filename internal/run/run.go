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
	// Uncertain 关卡 ID -> 学员自述「我哪里还不确定」。这是整套系统的灵魂：
	// 没通过可以再试，不知道自己不确定什么才是真问题。
	Uncertain map[string]string `json:"uncertain"`
	// Reconcile 关卡 ID -> 事后对账结果（was-problem / no-problem）。
	// 用来量「判断的准确度」——说会出问题的地方，后来真出问题了吗。
	Reconcile map[string]string `json:"reconcile"`
	// Retro 关卡 ID -> 复盘（卡点类型 + 一句「下次怎么办」）。
	// 只记结果不记复盘，错误谱系就永远是空的。
	Retro    map[string]Retro `json:"retro"`
	Started  string           `json:"started"`
	Finished bool             `json:"finished"`
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

// SubmitJudged 交给外部判定（模型或人）之后再提交。
// 外部判不过就直接拦下，不进关卡；判过则跳过 Expect 校验直接解锁——
// 因为需要外部判定的关卡本来就是「机器看不出对错」的那类。
func (r *Run) SubmitJudged(output string, pass bool, reason string) (ok bool, why string) {
	st := r.CurrentStage()
	if st == nil {
		return false, "已经全部过关了"
	}
	if strings.TrimSpace(output) == "" {
		return false, "还没贴东西——把这一步跑出来的结果发我"
	}
	if !pass {
		if reason == "" {
			reason = "这一步还没达到验收标准"
		}
		return false, reason
	}
	r.Results[st.ID] = strings.TrimSpace(output)
	r.Current++
	if r.Current >= len(r.Stages) {
		r.Finished = true
	}
	return true, ""
}

// NeedsExternalJudge 这一关是否需要模型/人工判定（Expect 为空即需要）。
func (r *Run) NeedsExternalJudge() bool {
	st := r.CurrentStage()
	return st != nil && len(st.Expect) == 0
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

// NoteUncertain 记下「我哪里还不确定」（同一关重复提交时覆盖，保留最新一次）。
func (r *Run) NoteUncertain(id, text string) {
	if r.Uncertain == nil {
		r.Uncertain = map[string]string{}
	}
	if text = trim(text); text != "" {
		r.Uncertain[id] = text
	}
}

// ReconcileStage 事后对账：这次「不确定」后来真成问题了吗（was-problem / no-problem）。
func (r *Run) ReconcileStage(id, outcome string) bool {
	if outcome != "was-problem" && outcome != "no-problem" {
		return false
	}
	if r.Reconcile == nil {
		r.Reconcile = map[string]string{}
	}
	r.Reconcile[id] = outcome
	return true
}

// JudgementHit 判断命中率：不确定的地方真出问题的比例（没有对账记录时返回 -1）。
func (r *Run) JudgementHit() float64 {
	if len(r.Reconcile) == 0 {
		return -1
	}
	hit := 0
	for _, v := range r.Reconcile {
		if v == "was-problem" {
			hit++
		}
	}
	return float64(hit) / float64(len(r.Reconcile))
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// Retro 一次复盘。
type Retro struct {
	Kind string `json:"kind"` // concept / condition / env / slip / unfinished
	Note string `json:"note"` // 一句话：下次怎么办
}

// RetroKinds 允许的卡点类型（跟教学规范一致）。
var RetroKinds = []string{"concept", "condition", "env", "slip", "unfinished"}

// ValidRetroKind 校验卡点类型。
func ValidRetroKind(k string) bool {
	for _, v := range RetroKinds {
		if v == k {
			return true
		}
	}
	return false
}

// NoteRetro 记一次复盘：卡点类型必须在允许的集合里，note 不能空。
func (r *Run) NoteRetro(id, kind, note string) bool {
	kind = trim(kind)
	note = trim(note)
	if id == "" || !ValidRetroKind(kind) || note == "" {
		return false
	}
	if r.Retro == nil {
		r.Retro = map[string]Retro{}
	}
	r.Retro[id] = Retro{Kind: kind, Note: note}
	return true
}

// RetroCount 按卡点类型统计次数（给「按错误出题」用）。
func (r *Run) RetroCount() map[string]int {
	out := map[string]int{}
	for _, v := range r.Retro {
		out[v.Kind]++
	}
	return out
}

// DominantKind 出现最多的卡点类型（没有记录返回空串）。
func (r *Run) DominantKind() string {
	best, bestN := "", 0
	for k, n := range r.RetroCount() {
		if n > bestN {
			best, bestN = k, n
		}
	}
	return best
}
