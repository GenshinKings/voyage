package server

// handlers_test.go — 第八轮-P1-2 验收：三条主链路的 handler 级测试（httptest）。
// 每条链路：成功 + 参数错误 + 状态冲突。
import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voyage/internal/exercise"
	"voyage/internal/llm"
	"voyage/internal/tutor"
)

func doReq(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRecorder()
	if body == "" {
		h.ServeHTTP(r, httptest.NewRequest(method, path, nil))
	} else {
		h.ServeHTTP(r, httptest.NewRequest(method, path, strings.NewReader(body)))
	}
	return r
}

// ---------- 链路 1：/api/state ----------

func TestAPIStateHandler(t *testing.T) {
	s := NewServer(t.TempDir())
	seedLesson(t, s.Dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: s.Dir, Client: echoLLM{}}
	h := s.Handlers()

	// ① 成功：200 + 关键载荷在场（curriculum/today/copy）
	r := doReq(h, "GET", "/api/state", "")
	if r.Code != 200 {
		t.Fatalf("state 应 200: %d", r.Code)
	}
	var out map[string]any
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatal("state 应为 JSON")
	}
	for _, key := range []string{"curriculum", "today", "copy", "sessions", "ledger"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("state 缺字段 %s", key)
		}
	}
	// ② 条件请求：ETag 命中 → 304（不发全量）
	etag := r.Header().Get("ETag")
	if etag != "" {
		rIf := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/state", nil)
		req.Header.Set("If-None-Match", etag)
		h.ServeHTTP(rIf, req)
		if rIf.Code != 304 {
			t.Fatalf("ETag 命中应 304: %d", rIf.Code)
		}
	}
	// ③ 状态分支：时间预算 10 → exercises/dungeons 不下发（两分钟档只留一步）
	s.Done.TimeBudget = "10"
	r4 := doReq(h, "GET", "/api/state", "")
	var out4 map[string]any
	_ = json.Unmarshal(r4.Body.Bytes(), &out4)
	if exs, ok := out4["exercises"].([]any); ok && len(exs) != 0 {
		t.Fatalf("预算 10 不应下发 exercises: %d", len(exs))
	}
}

// ---------- 链路 2：/api/tutor/send ----------

func TestAPITutorSendHandler(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &seqLLM{replies: []string{
		"检查题：把输出贴上来？", // open 开场
		"很好。",         // send 回复（无标记 → 结构化判定接住）
	}, judgeJSON: `{"pass":true,"reason":"输出正确","cause":"none"}`}}
	h := s.Handlers()

	// ② 参数错误：未知 nodeId → 404
	if r := doReq(h, "POST", "/api/tutor/send", `{"nodeId":"nope","text":"x"}`); r.Code != 404 {
		t.Fatalf("未知知识点应 404: %d", r.Code)
	}
	// ③ 状态冲突：未 open 先 send → 500（会话不存在）
	if r := doReq(h, "POST", "/api/tutor/send", `{"nodeId":"py-01","text":"x"}`); r.Code != 500 {
		t.Fatalf("未 open 应 500: %d", r.Code)
	}
	// ① 成功：open 后 send，结构化通道判过
	if r := doReq(h, "POST", "/api/tutor/open", `{"nodeId":"py-01"}`); r.Code != 200 {
		t.Fatalf("open 失败: %d", r.Code)
	}
	r := doReq(h, "POST", "/api/tutor/send", `{"nodeId":"py-01","text":"Python 3.12"}`)
	if r.Code != 200 {
		t.Fatalf("send 失败: %d %s", r.Code, r.Body.String())
	}
	var out struct {
		Reply   string `json:"reply"`
		Verdict string `json:"verdict"`
		Guide   int    `json:"guide"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	if out.Verdict != "pass" {
		t.Fatalf("应判过: %+v", out)
	}
}

// ---------- 链路 3：/api/exercise/answer ----------

func TestAPIExerciseAnswerHandler(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	// 练习引擎挂 mock；预置一道进行中的题（不经 generate——那是另一条链路）
	s.Exer = &exercise.Engine{Client: &seqLLM{replies: []string{"答对了，解法完整。\n[PASS]"}}, AgentsPath: s.AgentsPath}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	s.Exercises = []exercise.Exercise{{ID: "ex-1", Title: "变量改值", Topic: "变量", Subject: "Python",
		LessonID: "py-01", Status: "active", Messages: []llm.Message{{Role: "assistant", Content: "题面：把 x 改成 3"}}}}
	s.saveState()
	h := s.Handlers()

	// ② 参数错误：未知练习 id → 404
	if r := doReq(h, "POST", "/api/exercise/answer", `{"id":"nope","text":"x"}`); r.Code != 404 {
		t.Fatalf("未知练习应 404: %d", r.Code)
	}
	// ③ 状态冲突：引擎被拆走（LLM 未配置）→ 400
	saved := s.Exer
	s.Exer = nil
	if r := doReq(h, "POST", "/api/exercise/answer", `{"id":"ex-1","text":"x=3"}`); r.Code != 400 {
		t.Fatalf("无批改引擎应 400: %d", r.Code)
	}
	s.Exer = saved
	// ① 成功：作答通过 → verdict true + 状态 done + 履历「用过」
	r := doReq(h, "POST", "/api/exercise/answer", `{"id":"ex-1","text":"x = 3"}`)
	if r.Code != 200 {
		t.Fatalf("answer 失败: %d %s", r.Code, r.Body.String())
	}
	var out struct {
		Verdict bool `json:"verdict"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &out)
	if !out.Verdict || s.Exercises[0].Status != "done" {
		t.Fatalf("应通过且转 done: %+v %+v", out, s.Exercises[0].Status)
	}
	used := false
	for _, p := range s.Ledger.List() {
		if p.ID == "ex-1" {
			used = true
		}
	}
	if !used {
		t.Fatal("履历应记「用过」")
	}
}
