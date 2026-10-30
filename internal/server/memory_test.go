package server

// memory_test.go — 第七轮验收：巩固产出三件套齐全、矛盾 UPDATE 不并存、失败不阻塞主流程。
import (
	"context"
	"encoding/json"
	"errors"
	"math"
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

// 验收 4：ts 改成 40 天前且 confidence 0.4 → 跑巩固后进 cold，检索不再返回。
func TestDecayIntegrationViaConsolidate(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	old := time.Now().AddDate(0, 0, -40)
	if _, err := s.Memory.Add(memory.Entry{ID: "m-old", Text: "主人在学 Python 变量与类型", Kind: memory.KindFact,
		Source: "session:py-01@2026-09-18", Ts: old, Confidence: 0.4}, old); err != nil {
		t.Fatal(err)
	}
	s.Memory.Save(dir)
	// 衰减前的检索也拿不到它（双保险）；状态翻转发生在巩固
	if note := s.memoryNote(findLesson(s, "py-01")); note != "" {
		t.Fatalf("满足衰减判据的条目不得被检索引用: %q", note)
	}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &memMockLLM{chatJSON: `{"ops":[]}`}}
	if err := s.consolidateCore(); err != nil {
		t.Fatal(err)
	}
	e := memory.Load(dir).Get("m-old")
	if e.Status != memory.StatusCold {
		t.Fatalf("巩固后应翻转 cold: %s", e.Status)
	}
	if !strings.Contains(s.FetchConf.MemReport, "衰减 1 条") {
		t.Fatalf("报告应写明衰减: %q", s.FetchConf.MemReport)
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

// echoLLM 开场回声：把收到的最后一条 user 原文当回复——便于断言注入了什么。
type echoLLM struct{}

func (echoLLM) Chat(system string, msgs []llm.Message) (string, error) {
	return msgs[len(msgs)-1].Content, nil
}
func (echoLLM) ChatJSON(system string, msgs []llm.Message) (string, error) {
	return `{"ops":[]}`, nil
}

// seedLesson 造课程目录（一节 active）。
func seedLesson(t *testing.T, dir, id, subject, title string) {
	t.Helper()
	cur := tutor.Curriculum{Subjects: []string{subject}, UpdatedAt: time.Now(),
		Lessons: []tutor.Lesson{{ID: id, Subject: subject, Title: title, Brief: "能写带变量的语句", Status: "active", Order: 1}}}
	b, _ := json.Marshal(cur)
	if err := os.WriteFile(filepath.Join(dir, "lessons.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 验收 2：第二天开场确实引用了一条记忆，且引用文本能在条目里逐字找到；引用后 hits/confidence 上调。
func TestOpeningCitesRelevantMemoryVerbatim(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	text := "主人在学 Python，每晚去京东做 1.5 小时临时工"
	if _, err := s.Memory.Add(memory.Entry{ID: "m-1", Text: text, Kind: memory.KindFact,
		Source: "session:py-01@2026-10-27", Ts: time.Now().AddDate(0, 0, -1), Confidence: 0.9}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	lesson := findLesson(s, "py-01")
	s.Tutor.MemoryNote = s.memoryNote(lesson)
	if !strings.Contains(s.Tutor.MemoryNote, text) {
		t.Fatalf("引用笔记应含条目原文: %q", s.Tutor.MemoryNote)
	}
	sess, isNew, err := s.Tutor.StartOrResume(context.Background(), *lesson)
	if err != nil || !isNew {
		t.Fatalf("开场失败: %v", err)
	}
	opening := sess.Messages[len(sess.Messages)-1].Content
	if !strings.Contains(opening, text) {
		t.Fatal("开场应逐字引用条目文本")
	}
	// 入账：hits+1、confidence +0.05（浮点容差）、lastUsedAt 写回文件
	st := memory.Load(dir)
	e := st.Get("m-1")
	if e.Hits != 1 || math.Abs(e.Confidence-0.95) > 1e-9 || e.LastUsedAt.IsZero() {
		t.Fatalf("引用应入账: hits=%d conf=%.17g lastUsed zero=%v", e.Hits, e.Confidence, e.LastUsedAt.IsZero())
	}
}

// 无相关记忆 → 不引用（memoryNote 为空，绝不出「你上次说」）。
func TestOpeningSkipsWhenNoRelevantMemory(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	if _, err := s.Memory.Add(memory.Entry{ID: "m-2", Text: "主人喜欢摄影", Kind: memory.KindPreference,
		Source: "session:x", Ts: time.Now(), Confidence: 0.9}, time.Now()); err != nil {
		t.Fatal(err)
	}
	s.Memory.Save(dir)
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	if note := s.memoryNote(findLesson(s, "py-01")); note != "" {
		t.Fatalf("无关记忆不得引用: %q", note)
	}
	if e := memory.Load(dir).Get("m-2"); e.Hits != 0 {
		t.Fatal("未引用不得入账")
	}
}

// 旧结论措辞：ts 超 30 天且 confidence<0.6（未到 cold 线 0.5 之上）→ 「旧结论（截至 X 月）」。
func TestOpeningStaleMemoryGetsCaveat(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	text := "主人当时在学 Python 基础"
	if _, err := s.Memory.Add(memory.Entry{ID: "m-3", Text: text, Kind: memory.KindFact,
		Source: "session:old", Ts: time.Now().AddDate(0, 0, -40), Confidence: 0.55}, time.Now().AddDate(0, 0, -40)); err != nil {
		t.Fatal(err)
	}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	note := s.memoryNote(findLesson(s, "py-01"))
	if !strings.Contains(note, "旧结论") || !strings.Contains(note, "现在可能变了") || !strings.Contains(note, text) {
		t.Fatalf("旧记忆应用旧结论措辞并保留原文: %q", note)
	}
}
