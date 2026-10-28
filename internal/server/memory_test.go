package server

// memory_test.go — 第七轮验收：巩固产出三件套齐全、矛盾 UPDATE 不并存、失败不阻塞主流程。
import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"voyage/internal/llm"
	"voyage/internal/memory"
	"voyage/internal/tutor"
)

// memMockLLM 确定性 LLM：chatJSON=巩固应答；chatErr/jsonErr 注入失败。
type memMockLLM struct {
	chatJSON string
	jsonErr  error
	// 捕获 Consolidate 收到的输入（断言对话与已有记忆真的进了提示）
	lastUser string
}

func (m *memMockLLM) Chat(system string, msgs []llm.Message) (string, error) {
	return "开场", nil
}
func (m *memMockLLM) ChatJSON(system string, msgs []llm.Message) (string, error) {
	if len(msgs) > 0 {
		m.lastUser = msgs[len(msgs)-1].Content
	}
	if m.jsonErr != nil {
		return "", m.jsonErr
	}
	return m.chatJSON, nil
}

// seedTodaySession 造一场今天的假教学会话（lessons.json 追加 + tutor/<id>.json）。
func seedTodaySession(t *testing.T, dir, id, title, userSay, assistSay string, at time.Time) {
	t.Helper()
	cur := tutor.Curriculum{Subjects: []string{"Python"}, UpdatedAt: at}
	if b, err := os.ReadFile(filepath.Join(dir, "lessons.json")); err == nil {
		_ = json.Unmarshal(b, &cur)
	}
	cur.Lessons = append(cur.Lessons, tutor.Lesson{ID: id, Subject: "Python", Title: title, Brief: "能写出带变量的语句", Status: "done", Order: len(cur.Lessons) + 1})
	b, _ := json.Marshal(cur)
	if err := os.WriteFile(filepath.Join(dir, "lessons.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	sess := tutor.Session{NodeID: id, Title: title, UpdatedAt: at, Messages: []llm.Message{
		{Role: "user", Content: userSay},
		{Role: "assistant", Content: assistSay + "\n[PASS]"},
	}}
	b, _ = json.Marshal(sess)
	if err := os.MkdirAll(filepath.Join(dir, "tutor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tutor", id+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 验收 1：造两天假对话，跑巩固——产出 5~7 条，每条 source/ts/confidence 齐全。
func TestConsolidateProducesCompleteEntries(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	now := time.Now()
	seedTodaySession(t, dir, "py-01", "变量与类型", "我每晚去京东做 1.5 小时临时工，说发了工资给 token 充值", "记下了，这是你的时间预算", now)
	seedTodaySession(t, dir, "py-02", "打印与输入", "我偏好晚上学，安静", "好的，晚上安排", now.Add(-time.Hour))
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{chatJSON: `{"ops":[
		{"op":"ADD","text":"主人 10-28 起每晚去京东做 1.5 小时临时工","kind":"fact","source":"session:py-01@` + now.Format("2006-01-02") + `","confidence":0.9},
		{"op":"ADD","text":"主人偏好晚上学习","kind":"preference","source":"session:py-02@` + now.Format("2006-01-02") + `","confidence":0.85},
		{"op":"ADD","text":"主人说发了工资给 token 充值","kind":"commitment","source":"session:py-01@` + now.Format("2006-01-02") + `","confidence":0.8},
		{"op":"ADD","text":"主人在学 Python 变量与类型","kind":"fact","source":"session:py-01@` + now.Format("2006-01-02") + `","confidence":0.95},
		{"op":"ADD","text":"主人学习时喜欢安静环境","kind":"impression","source":"session:py-02@` + now.Format("2006-01-02") + `","confidence":0.7},
		{"op":"NOOP","reason":"对话里没有更多稳定信息"}
	]}`}}

	if err := s.consolidateCore(); err != nil {
		t.Fatal(err)
	}
	st := memory.Load(dir)
	if len(st.Entries) < 5 || len(st.Entries) > 7 {
		t.Fatalf("应产出 5~7 条: %d", len(st.Entries))
	}
	for _, e := range st.Entries {
		if e.Source == "" || e.Ts.IsZero() || e.Confidence <= 0 || e.Confidence > 1 {
			t.Fatalf("三件套不全: %+v", e)
		}
		if e.Status != memory.StatusActive {
			t.Fatalf("新条目应 active: %+v", e)
		}
	}
	// 对话真的进了提示（用户侧原话在场）
	mock := s.Tutor.Client.(*memMockLLM)
	if !strings.Contains(mock.lastUser, "临时工") {
		t.Fatal("巩固提示应包含当天对话原文")
	}
	// 报告与状态可见
	if !strings.Contains(s.FetchConf.MemReport, "新增 5") {
		t.Fatalf("报告应写明新增 5: %q", s.FetchConf.MemReport)
	}
	if s.FetchConf.MemRun != now.Format("2006-01-02") {
		t.Fatal("MemRun 应记当天")
	}
	// 幂等：当天再跑直接跳过（不再调 LLM）
	if err := s.consolidateCore(); err != nil {
		t.Fatal(err)
	}
}

// 验收 3：矛盾新事实 → 旧条目被 UPDATE（history 有旧值），库里不存在两条相反说法。
func TestConsolidateUpdateSupersedesContradiction(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	now := time.Now()
	seedTodaySession(t, dir, "py-01", "变量与类型", "我改学 Go 了，Python 先放放", "好的，方向换 Go", now)
	old := now.AddDate(0, 0, -3)
	if _, err := s.Memory.Add(memory.Entry{ID: "m-20261025-01", Text: "主人在学 Python",
		Kind: memory.KindFact, Source: "session:py-01@2026-10-25", Ts: old, Confidence: 0.8}, old); err != nil {
		t.Fatal(err)
	}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{chatJSON: `{"ops":[
		{"op":"UPDATE","id":"m-20261025-01","text":"主人 10-28 起改学 Go（Python 暂停）","source":"session:py-01@` + now.Format("2006-01-02") + `","confidence":0.95},
		{"op":"NOOP","reason":"同一条已 UPDATE"}
	]}`}}
	if err := s.consolidateCore(); err != nil {
		t.Fatal(err)
	}
	st := memory.Load(dir)
	if len(st.Entries) != 1 {
		t.Fatalf("库里不得并存两条相反说法: %d 条", len(st.Entries))
	}
	e := st.Entries[0]
	if !strings.Contains(e.Text, "Go") || strings.Contains(e.Text, "在学 Python") {
		t.Fatalf("正文应是新说法: %q", e.Text)
	}
	if len(e.History) != 1 || !strings.Contains(e.History[0].Text, "Python") {
		t.Fatalf("旧值应进 history: %+v", e.History)
	}
}

// 验收 5：巩固失败只记日志与报告，不阻塞主流程（教学/状态接口照常）。
func TestConsolidateFailureDoesNotBlockMainFlow(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	now := time.Now()
	seedTodaySession(t, dir, "py-01", "变量与类型", "今天学变量", "好的", now)
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{jsonErr: errors.New("LLM 超时")}}
	if err := s.consolidateCore(); err == nil {
		t.Fatal("巩固失败应返回错误供日志记录")
	}
	if !strings.HasPrefix(s.FetchConf.MemReport, "巩固失败") {
		t.Fatalf("失败应写进报告: %q", s.FetchConf.MemReport)
	}
	if n := len(memory.Load(dir).Entries); n != 0 {
		t.Fatalf("失败不得写脏数据: %d", n)
	}
	// 主流程活着：/api/state 照常 200
	r := httptest.NewRecorder()
	s.Handlers().ServeHTTP(r, httptest.NewRequest("GET", "/api/state", nil))
	if r.Code != 200 {
		t.Fatalf("失败后主流程受阻塞: %d", r.Code)
	}
}

// 纪律：非法操作逐条丢弃，绝无半条记忆；0 条新增=「今天没什么值得记的」。
func TestConsolidateDropsInvalidOps(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	now := time.Now()
	seedTodaySession(t, dir, "py-01", "变量与类型", "今天学变量", "好的", now)
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{chatJSON: `{"ops":[
		{"op":"ADD","text":"缺 confidence","kind":"fact","source":"session:py-01"},
		{"op":"ADD","text":"坏 kind","kind":"rumor","source":"session:py-01","confidence":0.5},
		{"op":"ADD","text":"","kind":"fact","source":"session:py-01","confidence":0.5},
		{"op":"UPDATE","id":"m-不存在","text":"无的放矢","confidence":0.9},
		{"op":"DELETE","id":"m-不存在","reason":"无的放矢"},
		{"op":"NOOP","reason":"没什么可记"}
	]}`}}
	if err := s.consolidateCore(); err != nil {
		t.Fatal(err)
	}
	if n := len(memory.Load(dir).Entries); n != 0 {
		t.Fatalf("非法操作不得入库: %d", n)
	}
	if !strings.Contains(s.FetchConf.MemReport, "今天没什么值得记的") {
		t.Fatalf("0 新增应明确说没什么值得记: %q", s.FetchConf.MemReport)
	}
}

// 手动入口：/api/memory/consolidate 触发异步巩固。
func TestConsolidateManualEndpoint(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{chatJSON: `{"ops":[]}`}}
	h := s.Handlers()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/api/memory/consolidate", nil))
	if r.Code != 200 {
		t.Fatalf("手动巩固应 200: %d", r.Code)
	}
	// LLM 未配置 → 400，不 panic
	s2 := NewServer(t.TempDir())
	r2 := httptest.NewRecorder()
	s2.Handlers().ServeHTTP(r2, httptest.NewRequest("POST", "/api/memory/consolidate", nil))
	if r2.Code != 400 {
		t.Fatalf("无 LLM 应 400: %d", r2.Code)
	}
}
