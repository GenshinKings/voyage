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
