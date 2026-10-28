package server

// round9_test.go — 第九轮-P0-1 验收（服务端出界）：
// 未配置 LLM 时调 /api/exercise/generate，响应体不含 llm.json/盘符/HTTP；
// 同一时刻 /api/diagnostics 能查到完整技术细节。
import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRound9P01UserCopyCleanDetailLogged(t *testing.T) {
	s := NewServer(t.TempDir()) // 无 llm.json → 模型未配置
	h := s.Handlers()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/api/exercise/generate", nil))
	body := r.Body.String()
	if r.Code != 400 {
		t.Fatalf("无模型应 400: %d %s", r.Code, body)
	}
	for _, leak := range []string{"llm.json", "HTTP", "C:\\", "/home/"} {
		if strings.Contains(body, leak) {
			t.Fatalf("响应体不应含 %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "去设置") {
		t.Fatalf("用户文案应带下一步: %s", body)
	}
	// 技术细节进了诊断环（日志同时有，见测试输出）
	rd := httptest.NewRecorder()
	h.ServeHTTP(rd, httptest.NewRequest("GET", "/api/diagnostics", nil))
	var out struct {
		Diagnostics []struct {
			Path   string `json:"path"`
			Detail string `json:"detail"`
		} `json:"diagnostics"`
	}
	if json.Unmarshal(rd.Body.Bytes(), &out) != nil {
		t.Fatalf("diagnostics 应可解析: %s", rd.Body.String())
	}
	found := false
	for _, d := range out.Diagnostics {
		if d.Path == "/api/exercise/generate" && strings.Contains(d.Detail, "llm.json") {
			found = true
		}
	}
	if !found {
		t.Fatalf("诊断环应含技术细节: %s", rd.Body.String())
	}
}

// 第九轮-P0-2 验收：三个空态各自有「为什么空 + 一个主按钮」，且按钮指向正确视图。
// （跳转目标以 onclick 函数断言——todayGoLearn=今天的课，todayGoPractice=去做题）
func TestRound9P02EmptyStatesActionable(t *testing.T) {
	s := NewServer(t.TempDir())
	h := s.Handlers()
	r := doReq(h, "GET", "/", "")
	if r.Code != 200 {
		t.Fatalf("首页应 200: %d", r.Code)
	}
	html := r.Body.String()
	for _, tc := range []struct{ why, target string }{
		{"练习要等今天这节收尾后生成", `onclick="todayGoLearn()"`},
		{"答对并通过点亮的才会出现在这里", `onclick="todayGoPractice()"`},
		{"关联图只画你真实调用过的知识点", `onclick="todayGoPractice()"`},
	} {
		i := strings.Index(html, tc.why)
		if i < 0 {
			t.Fatalf("空态说明缺失: %q", tc.why)
		}
		end := i + 700
		if end > len(html) {
			end = len(html)
		}
		if !strings.Contains(html[i:end], tc.target) {
			t.Fatalf("空态 %q 的主按钮应指向 %s", tc.why, tc.target)
		}
	}
}

// 第九轮-P1-2 验收（静态面）：aria ≥10 处、Tab 链（输入框1→发送2→三步3）、
// Esc 关弹层的 keydown 处理在场、侧栏 div 导航已按钮化（role=button + Enter/Space）。
// 键盘全流程（Tab→输入→Enter 发送→Esc 关卡片）另有 Playwright 实测，记录在 P1-3 同一浏览会话。
func TestRound9P12KeyboardAccess(t *testing.T) {
	s := NewServer(t.TempDir())
	h := s.Handlers()
	html := doReq(h, "GET", "/", "").Body.String()
	if n := strings.Count(html, "aria-label="); n < 10 {
		t.Fatalf("aria-label 应 ≥10 处: %d", n)
	}
	for _, want := range []string{
		`aria-label="回答输入框" tabindex="1"`,
		`aria-label="发送回答" tabindex="2"`,
		`tabindex="3" onclick="`,         // 今天三步主按钮（Tab 链第 3 环）
		"e.key==='Escape'",               // Esc 关弹层（palette/纪念卡详情）
		`.setAttribute('role','button')`, // 侧栏 div 导航按钮化
		"e.key==='Enter'||e.key===' '",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("键盘可用性要素缺失: %q", want)
		}
	}
}
