package tutor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 指标公式（见 docs/teach/metrics.md）：
//
//	平滑独立通过率 = (independent + 1) / (judged + 2)   // 拉普拉斯平滑
//	样本量门槛     = 5                                  // 少于此数不显示比率
//	趋势           = 后窗均值 − 前窗均值（两窗都要过门槛）
//
// 为什么平滑：1/1 和 100/100 的「100%」不是一回事；不平滑会让人误以为稳。
// 为什么设门槛：3 次判定算出来的比率只是噪音，显示它等于误导。
const (
	// MinSample 低于这个判定次数时不显示比率。
	MinSample = 5
	// trendWindow 趋势窗口（天）。
	trendWindow = 3
)

// Bucket 一个聚合桶（按节或按天）。
type Bucket struct {
	Judged      int            `json:"judged"`
	Passed      int            `json:"passed"`
	Independent int            `json:"independent"`
	Causes      map[string]int `json:"causes,omitempty"`
	Downgrades  int            `json:"downgrades,omitempty"`
	Recovered   int            `json:"recovered,omitempty"`
}

func newBucket() *Bucket { return &Bucket{Causes: map[string]int{}} }

// Rate 平滑后的独立通过率。
func (b *Bucket) Rate() float64 {
	if b == nil {
		return 0
	}
	return float64(b.Independent+1) / float64(b.Judged+2)
}

// Enough 样本量是否够显示这个数。
func (b *Bucket) Enough() bool { return b != nil && b.Judged >= MinSample }

// Stats —— 「助教会不会教」的证据来源（docs/teach/spec.md 第八节）。
type Stats struct {
	Judged      int            `json:"judged"`
	Passed      int            `json:"passed"`
	Independent int            `json:"independent"`
	Causes      map[string]int `json:"causes"`
	Downgrades  int            `json:"downgrades"`
	Recovered   int            `json:"recovered"`

	// 按节 / 按天聚合（聚合公式见文件头）
	ByLesson map[string]*Bucket `json:"byLesson,omitempty"`
	ByDay    map[string]*Bucket `json:"byDay,omitempty"`

	pendingRecovery bool
	mu              sync.Mutex
}

// IndependentPassRate 全程平滑独立通过率。
func (s *Stats) IndependentPassRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return (&Bucket{Judged: s.Judged, Independent: s.Independent}).Rate()
}

// RecoveryRate 退档后回升率；没有降档记录时返回 0。
func (s *Stats) RecoveryRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Downgrades == 0 {
		return 0
	}
	return float64(s.Recovered) / float64(s.Downgrades)
}

// bucket 取（或建）一个聚合桶。调用方需持有锁。
func (s *Stats) bucket(by map[string]*Bucket, key string) *Bucket {
	if by == nil || key == "" {
		return nil
	}
	b, ok := by[key]
	if !ok {
		b = newBucket()
		by[key] = b
	}
	return b
}

// notePassFor 记一次判定结果（带节与日期维度）。stuck=学习者点了「我卡住了」。
func (s *Stats) notePassFor(lesson, day string, pass, stuck bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Judged++
	buckets := []*Bucket{s.bucket(s.ByLesson, lesson), s.bucket(s.ByDay, day)}
	if pass {
		s.Passed++
		if !stuck {
			s.Independent++
			if s.pendingRecovery {
				s.Recovered++
				s.pendingRecovery = false
			}
		}
	}
	for _, b := range buckets {
		if b == nil {
			continue
		}
		b.Judged++
		if pass {
			b.Passed++
			if !stuck {
				b.Independent++
			}
		}
	}
}

// noteCauseFor 记一次未通过的错误类型（带节与日期维度）。
func (s *Stats) noteCauseFor(lesson, day, cause string) {
	if cause == "" || cause == "none" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Causes == nil {
		s.Causes = map[string]int{}
	}
	s.Causes[cause]++
	for _, b := range []*Bucket{s.bucket(s.ByLesson, lesson), s.bucket(s.ByDay, day)} {
		if b == nil {
			continue
		}
		if b.Causes == nil {
			b.Causes = map[string]int{}
		}
		b.Causes[cause]++
	}
}

// noteDowngradeFor 记一次降档（带节与日期维度）。
func (s *Stats) noteDowngradeFor(lesson, day string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Downgrades++
	s.pendingRecovery = true
	for _, b := range []*Bucket{s.bucket(s.ByLesson, lesson), s.bucket(s.ByDay, day)} {
		if b == nil {
			continue
		}
		b.Downgrades++
	}
}

// notePass / noteCause / noteDowngrade 保留无维度版本（测试与旧调用点用）。
func (s *Stats) notePass(pass, stuck bool) { s.notePassFor("", "", pass, stuck) }

func (s *Stats) noteCause(cause string) { s.noteCauseFor("", "", cause) }

func (s *Stats) noteDowngrade() { s.noteDowngradeFor("", "") }

// trend 最近 trendWindow 天与之前 trendWindow 天的平滑率差值。
// 两窗样本都不够时返回 ok=false（不显示趋势，别用小样本编故事）。
func (s *Stats) trend(now time.Time) (delta float64, earlier, later int, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stat, prev := newBucket(), newBucket()
	for day, b := range s.ByDay {
		t, err := time.Parse("2006-01-02", day)
		if err != nil || b == nil {
			continue
		}
		diff := int(now.Sub(t).Hours() / 24)
		if diff < 0 {
			continue
		}
		switch {
		case diff < trendWindow:
			*stat = mergeBuckets(stat, b)
		case diff < 2*trendWindow:
			*prev = mergeBuckets(prev, b)
		}
	}
	if !stat.Enough() || !prev.Enough() {
		return 0, 0, 0, false
	}
	return stat.Rate() - prev.Rate(), prev.Judged, stat.Judged, true
}

// mergeBuckets 把 b 合并进 dst（值语义，聚合桶不该被共享引用）。
func mergeBuckets(dst, b *Bucket) Bucket {
	out := *dst
	out.Judged += b.Judged
	out.Passed += b.Passed
	out.Independent += b.Independent
	out.Downgrades += b.Downgrades
	out.Recovered += b.Recovered
	if len(b.Causes) > 0 {
		if out.Causes == nil {
			out.Causes = map[string]int{}
		}
		for k, v := range b.Causes {
			out.Causes[k] += v
		}
	}
	return out
}

// Snapshot 一份可直接给接口/界面的指标快照。
func (s *Stats) Snapshot() map[string]any {
	delta, prevN, curN, hasTrend := s.trend(time.Now())

	s.mu.Lock()
	defer s.mu.Unlock()
	causes := map[string]int{}
	for k, v := range s.Causes {
		causes[k] = v
	}
	byLesson := map[string]any{}
	for k, v := range s.ByLesson {
		byLesson[k] = bucketView(v)
	}
	byDay := map[string]any{}
	days := make([]string, 0, len(s.ByDay))
	for k := range s.ByDay {
		days = append(days, k)
	}
	sort.Strings(days)
	for _, k := range days {
		byDay[k] = bucketView(s.ByDay[k])
	}

	out := map[string]any{
		"judged":              s.Judged,
		"passed":              s.Passed,
		"independent":         s.Independent,
		"independentPassRate": (&Bucket{Judged: s.Judged, Independent: s.Independent}).Rate(),
		"causes":              causes,
		"downgrades":          s.Downgrades,
		"recovered":           s.Recovered,
		"minSample":           MinSample,
		"sampleEnough":        s.Judged >= MinSample,
		"byLesson":            byLesson,
		"byDay":               byDay,
	}
	if s.Downgrades > 0 {
		out["recoveryRate"] = float64(s.Recovered) / float64(s.Downgrades)
	}
	if hasTrend {
		out["trend"] = map[string]any{"delta": delta, "prevJudged": prevN, "curJudged": curN}
	}
	return out
}

func bucketView(b *Bucket) map[string]any {
	if b == nil {
		return map[string]any{}
	}
	v := map[string]any{
		"judged":      b.Judged,
		"passed":      b.Passed,
		"independent": b.Independent,
		"rate":        b.Rate(),
		"enough":      b.Enough(),
	}
	if len(b.Causes) > 0 {
		v["causes"] = b.Causes
	}
	if b.Downgrades > 0 {
		v["downgrades"] = b.Downgrades
		v["recovered"] = b.Recovered
	}
	return v
}

// stats 懒加载统计（Tutor.Dir 下的 stats.json）。
func (t *Tutor) stats() *Stats {
	if t.Stats == nil {
		t.Stats = t.LoadStats()
	}
	return t.Stats
}

// recordVerdict 把一轮判定的结果记入指标（含节与日期维度）。
//
// kind: 判定得到的结论（pass/partial/fail/空）
// stuck: 学习者点了「我卡住了」（算「有提示」，不计入独立通过）
// cause: 未通过时的错误类型
func (t *Tutor) recordVerdict(kind string, stuck bool, cause, channel, lessonID string) {
	if kind == "" && cause == "" {
		return // 收尾后的陪聊轮，不算教学判定
	}
	day := t.now().Format("2006-01-02")
	st := t.stats()
	switch kind {
	case "pass":
		st.notePassFor(lessonID, day, true, stuck)
	case "partial":
		st.notePassFor(lessonID, day, false, stuck)
		st.noteCauseFor(lessonID, day, "unfinished")
	case "fail":
		st.notePassFor(lessonID, day, false, stuck)
		st.noteCauseFor(lessonID, day, cause)
	default:
		st.notePassFor(lessonID, day, false, stuck)
		st.noteCauseFor(lessonID, day, cause)
	}
	t.SaveStats(st)
}

// statsPath 统计文件落在 Tutor 的数据目录里。
func (t *Tutor) statsPath() string {
	return filepath.Join(t.Dir, "stats.json")
}

// LoadStats 读统计（缺文件=空统计，不阻塞）。
func (t *Tutor) LoadStats() *Stats {
	st := &Stats{Causes: map[string]int{}, ByLesson: map[string]*Bucket{}, ByDay: map[string]*Bucket{}}
	if t.Dir == "" {
		return st
	}
	b, err := os.ReadFile(t.statsPath())
	if err != nil {
		return st
	}
	if err := json.Unmarshal(b, st); err != nil {
		return &Stats{Causes: map[string]int{}, ByLesson: map[string]*Bucket{}, ByDay: map[string]*Bucket{}}
	}
	if st.Causes == nil {
		st.Causes = map[string]int{}
	}
	if st.ByLesson == nil {
		st.ByLesson = map[string]*Bucket{}
	}
	if st.ByDay == nil {
		st.ByDay = map[string]*Bucket{}
	}
	for _, m := range []map[string]*Bucket{st.ByLesson, st.ByDay} {
		for _, b := range m {
			if b != nil && b.Causes == nil {
				b.Causes = map[string]int{}
			}
		}
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
