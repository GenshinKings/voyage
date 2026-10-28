package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"voyage/internal/usererr"
)

// ---------- 基建：假服务器 + 假退避钟（不真连网、不真睡） ----------

type fakeResp struct {
	code   int
	body   string
	header map[string]string
}

// seqServer 依次返回 responses，结束后重复最后一个；计数请求次数。
func seqServer(t *testing.T, responses ...fakeResp) (*httptest.Server, *int64) {
	t.Helper()
	var n, idx int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(atomic.AddInt64(&n, 1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1
		} else {
			atomic.StoreInt64(&idx, int64(i))
		}
		r := responses[i]
		for k, v := range r.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(r.code)
		_, _ = w.Write([]byte(r.body))
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

type fakeSleep struct{ calls []time.Duration }

func (f *fakeSleep) install() func() {
	orig := backoffSleep
	backoffSleep = func(d time.Duration) { f.calls = append(f.calls, d) }
	return func() { backoffSleep = orig }
}

func testClient(srv *httptest.Server, timeout time.Duration) *Client {
	hc := srv.Client()
	if timeout > 0 {
		hc.Timeout = timeout
	}
	return &Client{Name: "test", Config: Config{BaseURL: srv.URL, Key: "k", Model: "m"}, HTTP: hc}
}

// ---------- 429 退避（Item-Retry-After 解析） ----------

func Test429HonorsItemRetryAfter(t *testing.T) {
	fs := &fakeSleep{}
	defer fs.install()()
	srv, n := seqServer(t,
		fakeResp{code: 429, body: `{"error":{"message":"rate limited"}}`, header: map[string]string{"Item-Retry-After": "7"}},
		fakeResp{code: 429, body: `rate limited plain`}, // 无 header → 回落指数退避
		fakeResp{code: 200, body: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`},
	)
	c := testClient(srv, 0)
	got, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	if err != nil || got != "ok" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if cnt := atomic.LoadInt64(n); cnt != 3 {
		t.Fatalf("请求数=%d want 3", cnt)
	}
	if len(fs.calls) != 2 {
		t.Fatalf("退避次数=%d want 2", len(fs.calls))
	}
	if fs.calls[0] != 7*time.Second {
		t.Fatalf("第一次退避应遵循 Item-Retry-After=7s，got %v", fs.calls[0])
	}
	if fs.calls[1] != 2*time.Second {
		t.Fatalf("第二次退避应回落指数退避 2s，got %v", fs.calls[1])
	}
}

// assertLayered 第九轮-P0-1：断言错误是两层的——用户文案无技术细节，Detail 含技术现场。
func assertLayered(t *testing.T, err error, wantDetail string) {
	t.Helper()
	if err == nil {
		t.Fatal("应有错误")
	}
	for _, leak := range []string{"HTTP", ".json", "C:\\", "/home/"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("用户文案不应含 %q: %q", leak, err.Error())
		}
	}
	d, ok := usererr.DetailOf(err)
	if !ok || !strings.Contains(d, wantDetail) {
		t.Fatalf("Detail 应含 %q，got %q（分层=%v）", wantDetail, d, ok)
	}
}

// ---------- 500：重试耗尽 ----------

func Test500RetriesThenGivesUp(t *testing.T) {
	fs := &fakeSleep{}
	defer fs.install()()
	srv, n := seqServer(t, fakeResp{code: 500, body: `boom`})
	c := testClient(srv, 0)
	_, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	assertLayered(t, err, "连续 3 次失败")
	if cnt := atomic.LoadInt64(n); cnt != 3 {
		t.Fatalf("请求数=%d want 3", cnt)
	}
	if len(fs.calls) != 2 || fs.calls[0] != time.Second || fs.calls[1] != 2*time.Second {
		t.Fatalf("指数退避应为 1s/2s，got %v", fs.calls)
	}
}

// ---------- 超时：网络类错误可重试 ----------

func TestTimeoutIsRetryable(t *testing.T) {
	fs := &fakeSleep{}
	defer fs.install()()
	var n int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&n, 1)
		time.Sleep(60 * time.Millisecond) // 客户端超时 5ms → 必超时
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	c := testClient(srv, 5*time.Millisecond)
	_, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	assertLayered(t, err, "连续 3 次失败")
	if cnt := atomic.LoadInt64(&n); cnt != 3 {
		t.Fatalf("请求数=%d want 3", cnt)
	}
}

// ---------- 坏 JSON：不可重试，立即失败 ----------

func TestBadJSONNotRetried(t *testing.T) {
	fs := &fakeSleep{}
	defer fs.install()()
	srv, n := seqServer(t, fakeResp{code: 200, body: `<html>not json</html>`})
	c := testClient(srv, 0)
	_, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	assertLayered(t, err, "响应解析失败")
	if cnt := atomic.LoadInt64(n); cnt != 1 {
		t.Fatalf("坏 JSON 不可重试，请求数=%d want 1", cnt)
	}
	if isRetryable(err) {
		t.Fatal("解析失败不应可重试")
	}
	if len(fs.calls) != 0 {
		t.Fatalf("不可重试不应退避，got %v", fs.calls)
	}
}

// ---------- 空 choices ----------

func TestEmptyChoices(t *testing.T) {
	fs := &fakeSleep{}
	defer fs.install()()
	srv, n := seqServer(t, fakeResp{code: 200, body: `{"choices":[]}`})
	c := testClient(srv, 0)
	_, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	assertLayered(t, err, "LLM 返回为空")
	// 空 choices 视为偶发（如内容过滤截断）——可重试，耗尽后报连续失败
	if cnt := atomic.LoadInt64(n); cnt != 3 {
		t.Fatalf("请求数=%d want 3", cnt)
	}
	if len(fs.calls) != 2 {
		t.Fatalf("重试应退避 2 次，got %v", fs.calls)
	}
}

// ---------- API 错误体 + 消息序列指纹 ----------

func TestAPIErrorMessageWithSequence(t *testing.T) {
	srv, n := seqServer(t, fakeResp{code: 400, body: `{"error":{"message":"消息序列不合法"}}`})
	c := testClient(srv, 0)
	_, err := c.Chat("SYS", []Message{{Role: "user", Content: "hi"}})
	assertLayered(t, err, "LLM API 错误")
	if d, _ := usererr.DetailOf(err); !strings.Contains(d, "消息序列: system,user") {
		t.Fatalf("Detail 应含消息序列指纹")
	}
	if cnt := atomic.LoadInt64(n); cnt != 1 {
		t.Fatalf("API 明确拒绝不可重试，请求数=%d want 1", cnt)
	}
}

// ---------- 工具调用参数解析（含失败） ----------

func TestToolCallArgsParse(t *testing.T) {
	bad := ToolCall{ID: "c1", Name: "f", ArgsRaw: `{bad json`}
	if err := bad.Args(&map[string]any{}); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
	good := ToolCall{ID: "c1", Name: "f", ArgsRaw: `{"url":"https://x","n":3}`}
	var v struct {
		URL string `json:"url"`
		N   int    `json:"n"`
	}
	if err := good.Args(&v); err != nil || v.URL != "https://x" || v.N != 3 {
		t.Fatalf("Args 解析失败: %v %+v", err, v)
	}
	if err := (ToolCall{ArgsRaw: ""}).Args(&v); err == nil {
		t.Fatal("空 ArgsRaw 应报错（空串不是合法 JSON）")
	}
}

func TestChatToolsParsesToolCalls(t *testing.T) {
	srv, _ := seqServer(t, fakeResp{code: 200, body: `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c9","type":"function","function":{"name":"kb_save","arguments":"{\"id\":\"x\"}"}}]}}]}`})
	c := testClient(srv, 0)
	content, calls, err := c.ChatTools("SYS", []Message{{Role: "user", Content: "go"}}, nil)
	if err != nil || content != "" || len(calls) != 1 || calls[0].Name != "kb_save" {
		t.Fatalf("content=%q calls=%v err=%v", content, calls, err)
	}
	var args struct {
		ID string `json:"id"`
	}
	if err := calls[0].Args(&args); err != nil || args.ID != "x" {
		t.Fatalf("args=%v err=%v", args, err)
	}
}

// ---------- stripFence ----------

func TestStripFence(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\nplain\n```", "plain"},
		{"  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
		{"no fence at all", "no fence at all"},
		{"```json\n{\"a\":1}\n", `{"a":1}`}, // 缺右围栏：剥头不剥尾
		{"```", ""},                         // 裸围栏无换行：剥头尾后为空
	}
	for _, c := range cases {
		if got := stripFence(c.in); got != c.want {
			t.Errorf("stripFence(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

// ---------- ChatJSON / 空内容 / 未配置 ----------

func TestChatJSONStripsFence(t *testing.T) {
	fenced := "```json\n{\"k\":1}\n```"
	raw, _ := json.Marshal(map[string]any{"choices": []map[string]any{
		{"message": map[string]any{"role": "assistant", "content": fenced}},
	}})
	srv, _ := seqServer(t, fakeResp{code: 200, body: string(raw)})
	c := testClient(srv, 0)
	got, err := c.ChatJSON("SYS", nil)
	if err != nil || got != `{"k":1}` {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestChatNullContent(t *testing.T) {
	srv, _ := seqServer(t, fakeResp{code: 200, body: `{"choices":[{"message":{"role":"assistant","content":null}}]}`})
	c := testClient(srv, 0)
	got, err := c.Chat("SYS", nil)
	if err != nil || got != "" {
		t.Fatalf("content=null 应返回空串，got %q err=%v", got, err)
	}
}

func TestUnconfiguredClient(t *testing.T) {
	c := &Client{Config: Config{BaseURL: "http://127.0.0.1:1"}}
	if _, err := c.Chat("S", nil); err == nil || !strings.Contains(err.Error(), "去设置页") {
		t.Fatalf("无 key 用户文案应指向设置页，got %v", err)
	}
	assertLayered(t, func() error { _, e := c.Chat("S", nil); return e }(), "未配置")
	if _, _, err := c.ChatTools("S", nil, nil); err == nil {
		t.Fatal("ChatTools 无 key 也应报错")
	}
}

// ---------- Store：旧格式迁移 / 坏文件 / 保存往返 ----------

func TestLoadStoreLegacyAndErrors(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "legacy.json")
	os.WriteFile(legacy, []byte(`{"baseUrl":"https://api.test","key":"sk-x","model":"m1"}`), 0o600)
	st, err := LoadStore(legacy)
	if err != nil || st.Active != "default" || st.Profiles["default"].Model != "m1" {
		t.Fatalf("legacy 迁移失败: %+v %v", st, err)
	}
	multi := filepath.Join(dir, "multi.json")
	os.WriteFile(multi, []byte(`{"profiles":{"b":{"name":"b","key":"k2"},"a":{"name":"a","key":"k1"}}}`), 0o600)
	st, err = LoadStore(multi)
	if err != nil || st.Active == "" || len(st.Profiles) != 2 {
		t.Fatalf("multi 解析失败: %+v %v", st, err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte(`not json at all`), 0o600)
	if _, err := LoadStore(bad); err == nil || !strings.Contains(err.Error(), "无法解析") {
		t.Fatalf("坏文件应报无法解析，got %v", err)
	}
	if _, err := LoadStore(filepath.Join(dir, "nope.json")); err == nil {
		t.Fatal("缺文件应报错")
	}
	out := filepath.Join(dir, "out.json")
	if err := st.Save(out); err != nil {
		t.Fatal(err)
	}
	st2, err := LoadStore(out)
	if err != nil || len(st2.Profiles) != 2 {
		t.Fatalf("Save 往返失败: %v", err)
	}
	_, err = (&Store{Active: "none", Profiles: map[string]Profile{}}).ActiveProfile()
	if err == nil || !strings.Contains(err.Error(), "没有激活") {
		t.Fatalf("缺 active profile 应报错，got %v", err)
	}
	if p, err := st2.ActiveProfile(); err != nil || p.Name == "" {
		t.Fatalf("ActiveProfile 应命中: %v %v", p, err)
	}
}

// ---------- 小件：MaskKey / truncateLog / lr2seq / profTag ----------

func TestMaskKey(t *testing.T) {
	if got := (Profile{Key: "sk-1234567890abcdef"}).MaskKey(); got != "sk-1234****cdef" {
		t.Fatalf("got %q", got)
	}
	if got := (Profile{Key: "short"}).MaskKey(); got != "****" {
		t.Fatalf("短 key 应全打码，got %q", got)
	}
}

func TestTruncateLog(t *testing.T) {
	if got := truncateLog(strings.Repeat("x", 200)); len(got) != 163 || !strings.HasSuffix(got, "…") {
		t.Fatalf("超长应截 160 字节+省略号（… 占 3 字节，总 163），len=%d", len(got))
	}
	if got := truncateLog("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
}

func TestLr2Seq(t *testing.T) {
	body := []byte(`{"model":"m","messages":[{"role":"system","content":"s"},{"role":"assistant","tool_calls":[{"id":"1"}]},{"role":"user","content":"u"}]}`)
	if got := strings.Join(lr2seq(body), ","); got != "system,assistant(+tc1),user" {
		t.Fatalf("got %q", got)
	}
	if seq := lr2seq([]byte(`broken`)); len(seq) != 1 || seq[0] != "?" {
		t.Fatalf("坏 body 应返回 ?，got %v", seq)
	}
}

func TestProfTag(t *testing.T) {
	if got := (&Client{Name: "p1", Config: Config{BaseURL: "http://x"}}).profTag(); got != "p1" {
		t.Fatalf("got %q", got)
	}
	if got := (&Client{Config: Config{BaseURL: "http://x"}}).profTag(); got != "http://x" {
		t.Fatalf("无 Name 应回落 BaseURL，got %q", got)
	}
}

// ---------- upstreamError 与 isRetryable ----------

func TestUpstreamErrorAndRetryable(t *testing.T) {
	ue := &upstreamError{code: 429, retryAfter: 3 * time.Second}
	var target *upstreamError
	if !errors.As(fmt.Errorf("wrap: %w", ue), &target) || target.retryAfter != 3*time.Second {
		t.Fatal("errors.As 应能解包 upstreamError")
	}
	if got := ue.Error(); !strings.Contains(got, "服务端要求等待 3s") {
		t.Fatalf("错误文本应含服务端要求间隔，got %q", got)
	}
	if !isRetryable(&upstreamError{code: 502}) {
		t.Fatal("上游 429/5xx 可重试")
	}
	if isRetryable(fmt.Errorf("响应解析失败（HTTP 200）")) {
		t.Fatal("解析失败不可重试")
	}
	if isRetryable(fmt.Errorf("LLM API 错误: bad")) {
		t.Fatal("API 明确拒绝不可重试")
	}
	if !isRetryable(fmt.Errorf(`Get "http://x": context deadline exceeded`)) {
		t.Fatal("网络超时可重试")
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://x", Key: "k", Model: "m"})
	if c.HTTP == nil || c.HTTP.Timeout == 0 {
		t.Fatal("NewClient 应给默认超时")
	}
	if c.profTag() != "default" {
		t.Fatalf("默认 Name=default，got %q", c.profTag())
	}
	p := Profile{Model: "m"}
	if p.Config().Model != "m" {
		t.Fatal("Profile→Config 换算失败")
	}
}
