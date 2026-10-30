package tutor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Stats —— 「助教会不会教」的唯一证据来源（docs/teach/spec.md 第八节）。
//
// 三个指标：
//   - 独立通过率：没点「我卡住了」直接通过的占比
//   - 错误类型分布：concept / condition / env / slip / unfinished 各出现多少
//   - 退档后回升率：被降档之后，下一轮是否独立通过
//
// 没有这三个数，任何「助教变好了」的说法都是空口。
type Stats struct {
	Judged      int            `json:"judged"`      // 走过判定的轮次
	Passed      int            `json:"passed"`      // 判定通过
	Independent int            `json:"independent"` // 无提示（未点卡住/未降档）直接通过
	Causes      map[string]int `json:"causes"`      // 错误类型分布
	Downgrades  int            `json:"downgrades"`  // 降档次数
	Recovered   int            `json:"recovered"`   // 降档后下一轮独立通过

	pendingRecovery bool
	mu              sync.Mutex
}

// IndependentPassRate 独立通过率（0~1）；没数据时返回 0。
func (s *Stats) IndependentPassRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Judged == 0 {
		return 0
	}
	return float64(s.Independent) / float64(s.Judged)
}

// RecoveryRate 退档后回升率（0~1）；没有降档记录时返回 0。
func (s *Stats) RecoveryRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Downgrades == 0 {
		return 0
	}
	return float64(s.Recovered) / float64(s.Downgrades)
}

// Snapshot 一份可直接给接口/界面的指标快照。
func (s *Stats) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	causes := map[string]int{}
	for k, v := range s.Causes {
		causes[k] = v
	}
	rate, rec := 0.0, 0.0
	if s.Judged > 0 {
		rate = float64(s.Independent) / float64(s.Judged)
	}
	if s.Downgrades > 0 {
		rec = float64(s.Recovered) / float64(s.Downgrades)
	}
	return map[string]any{
		"judged":              s.Judged,
		"passed":              s.Passed,
		"independent":         s.Independent,
		"independentPassRate": rate,
		"causes":              causes,
		"downgrades":          s.Downgrades,
		"recovered":           s.Recovered,
		"recoveryRate":        rec,
	}
}

// notePass 记一次判定结果。stuck=学习者点了「我卡住了」。
func (s *Stats) notePass(pass, stuck bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Judged++
	if !pass {
		return
	}
	s.Passed++
	if stuck {
		return
	}
	s.Independent++
	if s.pendingRecovery {
		s.Recovered++
		s.pendingRecovery = false
	}
}

// noteCause 记一次未通过的错误类型。
func (s *Stats) noteCause(cause string) {
	if cause == "" || cause == "none" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Causes == nil {
		s.Causes = map[string]int{}
	}
	s.Causes[cause]++
}

// noteDowngrade 记一次降档：下一轮若独立通过算「回升」。
func (s *Stats) noteDowngrade() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Downgrades++
	s.pendingRecovery = true
}

// stats 懒加载统计（Tutor.Dir 下的 stats.json）。
func (t *Tutor) stats() *Stats {
	if t.Stats == nil {
		t.Stats = t.LoadStats()
	}
	return t.Stats
}

// recordVerdict 把一轮判定的结果记入指标。
//
// kind: marker/结构化通道得到的判定（pass/partial/fail/空）
// stuck: 学习者点了「我卡住了」（算「有提示」，不计入独立通过）
// cause: 未通过时的错误类型
func (t *Tutor) recordVerdict(kind string, stuck bool, cause, channel string) {
	if kind == "" && cause == "" {
		return // 收尾后的陪聊轮，不算教学判定
	}
	st := t.stats()
	switch kind {
	case "pass":
		st.notePass(true, stuck)
	case "partial":
		// 半过=过了但有洞：计入通过，但不算「独立干净通过」
		st.notePass(false, stuck)
		st.noteCause("unfinished")
	case "fail":
		st.notePass(false, stuck)
		if cause != "" {
			st.noteCause(cause)
		}
	default:
		// 空 kind + 有 cause：结构判定失败但没标记
		st.notePass(false, stuck)
		st.noteCause(cause)
	}
	t.SaveStats(st)
}

// statsPath 统计文件落在 Tutor 的数据目录里。
func (t *Tutor) statsPath() string {
	return filepath.Join(t.Dir, "stats.json")
}

// LoadStats 读统计（缺文件=空统计，不阻塞）。
func (t *Tutor) LoadStats() *Stats {
	st := &Stats{Causes: map[string]int{}}
	if t.Dir == "" {
		return st
	}
	b, err := os.ReadFile(t.statsPath())
	if err != nil {
		return st
	}
	if err := json.Unmarshal(b, st); err != nil {
		return &Stats{Causes: map[string]int{}}
	}
	if st.Causes == nil {
		st.Causes = map[string]int{}
	}
	return st
}

// SaveStats 落盘（先写临时文件再改名，避免写一半被打断）。
func (t *Tutor) SaveStats(st *Stats) {
	if t.Dir == "" || st == nil {
		return
	}
	st.mu.Lock()
	b, err := json.MarshalIndent(st, "", "  ")
	st.mu.Unlock()
	if err != nil {
		return
	}
	_ = os.MkdirAll(t.Dir, 0o755)
	final := t.statsPath()
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, final)
}
