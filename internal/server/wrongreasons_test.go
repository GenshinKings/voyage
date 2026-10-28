package server

// wrongreasons_test.go — 第八轮-P0-2 验收：错因真的参与教学。
import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"voyage/internal/tutor"
)

func postJSON(s *Server, path, body string) map[string]any {
	t2 := httptest.NewRecorder()
	s.Handlers().ServeHTTP(t2, httptest.NewRequest("POST", path, strings.NewReader(body)))
	if t2.Code != 200 {
		return map[string]any{"__code": float64(t2.Code), "__body": t2.Body.String()}
	}
	var out map[string]any
	_ = json.Unmarshal(t2.Body.Bytes(), &out)
	return out
}

// 验收：同一知识点 slip×2 的历史 → 开课时 guideLevels 初始值=2（半提示），不是 1（样例）。
func TestGuideStartsAtHintAfterTwoSlips(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.WrongReasons = map[string]map[string]int{"py-01": {"slip": 2}}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	out := postJSON(s, "/api/tutor/open", `{"nodeId":"py-01"}`)
	if _, bad := out["__code"]; bad {
		t.Fatalf("open 失败: %v", out)
	}
	if got := s.GuideLevels["py-01"]; got != tutor.GuideHint {
		t.Fatalf("连错 2 次应从半提示档起步: %d", got)
	}
	if s.Tutor.GuideLevel != tutor.GuideHint {
		t.Fatalf("人级运行时档位应同步: %d", s.Tutor.GuideLevel)
	}
}

// 验收：预置一条相关错因 → 开场文本出现该错因的自然表述（关键词断言）。
func TestOpeningNamesRecentWrongReason(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	// 昨天在 py-02（同科目）把变量名拼错——证据与本节标题有「变量」交叠 → 相关
	s.WrongLog = []WrongEvent{{At: time.Now().AddDate(0, 0, -1), LessonID: "py-02",
		Subject: "Python", Reason: tutor.CauseSlip, Evidence: "变量名拼错了：nmae 应该是 name"}}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	out := postJSON(s, "/api/tutor/open", `{"nodeId":"py-01"}`)
	sessb, _ := json.Marshal(out["session"])
	sess := &tutor.Session{}
	_ = json.Unmarshal(sessb, sess)
	opening := sess.Messages[len(sess.Messages)-1].Content
	if !strings.Contains(opening, "变量名拼错") {
		t.Fatalf("开场应点名最近的错因（关键词断言）: %q", opening)
	}
	if !strings.Contains(opening, "上次你错在") {
		t.Fatalf("错因引用应是自然句: %q", opening)
	}
}

// 验收：无历史时，开场不得出现任何「上次你…」类句子。
func TestOpeningNoHistoryNoLastTimeSentence(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	out := postJSON(s, "/api/tutor/open", `{"nodeId":"py-01"}`)
	sessb, _ := json.Marshal(out["session"])
	sess := &tutor.Session{}
	_ = json.Unmarshal(sessb, sess)
	opening := sess.Messages[len(sess.Messages)-1].Content
	if strings.Contains(opening, "上次你") || strings.Contains(opening, "误因回看") {
		t.Fatalf("无历史不得出现「上次你」类句子: %q", opening)
	}
}

// 不相关错因（他科目）不进开场。
func TestOpeningIgnoresForeignSubjectWrongs(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.WrongLog = []WrongEvent{{At: time.Now(), LessonID: "ph-01",
		Subject: "摄影", Reason: tutor.CauseConcept, Evidence: "光圈快门搞反了"}}
	s.Tutor = &tutor.Tutor{Dir: dir, Client: echoLLM{}}
	if note := s.subjectWrongNote(findLesson(s, "py-01")); note != "" {
		t.Fatalf("他科目错因不得引用: %q", note)
	}
}

// 验收：partial 收尾且本节有未消除错因 → 响应带「下次会先考你这个」预告。
func TestPartialVerdictCarriesPreview(t *testing.T) {
	dir := t.TempDir()
	s := NewServer(dir)
	seedLesson(t, dir, "py-01", "Python", "变量与类型")
	s.Tutor = &tutor.Tutor{Dir: dir, Client: &seqLLM{replies: []string{
		"检查题：把输出贴上来？",
		"答了一半。【PASS:partial】\n[CAUSE:slip|漏贴了第二步的输出]", // 半懂收尾+错因
	}}}
	postJSON(s, "/api/tutor/open", `{"nodeId":"py-01"}`)
	out := postJSON(s, "/api/tutor/send", `{"nodeId":"py-01","text":"hello"}`)
	if out["verdict"] != "partial" {
		t.Fatalf("应半懂收尾: %v", out["verdict"])
	}
	preview, _ := out["preview"].(string)
	if !strings.Contains(preview, "下次会先考你这个") {
		t.Fatalf("partial 应带下节预告: %v", out)
	}
}

// wrongRelated 的 2 字交叠判据单元测试。
func TestContainsOverlap(t *testing.T) {
	if !containsOverlap("变量名拼错了", "变量与类型") {
		t.Fatal("「变量」交叠应判相关")
	}
	if containsOverlap("光圈快门", "变量与类型") {
		t.Fatal("无交叠不应判相关")
	}
	if containsOverlap("光", "光合作用") {
		t.Fatal("单字不构成关键词交叠")
	}
}
