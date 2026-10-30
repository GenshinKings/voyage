package tutor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStatsCounters(t *testing.T) {
	st := &Stats{Causes: map[string]int{}}

	st.notePass(true, false) // 独立通过
	st.notePass(true, true)  // 点了卡住后通过（算通过，不算独立）
	st.notePass(false, false)
	st.noteCause("concept")
	st.noteDowngrade()
	st.notePass(true, false) // 退档后独立通过 → 回升

	if st.Judged != 4 || st.Passed != 3 || st.Independent != 2 {
		t.Fatalf("计数不对：%+v", st.Snapshot())
	}
	if got := st.IndependentPassRate(); got < 0.49 || got > 0.51 {
		t.Fatalf("独立通过率应为 0.5，实际 %v", got)
	}
	if st.Downgrades != 1 || st.Recovered != 1 {
		t.Fatalf("回升统计不对：%+v", st.Snapshot())
	}
	if got := st.RecoveryRate(); got != 1 {
		t.Fatalf("退档后回升率应为 1，实际 %v", got)
	}
	if st.Causes["concept"] != 1 {
		t.Fatalf("错误类型分布不对：%v", st.Causes)
	}
}

func TestStatsPersist(t *testing.T) {
	dir := t.TempDir()
	tu := &Tutor{Dir: dir}

	st := tu.stats()
	if st == nil || st.Judged != 0 {
		t.Fatal("新 Tutor 应拿到空统计")
	}
	st.notePass(true, false)
	st.noteCause("slip")
	tu.SaveStats(st)

	// 重新加载
	tu2 := &Tutor{Dir: dir}
	got := tu2.LoadStats()
	if got.Judged != 1 || got.Causes["slip"] != 1 {
		b, _ := json.Marshal(got.Snapshot())
		t.Fatalf("落盘/读回不一致：%s", b)
	}

	// 坏文件不炸，退化成空统计
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), []byte("{坏"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := (&Tutor{Dir: dir}).LoadStats()
	if bad.Judged != 0 || bad.Causes == nil {
		t.Fatalf("坏文件应退化成空统计，实际 %+v", bad.Snapshot())
	}
}

func TestRecordVerdict(t *testing.T) {
	tu := &Tutor{Dir: t.TempDir()}

	tu.recordVerdict("pass", false, "", "structured")
	tu.recordVerdict("fail", false, "concept", "structured")
	tu.recordVerdict("", false, "", "marker") // 陪聊轮：不该计入

	snap := tu.stats().Snapshot()
	if snap["judged"] != 2 {
		t.Fatalf("陪聊轮不该计数，实际 %v", snap["judged"])
	}
	causes, _ := snap["causes"].(map[string]int)
	if causes["concept"] != 1 {
		t.Fatalf("错误类型应记 1 次 concept，实际 %v", causes)
	}
}
