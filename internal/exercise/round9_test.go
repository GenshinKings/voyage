package exercise

// round9_test.go — 第九轮-P0-1 验收：exercise 三个调用点的用户文案与技术细节分层。
import (
	"strings"
	"testing"

	"voyage/internal/usererr"
)

var leaks = []string{".json", "http", "HTTP", "C:\\", "/home/"}

func assertCleanUserCopy(t *testing.T, err error, wantDetailFragment string) {
	t.Helper()
	if err == nil {
		t.Fatal("应有错误")
	}
	for _, l := range leaks {
		if strings.Contains(err.Error(), l) {
			t.Fatalf("用户文案不应含 %q: %q", l, err.Error())
		}
	}
	d, ok := usererr.DetailOf(err)
	if !ok || !strings.Contains(d, wantDetailFragment) {
		t.Fatalf("Detail 应含 %q，got %q（分层=%v）", wantDetailFragment, d, ok)
	}
}

// 未配置模型出题：用户拿「去设置里贴一个」，llm.json 路径只进 Detail。
func TestGenerateNoClientUserCopy(t *testing.T) {
	e := &Engine{}
	_, err := e.Generate([]string{"变量"}, "")
	assertCleanUserCopy(t, err, "outpost2-data/llm.json")
}

// 未配置模型批改：同上。
func TestAnswerNoClientUserCopy(t *testing.T) {
	e := &Engine{}
	_, _, err := e.Answer(&Exercise{}, "答案")
	assertCleanUserCopy(t, err, "outpost2-data/llm.json")
}

// 出题解析失败：原始 LLM 输出只进 Detail。
func TestGenerateParseFailUserCopy(t *testing.T) {
	e := &Engine{Client: &mockClient{jsonOut: []string{"模型抽风输出的一段不像 JSON 的话"}}}
	_, err := e.Generate([]string{"变量"}, "")
	assertCleanUserCopy(t, err, "出题解析失败")
}
