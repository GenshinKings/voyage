package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 三件套硬要求（第七轮需求 §一）：source / ts / confidence 缺一不许入库。
func TestValidateRequiresTriple(t *testing.T) {
	now := time.Now()
	base := Entry{ID: "m-1", Text: "主人每晚做 1.5 小时临时工", Kind: KindFact,
		Source: "session:1026-ythono-01@2026-10-28", Ts: now, Confidence: 0.9}
	if err := Validate(base); err != nil {
		t.Fatalf("合法条目不应报错: %v", err)
	}
	noSource := base
	noSource.Source = " "
	if err := Validate(noSource); err == nil {
		t.Fatal("缺 source 必须拒收")
	}
	noTs := base
	noTs.Ts = time.Time{}
	if err := Validate(noTs); err == nil {
		t.Fatal("缺 ts 必须拒收")
	}
	badConf := base
	badConf.Confidence = 0
	if err := Validate(badConf); err == nil {
		t.Fatal("confidence=0 必须拒收")
	}
	badConf2 := base
	badConf2.Confidence = 1.5
	if err := Validate(badConf2); err == nil {
		t.Fatal("confidence>1 必须拒收")
	}
	badKind := base
	badKind.Kind = "rumor"
	if err := Validate(badKind); err == nil {
		t.Fatal("kind 越白名单必须拒收")
	}
	empty := base
	empty.Text = ""
	if err := Validate(empty); err == nil {
		t.Fatal("空 text 必须拒收")
	}
}

// Add 拒收非法条目——「半条记忆」不允许出现；合法条目自动置 active。
func TestAddRejectsHalfEntries(t *testing.T) {
	s := New()
	now := time.Now()
	if _, err := s.Add(Entry{Text: "缺 source", Kind: KindFact, Ts: now, Confidence: 0.5}, now); err == nil {
		t.Fatal("缺 source 的 Add 必须失败")
	}
	if len(s.Entries) != 0 {
		t.Fatalf("失败条目不得入库: %d", len(s.Entries))
	}
	e, err := s.Add(Entry{Text: "合法", Kind: KindFact, Source: "session:x", Ts: now, Confidence: 0.8}, now)
	if err != nil || e.Status != StatusActive {
		t.Fatalf("合法条目应入库且 active: %+v %v", e, err)
	}
}

// 落盘往返：memory/memories.json 存在、字段齐全。
func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir) // 无文件 → 空库
	if len(s.Entries) != 0 {
		t.Fatal("无文件应得空库")
	}
	now := time.Now()
	_, err := s.Add(Entry{ID: "m-20261028-01", Text: "测试条目", Kind: KindPreference,
		Source: "session:t@2026-10-28", Ts: now, Confidence: 0.7}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "memory", "memories.json")); err != nil {
		t.Fatalf("memories.json 应存在: %v", err)
	}
	s2 := Load(dir)
	if len(s2.Entries) != 1 || s2.Entries[0].ID != "m-20261028-01" || s2.Entries[0].Confidence != 0.7 {
		t.Fatalf("重载应还原条目: %+v", s2.Entries)
	}
	// 文件形态对照需求示例：version+entries 顶层，条目含三件套字段
	b, _ := os.ReadFile(filepath.Join(dir, "memory", "memories.json"))
	var probe struct {
		Version int `json:"version"`
		Entries []struct {
			Source     string  `json:"source"`
			Confidence float64 `json:"confidence"`
			Status     string  `json:"status"`
		} `json:"entries"`
	}
	if json.Unmarshal(b, &probe) != nil || probe.Version != 1 || len(probe.Entries) != 1 ||
		probe.Entries[0].Source == "" || probe.Entries[0].Confidence != 0.7 || probe.Entries[0].Status != "active" {
		t.Fatalf("文件形态不符: %s", b)
	}
}

// New 空库（version=1）。
func New() *Store { return &Store{Version: 1} }
