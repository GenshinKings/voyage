package server

// verdict_e2e_test.go — 第八轮-P0-1 验收（服务端全链路）：
// 「模型永不吐 [PASS]」的假客户端跑完一整节课，仍正常收尾（结构化通道），diagnostics.channel=structured。
import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voyage/internal/evidence"
	"voyage/internal/llm"
	"voyage/internal/tutor"
)

// seqLLM 教学回复按序轮转（Chat），判定 JSON 固定（ChatJSON）。
type seqLLM struct {
	replies   []string
	n         int
	judgeJSON string
}

func (m *seqLLM) Chat(system string, msgs []llm.Message) (string, error) {
	if m.n < len(m.replies) {
		r := m.replies[m.n]
		m.n++
		return r, nil
	}
	return m.replies[len(m.replies)-1], nil
}
func (m *seqLLM) ChatJSON(system string, msgs []llm.Message) (string, error) {
	return m.judgeJSON, nil
}

func TestFullLessonClosesViaStructuredChannel(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &seqLLM{
		replies: []string{
			"开场：先装 Python。检查题：运行 python -V，把输出贴上来？", // 开场（含检查题）
			"很好，版本号出来了。",                            // 回答 1：无标记 → 结构化判定
			"理由说得通。",                                // FU 回答
			"好，收尾。[FU-PASS]",                        // FU 判定（模型这里给标记；若不给，bare-[PASS] 兜底在 marker 通道）
		},
		judgeJSON: `{"pass":true,"reason":"输出了版本号","cause":"none"}`,
	}}
	h := s.Handlers()
	post := func(path, body string) map[string]any {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if r.Code != 200 {
			t.Fatalf("%s → %d: %s", path, r.Code, r.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &out)
		return out
	}
	post("/api/tutor/open", `{"nodeId":"py-01"}`)
	v1 := post("/api/tutor/send", `{"nodeId":"py-01","text":"输出 Python 3.12.0"}`)
	if v1["verdict"] != "pass" {
		t.Fatalf("无标记回答应经结构化通道判过: %v", v1["verdict"])
	}
	// 首过必追问（第六轮-P1-1），追问守住才真正收尾
	post("/api/tutor/send", `{"nodeId":"py-01","text":"因为 -V 让解释器打印版本号"}`)
	// 会话诊断里必须有 structured 通道的 pass
	b, _ := os.ReadFile(filepath.Join(dir, "tutor", "py-01.json"))
	var sess tutor.Session
	if json.Unmarshal(b, &sess) != nil {
		t.Fatal("会话文件应可解析")
	}
	found := false
	for _, d := range sess.Diagnostics {
		if d.Channel == "structured" && d.Verdict == "pass" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics 缺 structured pass: %+v", sess.Diagnostics)
	}
	// 最后一轮 FU 收尾：课程推进 + 履历入账（整节课闭环没断）
	post("/api/tutor/send", `{"nodeId":"py-01","text":"因为 -V 让解释器打印版本号"}`)
	cur := s.Tutor.Curriculum()
	if cur.Lessons[0].Status != "done" {
		t.Fatalf("课程应收尾: %+v", cur.Lessons[0])
	}
	known := false
	for _, p := range s.Ledger.List() {
		if p.ID == "py-01" && p.Depth == evidence.Known {
			known = true
		}
	}
	if !known {
		t.Fatal("履历应记「知道」——收尾闭环完整")
	}
}

// 结构化通道判 fail → 误因入错因分布（与标记通道的 [CAUSE] 同库）。
func TestStructuredFailRecordsCause(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &seqLLM{
		replies: []string{
			"检查题：把输出贴上来？",
			"你只贴了一半输出。", // 无标记、无 [CAUSE] —— 结构化通道接管
		},
		judgeJSON: `{"pass":false,"reason":"没贴出要求的第二步","cause":"unfinished"}`,
	}}
	h := s.Handlers()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/api/tutor/open", strings.NewReader(`{"nodeId":"py-01"}`)))
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/api/tutor/send", strings.NewReader(`{"nodeId":"py-01","text":"hello"}`)))
	if r.Code != 200 {
		t.Fatal(r.Body.String())
	}
	dist := s.WrongReasons["py-01"]
	if dist == nil || dist["unfinished"] != 1 {
		t.Fatalf("结构化 fail 误因应入库: %+v", s.WrongReasons)
	}
	if s.WrongNotes["py-01"] == "" {
		t.Fatal("证据应入 WrongNotes")
	}
}
