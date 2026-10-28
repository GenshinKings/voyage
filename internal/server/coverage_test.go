package server

// coverage_test.go — 第八轮-P1-2：handler 级测试的广度补齐（覆盖率 ≥50% 验收）。
import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"voyage/internal/llm"
	"voyage/internal/sandbox"
	"voyage/internal/tutor"
)

// ---------- 纯函数与助手 ----------

func TestTruncateNoteAndTail(t *testing.T) {
	if truncateNote("") != "" {
		t.Fatal("空报告截断应为空")
	}
	long := strings.Repeat("长", 600)
	got := truncateNote(long)
	if !strings.HasSuffix(got, "…（全文见设置页·内容进货）") {
		t.Fatalf("超长应截断并提示全文位置: %.40s", got)
	}
	out := tailLines("a\nb\nc\nd", 2)
	if !strings.Contains(out, "c\nd") || !strings.HasPrefix(out, "…") {
		t.Fatalf("tailLines 应取末 N 行: %q", out)
	}
	if tailLines("a\nb", 5) != "a\nb" {
		t.Fatal("不足 N 行原样返回")
	}
}

func TestSessionHelpersAndFirstFile(t *testing.T) {
	if len(sessMessages(nil)) != 0 {
		t.Fatal("nil 会话应返回空消息")
	}
	if sessProposal(nil) != nil {
		t.Fatal("nil 会话无提案")
	}
	sd := sessionDigest(&tutor.Session{Messages: []llm.Message{{Role: "assistant", Content: "讲了映射"}}})
	if !strings.Contains(sd, "讲了映射") {
		t.Fatalf("摘要应取助教发言: %q", sd)
	}
	c := &sandbox.BreakCase{Files: map[string]string{"main.go": "x"}}
	if firstFile(c) != "main.go" {
		t.Fatalf("首文件应取到: %v", firstFile(c))
	}
	if firstFile(&sandbox.BreakCase{}) != "main.go" {
		t.Fatal("空案例兜底 main.go")
	}
}

func TestGraduationReasonAndIterHour(t *testing.T) {
	s := NewServer(t.TempDir())
	s.FetchConf.IterHour = 99
	if s.iterHour() != 3 {
		t.Fatal("越界时刻回默认 3")
	}
	s.FetchConf.IterHour = 5
	if s.iterHour() != 5 {
		t.Fatal("合法时刻原样")
	}
	if s.hasOpenObservation() {
		t.Fatal("空 backlog 无 open 观察")
	}
	s.Obs = []tutor.Observation{{ID: "o1", Status: "open"}}
	if !s.hasOpenObservation() {
		t.Fatal("有 open 应为真")
	}
	if s.existingObservations() == "（空）" {
		t.Fatal("有 open 观察时清单非空")
	}
}

func TestTodayCareerAndReadAgents(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	if !strings.Contains(s.todayCareer(), "日期") {
		t.Fatalf("生涯数据应含日期头: %q", s.todayCareer())
	}
	if err := os.Remove(s.AgentsPath); err != nil {
		t.Fatal(err)
	}
	if s.readAgents() != "" {
		t.Fatal("无 AGENTS.md 返回空")
	}
	if err := os.WriteFile(s.AgentsPath, []byte("自定义约束"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s.readAgents() != "自定义约束" {
		t.Fatal("readAgents 应热读文件")
	}
}

func TestExtractUserEvidenceVariants(t *testing.T) {
	d := &tutor.Dungeon{Repo: "upstream/proj", Messages: []llm.Message{
		{Role: "user", Content: "做完了 https://github.com/me/my-repo/commit/abc"},
	}}
	if ev := extractUserEvidence(d); !strings.Contains(ev, "github.com/me") {
		t.Fatalf("应抓他自己的仓库链接: %q", ev)
	}
	d2 := &tutor.Dungeon{Repo: "upstream/proj", Messages: []llm.Message{
		{Role: "user", Content: "写在这个文件 C:\\work\\main.py 里"},
	}}
	if ev := extractUserEvidence(d2); !strings.Contains(ev, "main.py") {
		t.Fatalf("应抓本地产物路径: %q", ev)
	}
	d3 := &tutor.Dungeon{Repo: "upstream/proj", Messages: []llm.Message{{Role: "user", Content: "还没做"}}}
	if ev := extractUserEvidence(d3); ev != "" {
		t.Fatalf("找不到证据应留空（不用原仓库冒充）: %q", ev)
	}
}

// ---------- 冷门路由广度 ----------

func TestMiscSysRoutes(t *testing.T) {
	s := NewServer(t.TempDir())
	s.Tutor = &tutor.Tutor{Dir: s.Dir, Client: echoLLM{}}
	h := s.Handlers()
	// /api/refresh：重排今日计划
	if r := doReq(h, "POST", "/api/refresh", ""); r.Code != 200 {
		t.Fatalf("refresh 应 200: %d", r.Code)
	}
	// /api/version
	if r := doReq(h, "GET", "/api/version", ""); r.Code != 200 {
		t.Fatalf("version 应 200: %d", r.Code)
	}
	// /api/pause 参数错误（0 天）与成功
	if r := doReq(h, "POST", "/api/pause", `{"days":0}`); r.Code != 400 {
		t.Fatalf("0 天应 400: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/pause", `{"days":14}`); r.Code != 200 {
		t.Fatalf("暂停应 200: %d", r.Code)
	}
	if s.PausedUntil == nil || !s.PausedUntil.After(time.Now()) {
		t.Fatal("暂停期应写入未来时刻")
	}
	if r := doReq(h, "POST", "/api/pause/end", ""); r.Code != 200 || s.PausedUntil != nil {
		t.Fatal("结束暂停应清空")
	}
	// /api/budget 参数错误与成功
	if r := doReq(h, "POST", "/api/budget", `{"budget":"7"}`); r.Code != 400 {
		t.Fatalf("非法预算应 400: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/budget", `{"budget":"30"}`); r.Code != 200 || s.Done.TimeBudget != "30" {
		t.Fatal("预算 30 应写入")
	}
	// /api/mood：日终自报
	if r := doReq(h, "POST", "/api/mood", `{"mood":"还想来","completed":true}`); r.Code != 200 || s.Done.EndMood != "还想来" {
		t.Fatal("日终自报应写入")
	}
	// /api/config/exp
	if r := doReq(h, "POST", "/api/config/exp", `{"elo":true}`); r.Code != 200 || !s.ExpElo {
		t.Fatal("实验开关应写入")
	}
	// /api/complete-main：消化产出物动作
	if r := doReq(h, "POST", "/api/complete-main", `{"title":"定选题（解决自己真实痛点）"}`); r.Code != 200 {
		t.Fatalf("complete-main 应 200: %d", r.Code)
	}
	// /api/graph
	if r := doReq(h, "GET", "/api/graph", ""); r.Code != 200 {
		t.Fatalf("graph 应 200: %d", r.Code)
	}
	// /evidence.html
	if r := doReq(h, "GET", "/evidence.html", ""); r.Code != 200 {
		t.Fatalf("证据页应 200: %d", r.Code)
	}
	// /api/export/all：zip 且排除 llm.json
	_ = os.WriteFile(filepath.Join(s.Dir, "llm.json"), []byte("{}"), 0o600)
	if r := doReq(h, "GET", "/api/export/all", ""); r.Code != 200 {
		t.Fatalf("导出应 200: %d", r.Code)
	}
	// /api/llm/profiles（无配置也 200）
	if r := doReq(h, "GET", "/api/llm/profiles", ""); r.Code != 200 {
		t.Fatalf("profiles 应 200: %d", r.Code)
	}
	// /api/llm/profile：参数错误与成功配置
	if r := doReq(h, "POST", "/api/llm/profile", `{"name":"","baseUrl":"x","model":"m","key":"k"}`); r.Code != 400 {
		t.Fatalf("缺 name 应 400: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/llm/profile", `{"name":"m1","baseUrl":"http://x","model":"m","key":"sk-test"}`); r.Code != 200 {
		t.Fatalf("配置模型应 200: %d %s", r.Code, r.Body.String())
	}
	// /api/trophy/evidence：不存在 404
	if r := doReq(h, "POST", "/api/trophy/evidence", `{"dungeonId":"nope","evidence":"x"}`); r.Code != 404 {
		t.Fatalf("未知纪念卡应 404: %d", r.Code)
	}
	// /api/resume/doc：无作品 200 空态
	if r := doReq(h, "GET", "/api/resume/doc", ""); r.Code != 200 {
		t.Fatalf("resume/doc 应 200 空态: %d", r.Code)
	}
}

func TestMiscLearnAndShopRoutes(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	h := s.Handlers()
	// /api/subject：GET 列表 / POST 空名 400
	if r := doReq(h, "GET", "/api/subject", ""); r.Code != 200 {
		t.Fatalf("科目列表应 200: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/subject", `{"name":"  "}`); r.Code != 400 {
		t.Fatalf("空科目名应 400: %d", r.Code)
	}
	// /api/direction/open：无会话返回空
	if r := doReq(h, "GET", "/api/direction/open", ""); r.Code != 200 {
		t.Fatalf("direction/open 应 200: %d", r.Code)
	}
	// /api/direction/commit：提案不完整 400
	if r := doReq(h, "POST", "/api/direction/commit", `{"subject":""}`); r.Code != 400 {
		t.Fatalf("空提案应 400: %d", r.Code)
	}
	// /api/dungeon/send：无进行中副本 400
	if r := doReq(h, "POST", "/api/dungeon/send", `{"id":"dn-x","text":"x"}`); r.Code != 400 {
		t.Fatalf("无副本应 400: %d", r.Code)
	}
	// /api/dungeon/reopen / abandon：未知 id 404
	if r := doReq(h, "POST", "/api/dungeon/reopen", `{"id":"nope"}`); r.Code != 404 {
		t.Fatalf("reopen 未知应 404: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/dungeon/abandon", `{"id":"nope"}`); r.Code != 404 {
		t.Fatalf("abandon 未知应 404: %d", r.Code)
	}
	// /api/review/due：空到期卡
	if r := doReq(h, "GET", "/api/review/due", ""); r.Code != 200 {
		t.Fatalf("review/due 应 200: %d", r.Code)
	}
	// /api/complete-review：未知卡 404
	if r := doReq(h, "POST", "/api/complete-review", `{"id":"nope","rating":3}`); r.Code != 404 {
		t.Fatalf("未知复习卡应 404: %d", r.Code)
	}
	// /api/opportunities：GET 首访空态 + POST 触发刷新
	if r := doReq(h, "GET", "/api/opportunities", ""); r.Code != 200 {
		t.Fatalf("opportunities 应 200: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/opportunities", ""); r.Code != 200 {
		t.Fatalf("opportunities 刷新应 200: %d", r.Code)
	}
	// /api/notes 与 /api/reflib
	if r := doReq(h, "GET", "/api/notes", ""); r.Code != 200 {
		t.Fatalf("notes 应 200: %d", r.Code)
	}
	if r := doReq(h, "GET", "/api/reflib", ""); r.Code != 200 {
		t.Fatalf("reflib 应 200: %d", r.Code)
	}
	// /api/backlog 与 /api/fetch
	if r := doReq(h, "GET", "/api/backlog", ""); r.Code != 200 {
		t.Fatalf("backlog 应 200: %d", r.Code)
	}
	if r := doReq(h, "GET", "/api/fetch", ""); r.Code != 200 {
		t.Fatalf("fetch 状态应 200: %d", r.Code)
	}
	// /api/fetch/run 与 /api/reflect/run：LLM 未配置 400；已配置则 200（echo mock 不走网络）
	if r := doReq(h, "POST", "/api/fetch/run", ""); r.Code != 200 {
		t.Fatalf("进货触发应 200: %d", r.Code)
	}
	if r := doReq(h, "POST", "/api/reflect/run", ""); r.Code != 200 {
		t.Fatalf("反思触发应 200: %d", r.Code)
	}
	s2 := NewServer(t.TempDir()) // 无 Tutor：才走「LLM 未配置」400
	h2 := s2.Handlers()
	if r := doReq(h2, "POST", "/api/fetch/run", ""); r.Code != 400 {
		t.Fatalf("无 LLM 进货应 400: %d", r.Code)
	}
	if r := doReq(h2, "POST", "/api/reflect/run", ""); r.Code != 400 {
		t.Fatalf("无 LLM 反思应 400: %d", r.Code)
	}
	// /api/dungeon/list：空列表 200
	if r := doReq(h, "GET", "/api/dungeon/list", ""); r.Code != 200 {
		t.Fatalf("dungeon/list 应 200: %d", r.Code)
	}
}
