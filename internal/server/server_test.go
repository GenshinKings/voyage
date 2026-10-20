package server

// server_test.go — 应用壳测试：种子装配 → 计划 → 完成 → 自报 → 履历联动 → 证据页。
import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"outpost2/internal/evidence"
)

// 履历跨重启不丢：ledger.json 是单一真相源，state.json 怎么变都吞不掉履历（事故 2026-10-20）。
func TestLedgerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	// 第一次启动：Record 一条 + 只写 ledger.json（模拟真实 Record 路径）
	s1 := NewServer(dir)
	s1.Ledger.Record("L-1", "线性回归与梯度下降", evidence.Known, time.Now(), "tutor:机器学习", "")
	s1.save("ledger.json", s1.Ledger)
	// 重启加载（state.json 里没有 ledger 副本——v3.3 之前的事故形态）
	s2 := NewServer(dir)
	if got := s2.Ledger.List(); len(got) != 1 || got[0].ID != "L-1" {
		t.Fatalf("重启后履历丢失: %+v", got)
	}
	// ledger.json 缺失时优雅降级为空账本而不是启动失败
	os.Remove(filepath.Join(dir, "ledger.json"))
	s3 := NewServer(dir)
	if s3.Ledger == nil || len(s3.Ledger.List()) != 0 {
		t.Fatal("缺失 ledger.json 应降级为空账本")
	}
}

func TestServerDailyLoop(t *testing.T) {
	s := NewServer(t.TempDir())
	// 种子：三产出物
	if len(s.Goals) != 3 {
		t.Fatalf("种子产出物应为 3: %d", len(s.Goals))
	}
	h := s.Handlers()
	// 首页可渲染
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "今天丢来的") {
		t.Fatalf("首页渲染失败: %d", r.Code)
	}
	// 刷新计划：主推进切片来自 P1 验收清单
	plan := s.RefreshPlan()
	if len(plan.Main) == 0 {
		t.Fatal("计划应有主推进切片")
	}
	if !strings.Contains(plan.Main[0].Title, "定选题") {
		t.Fatalf("首切片应为 P1 第一个动作: %+v", plan.Main)
	}
	// 完成一片主推进：P1 的 action 消化
	first := s.Plan.Main[0].Title
	s.CompleteMain(first)
	if s.Goals[0].Actions[0] == first {
		t.Fatal("完成的动作应从验收清单消化")
	}
	// 沙盒验收通过 → 履记「用过」
	lib := map[string]string{"main.go": "x"}
	_ = lib
	// 自报：完成且还想来 → bandit 收到高奖励
	s.Done.Shape = "standard"
	s.SettleMood("还想来", true)
	if s.Bandit.Posterior["local|standard"][0] < 2 {
		t.Fatal("bandit 应结算正向奖励")
	}
	// /api/state 可用
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, httptest.NewRequest("GET", "/api/state", nil))
	if r2.Code != 200 {
		t.Fatalf("/api/state 失败: %d", r2.Code)
	}
	// 证据页
	r3 := httptest.NewRecorder()
	h.ServeHTTP(r3, httptest.NewRequest("GET", "/evidence.html", nil))
	if !strings.Contains(r3.Body.String(), "学习履历") {
		t.Fatal("证据页应渲染")
	}
}
