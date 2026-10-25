package server

// server_test.go — 应用壳测试：种子装配 → 计划 → 完成 → 自报 → 履历联动 → 证据页。
import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"outpost2/internal/evidence"
	"outpost2/internal/llm"
	"outpost2/internal/profile"
	"outpost2/internal/reflib"
	"outpost2/internal/tutor"
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

// 错题回归闭环：首次答错 → 记入错题本（pending）→ 出题清单包含它 → 做对后转已巩固。
func TestWrongHitReinforceLoop(t *testing.T) {
	s := NewServer(t.TempDir())
	// 同一知识点错两次：计数累计，pending
	s.recordWrongHit("机器学习", "混淆矩阵")
	s.recordWrongHit("机器学习", "混淆矩阵")
	h := s.WrongHits[wrongKey("机器学习", "混淆矩阵")]
	if h == nil || h.Count != 2 || !h.Pending {
		t.Fatalf("错题应记入并 pending: %+v", h)
	}
	// 出题清单：本含错题、他科不含
	got := s.pendingWrongTopics("机器学习")
	if len(got) != 1 || got[0] != "混淆矩阵" {
		t.Fatalf("待巩固清单应含混淆矩阵: %v", got)
	}
	if len(s.pendingWrongTopics("摄影")) != 0 {
		t.Fatal("他科不应看到机器学习的错题")
	}
	// 空知识点不入账
	s.recordWrongHit("机器学习", "  ")
	if len(s.WrongHits) != 1 {
		t.Fatal("空知识点不应入账")
	}
	// 做对 → 转已巩固，出题清单清空
	h.Pending = false
	if len(s.pendingWrongTopics("机器学习")) != 0 {
		t.Fatal("做对后不应再出现在待巩固清单")
	}
}

// 学习者画像：首启建模板 → /api/profile 写读回环 → 重启不丢（画像也是持久事实）。
func TestProfileHandlerRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	if s.ProfilePath == "" {
		t.Fatal("NewServer 应初始化画像路径")
	}
	h := s.Handlers()
	// GET 初始为模板（正文空）
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/profile", nil))
	if r.Code != 200 {
		t.Fatalf("GET /api/profile 失败: %d", r.Code)
	}
	// POST 写入
	body := `{"content":"# 学习者画像\n\n- 后端工程师\n"}`
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, httptest.NewRequest("POST", "/api/profile", strings.NewReader(body)))
	if r2.Code != 200 {
		t.Fatalf("POST /api/profile 失败: %d", r2.Code)
	}
	// 重启后仍在
	s2 := NewServer(dir)
	if got := profile.Load(s2.ProfilePath); !strings.Contains(got, "后端工程师") {
		t.Fatalf("重启后画像丢失: %q", got)
	}
}

// 教学参考库：NewServer 建 kb/ 目录 + /api/reflib 暴露清单（教务入库后前端可见）。
func TestRefLibHandler(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	if s.RefDir == "" {
		t.Fatal("NewServer 应初始化参考库目录")
	}
	if err := reflib.Add(s.RefDir, reflib.Doc{ID: "x", Title: "参考一篇"}, "正文"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	s.Handlers().ServeHTTP(r, httptest.NewRequest("GET", "/api/reflib", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "参考一篇") {
		t.Fatalf("/api/reflib 应返回清单: %d %s", r.Code, r.Body.String())
	}
}

// 内容进货：配置回环（fetch.json 持久化）+ 注入 mock 助教真跑一次进货（工具落参考库+报告落盘）。
func TestFetchCurate(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	h := s.Handlers()
	// 配置代理与时刻
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/api/fetch/config", strings.NewReader(`{"proxy":"http://127.0.0.1:7897","hour":8}`)))
	if r.Code != 200 {
		t.Fatalf("config 失败: %d", r.Code)
	}
	// GET 状态
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, httptest.NewRequest("GET", "/api/fetch", nil))
	if !strings.Contains(r2.Body.String(), "7897") || !strings.Contains(r2.Body.String(), `"hour":8`) {
		t.Fatalf("状态应含代理与时刻: %s", r2.Body.String())
	}
	// 重启后配置仍在
	s2 := NewServer(dir)
	if s2.FetchConf.Proxy != "http://127.0.0.1:7897" || s2.FetchConf.Hour != 8 {
		t.Fatalf("fetch.json 未持久化: %+v", s2.FetchConf)
	}
	// 注入 mock 助教（先调 kb_save 工具再收口）→ 真跑一次进货
	s2.Tutor = &tutor.Tutor{Dir: dir, Client: &curatorMock{}, RefDir: s2.RefDir}
	report, err := s2.doCurate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, "进货完成") {
		t.Fatalf("报告不对: %q", report)
	}
	if idx := reflib.LoadIndex(s2.RefDir); len(idx.Docs) == 0 {
		t.Fatal("进货应已入库参考库")
	}
	// LLM 未配置时 RunCurate 应拒绝
	s3 := NewServer(t.TempDir())
	s3.Tutor = nil
	if err := s3.RunCurate(); err == nil {
		t.Fatal("无 LLM 应拒绝进货")
	}
}

// 笔记工作区：NewServer 建 notes/ 目录 + /api/notes 列表与诊断状态 + 巡检按钮拒绝无 LLM。
func TestNotesHandler(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	if s.NotesDir == "" {
		t.Fatal("NewServer 应初始化笔记目录")
	}
	os.WriteFile(filepath.Join(s.NotesDir, "线性回归.md"), []byte("梯度下降就是往低处走"), 0o644)
	h := s.Handlers()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/notes", nil))
	if r.Code != 200 || !strings.Contains(r.Body.String(), "线性回归.md") {
		t.Fatalf("/api/notes 应返回笔记列表: %d %s", r.Code, r.Body.String())
	}
	// 无 LLM → 巡检拒绝
	s.Tutor = nil
	r2 := httptest.NewRecorder()
	h.ServeHTTP(r2, httptest.NewRequest("POST", "/api/notes/review", nil))
	if r2.Code != 400 {
		t.Fatalf("无 LLM 巡检应 400: %d", r2.Code)
	}
	// noteContext 含科目与知识点状态
	s3 := NewServer(t.TempDir())
	s3.Tutor = &tutor.Tutor{Dir: t.TempDir()}
	ctx := s3.noteContext()
	if !strings.Contains(ctx, "还没有开课") {
		t.Fatalf("无课上下文应有兜底: %q", ctx)
	}
}

// curatorMock 只会调 kb_save 工具然后收口（无网络）。
type curatorMock struct{}

func (m *curatorMock) Chat(system string, msgs []llm.Message) (string, error) { return "ok", nil }
func (m *curatorMock) ChatJSON(system string, msgs []llm.Message) (string, error) {
	return "{}", nil
}
func (m *curatorMock) ChatTools(system string, msgs []llm.Message, tools []llm.Tool) (string, []llm.ToolCall, error) {
	for _, msg := range msgs { // 已有工具结果 → 收口
		if msg.Role == "tool" {
			return "进货完成：本轮存了 1 篇。", nil, nil
		}
	}
	args, _ := json.Marshal(map[string]string{"title": "进货测试仓库", "digest": "测试用", "content": "# 测试"})
	return "", []llm.ToolCall{{ID: "c1", Name: "kb_save", ArgsRaw: string(args)}}, nil
}
