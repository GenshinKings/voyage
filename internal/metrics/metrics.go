// Package metrics —— 北极星的三信号。
//
// 宪法 1.1 写着目标函数是「学会了，且想继续学」，四个度量里：
//
//	确定性验收 ✅（已有）
//	FSRS 延迟保持率 ❌（排期有、结果没算）——本包补
//	迁移 ⚠️（提示词里有，没口径）——本包先留接口，等接上 run 数据
//	自发回归 / uptake / 自报 ❌ ——本包补「自发回归」
//
// 刻意做得极小：只记事件、只算三件事，不新增页面、不做可视化。
// 理由：先有数，再谈「产品有没有用」。
package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// 三种事件。
const (
	KindRetention = "retention" // 复测到期时答没答上
	KindTransfer  = "transfer"  // 复测过的知识点，在后来的实战里被用上
	KindReturn    = "return"    // 相隔 >24h 又自己回来了
)

// Event 一条事件。
type Event struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Key  string    `json:"key"` // 卡片 id / 知识点 / 会话 id
	OK   bool      `json:"ok"`
	Note string    `json:"note,omitempty"`
}

// Store 存 data/metrics.jsonl。
type Store struct{ Path string }

func New(dataDir string) Store { return Store{Path: filepath.Join(dataDir, "metrics.jsonl")} }

// Record 追加一条事件。
func (s Store) Record(e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}

// Events 读全部事件（坏行跳过，不让一行毁掉统计）。
func (s Store) Events() []Event {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return nil
	}
	var out []Event
	for _, line := range splitLines(raw) {
		var e Event
		if json.Unmarshal(line, &e) == nil && e.Kind != "" {
			out = append(out, e)
		}
	}
	return out
}

// Summary 三信号汇总。
type Summary struct {
	RetentionPass  int     `json:"retentionPass"`
	RetentionTotal int     `json:"retentionTotal"`
	RetentionRate  float64 `json:"retentionRate"`
	TransferHits   int     `json:"transferHits"`
	ReturnHits     int     `json:"returnHits"`
	LastSessionAt  string  `json:"lastSessionAt,omitempty"`
	HasLastSession bool    `json:"hasLastSession"`
}

// Summarize 算出来。days<=0 表示全部历史。
func (s Store) Summarize(days int) Summary {
	evs := s.Events()
	cut := time.Time{}
	if days > 0 {
		cut = time.Now().AddDate(0, 0, -days)
	}
	var sum Summary
	for _, e := range evs {
		if !cut.IsZero() && e.At.Before(cut) {
			continue
		}
		switch e.Kind {
		case KindRetention:
			sum.RetentionTotal++
			if e.OK {
				sum.RetentionPass++
			}
		case KindTransfer:
			if e.OK {
				sum.TransferHits++
			}
		case KindReturn:
			if e.OK {
				sum.ReturnHits++
			}
		}
	}
	if sum.RetentionTotal > 0 {
		sum.RetentionRate = float64(sum.RetentionPass) / float64(sum.RetentionTotal)
	}
	return sum
}

// MarkSession 记一次「他打开了」。相隔超过 24 小时算一次自发回归
// （我们没有任何推送，所以只要有 24 小时空档又回来，就是自发的）。
func (s Store) MarkSession() (bool, error) {
	evs := s.Events()
	var last time.Time
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == KindReturn {
			last = evs[i].At
			break
		}
	}
	if last.IsZero() {
		if err := s.Record(Event{Kind: KindReturn, Key: "first", OK: false, Note: "第一次进来，不算回归"}); err != nil {
			return false, err
		}
		return false, nil
	}
	gap := time.Since(last)
	if gap < 24*time.Hour {
		return false, nil
	}
	if err := s.Record(Event{Kind: KindReturn, Key: "session", OK: true,
		Note: "相隔 " + gap.Round(time.Hour).String() + " 自己回来了"}); err != nil {
		return false, err
	}
	return true, nil
}

func splitLines(raw []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			if i > start {
				out = append(out, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}
