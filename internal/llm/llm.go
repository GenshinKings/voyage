// Package llm — 前哨 2.0 的 LLM 客户端（DeepSeek/OpenAI 兼容 chat API）。
// 配置：outpost2-data/llm.json（baseUrl/key/model）——已从 1.x 迁移。
package llm

import (
	"log"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL string `json:"baseUrl"`
	Key     string `json:"key"`
	Model   string `json:"model"`
}

// Profile 一个可切换的模型配置（任意 OpenAI 兼容 chat API）。
type Profile struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Key     string `json:"key"`
	Model   string `json:"model"`
}

func (p Profile) Config() Config { return Config{BaseURL: p.BaseURL, Key: p.Key, Model: p.Model} }

// MaskKey 打码显示（前端只见首尾）。
func (p Profile) MaskKey() string {
	if len(p.Key) < 10 {
		return "****"
	}
	return p.Key[:7] + "****" + p.Key[len(p.Key)-4:]
}

// Store llm.json 的多 profile 形态：active 指向当前作为 AI 助教的配置。
type Store struct {
	Active   string             `json:"active"`
	Profiles map[string]Profile `json:"profiles"`
}

// LoadStore 读取并兼容旧单配置格式（无 profiles 时自动迁移为 default）。
func LoadStore(path string) (*Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st Store
	if json.Unmarshal(b, &st) == nil && st.Profiles != nil && len(st.Profiles) > 0 {
		if st.Active == "" {
			for k := range st.Profiles {
				st.Active = k
				break
			}
		}
		return &st, nil
	}
	var legacy Config
	if json.Unmarshal(b, &legacy) == nil && legacy.Key != "" {
		return &Store{Active: "default", Profiles: map[string]Profile{
			"default": {Name: "default", BaseURL: legacy.BaseURL, Key: legacy.Key, Model: legacy.Model},
		}}, nil
	}
	return nil, fmt.Errorf("无法解析 llm.json")
}

func (st *Store) Save(path string) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600) // 0600：含 key，仅本用户可读
}

// ActiveProfile 当前生效的配置。
func (st *Store) ActiveProfile() (Profile, error) {
	p, ok := st.Profiles[st.Active]
	if !ok {
		return p, fmt.Errorf("没有激活的模型配置")
	}
	return p, nil
}

// ChatClient 对话接口（生产=*Client；测试=mock 可注入）。
type ChatClient interface {
	Chat(system string, msgs []Message) (string, error)
	ChatJSON(system string, msgs []Message) (string, error)
}

func LoadConfig(path string) (Config, error) {
	c := Config{}
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

// Message 对话消息。扩展 function calling：assistant 可带 ToolCalls，
// 工具结果以 Role="tool" + ToolCallID 回传（会话持久化向后兼容，旧字段不变）。
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// Tool 暴露给模型的工具（JSON schema 参数声明）。
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolCall 模型发起的一次工具调用（ArgsRaw 为 JSON 字符串，Args() 反序列化）。
type ToolCall struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ArgsRaw string `json:"args"`
}

// Args 把调用参数反序列化到 v。
func (tc ToolCall) Args(v any) error {
	return json.Unmarshal([]byte(tc.ArgsRaw), v)
}

// ToolClient 支持 function calling 的客户端（*Client 实现；mock 可选实现——
// 不实现则调用方回退普通 Chat，教学/进货循环自动降级）。
type ToolClient interface {
	ChatTools(system string, msgs []Message, tools []Tool) (content string, calls []ToolCall, err error)
}

type Client struct {
	Name   string // profile 名（日志/排障用）
	Config Config
	HTTP   *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{Name: "default", Config: cfg, HTTP: &http.Client{Timeout: 180 * time.Second}}
}

// ---------- wire 格式（OpenAI chat API）----------

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []wireMessage `json:"messages"`
	Tools    []any         `json:"tools,omitempty"`
}

type wireToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

func toWire(m Message) wireMessage {
	w := wireMessage{Role: m.Role, ToolCallID: m.ToolCallID}
	if m.Content != "" {
		s := m.Content
		w.Content = &s
	}
	for _, tc := range m.ToolCalls {
		var wt wireToolCall
		wt.ID, wt.Type, wt.Function.Name, wt.Function.Arguments = tc.ID, "function", tc.Name, tc.ArgsRaw
		w.ToolCalls = append(w.ToolCalls, wt)
	}
	return w
}

func fromWire(w wireMessage) Message {
	m := Message{Role: w.Role, ToolCallID: w.ToolCallID}
	if w.Content != nil {
		m.Content = *w.Content
	}
	for _, tc := range w.ToolCalls {
		m.ToolCalls = append(m.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, ArgsRaw: tc.Function.Arguments})
	}
	return m
}

type chatResponse struct {
	Choices []struct {
		Message wireMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// backoffSleep 退避睡眠（包级变量：测试注入假钟，不真睡）。
var backoffSleep = time.Sleep

// upstreamError 上游 429/5xx：携带状态码与服务端要求的重试间隔（Item-Retry-After）。
type upstreamError struct {
	code       int
	retryAfter time.Duration // >0 = 服务端显式指定（429 Item-Retry-After）
	body       string
}

func (e *upstreamError) Error() string {
	if e.retryAfter > 0 {
		return fmt.Sprintf("上游 HTTP %d（可重试，服务端要求等待 %s）: %s", e.code, e.retryAfter, truncateLog(e.body))
	}
	return fmt.Sprintf("上游 HTTP %d（可重试）: %s", e.code, truncateLog(e.body))
}

func (c *Client) post(body []byte) (wireMessage, error) {
	// 小穗五轮二：网络抖动/429/5xx/超时 重试 2 次指数退避（1s/3s）；每次调用写 server.log（时间+耗时+结果）
	// 第六轮研究落地（docs/research/Agent可靠性与记忆.md 规则5）：服务端显式要求等待（Item-Retry-After）时按其秒数退避，覆盖指数退避。
	var msg wireMessage
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s（近似 1s/3s 语义的指数退避）
			var ue *upstreamError
			if errors.As(lastErr, &ue) && ue.retryAfter > 0 {
				backoff = ue.retryAfter
			}
			backoffSleep(backoff)
		}
		start := time.Now()
		msg, lastErr = c.postOnce(body)
		elapsed := time.Since(start)
		if lastErr == nil {
			log.Printf("[LLM] ok %s %s 耗时%.1fs", c.Config.Model, c.profTag(), elapsed.Seconds())
			return msg, nil
		}
		retryable := isRetryable(lastErr)
		log.Printf("[LLM] fail(第%d次) %s %s 耗时%.1fs 错误:%v 可重试:%v", attempt+1, c.Config.Model, c.profTag(), elapsed.Seconds(), truncateLog(lastErr.Error()), retryable)
		if !retryable {
			return wireMessage{}, lastErr
		}
	}
	return wireMessage{}, fmt.Errorf("LLM 调用连续 3 次失败（最后错误：%v）——对话已保留，可直接重试这一节", lastErr)
}

// profTag 模型配置名（排障时可辨认走的是哪个 profile）。
func (c *Client) profTag() string {
	if c.Name != "" {
		return c.Name
	}
	return c.Config.BaseURL
}

func truncateLog(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// isRetryable 用户可读语义的可重试判定：网络错误（超时/连接失败）与 429/5xx 在 postOnce 里包成可识别错误。
func isRetryable(err error) bool {
	s := err.Error()
	if strings.Contains(s, "LLM API 错误") || strings.Contains(s, "响应解析失败") {
		// 明确 API 拒绝/畸形输出不重试（重试大概率同样失败）
		return false
	}
	return true // 其余（网络断/超时/非 2xx 包装错误）可重试
}

func (c *Client) postOnce(body []byte) (wireMessage, error) {
	url := c.Config.BaseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return wireMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Config.Key)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return wireMessage{}, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 429 || resp.StatusCode >= 500 {
		e := &upstreamError{code: resp.StatusCode, body: string(b)}
		if ra := strings.TrimSpace(resp.Header.Get("Item-Retry-After")); ra != "" {
			if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
				e.retryAfter = time.Duration(sec) * time.Second
			}
		}
		return wireMessage{}, e
	}
	var lr chatResponse
	if err := json.Unmarshal(b, &lr); err != nil {
		return wireMessage{}, fmt.Errorf("响应解析失败（HTTP %d）: %w", resp.StatusCode, err)
	}
	if lr.Error != nil && lr.Error.Message != "" {
		// 消息序列指纹：排障时能立刻看出是哪一帧形状不被服务端接受
		return wireMessage{}, fmt.Errorf("LLM API 错误: %s [消息序列: %s]", lr.Error.Message, strings.Join(lr2seq(body), ","))
	}
	if len(lr.Choices) == 0 {
		return wireMessage{}, fmt.Errorf("LLM 返回为空（HTTP %d）", resp.StatusCode)
	}
	return lr.Choices[0].Message, nil
}

// lr2seq 从请求体提取消息角色序列（assistant 带 tool_calls 标注 +tc N）。
func lr2seq(body []byte) []string {
	var req chatRequest
	if json.Unmarshal(body, &req) != nil {
		return []string{"?"}
	}
	out := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		s := m.Role
		if len(m.ToolCalls) > 0 {
			s += fmt.Sprintf("(+tc%d)", len(m.ToolCalls))
		}
		out = append(out, s)
	}
	return out
}

// Chat 多轮对话（system + 历史 + 本题）。
func (c *Client) Chat(system string, msgs []Message) (string, error) {
	if c.Config.Key == "" {
		return "", fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	all := []wireMessage{{Role: "system", Content: &system}}
	for _, m := range msgs {
		all = append(all, toWire(m))
	}
	body, _ := json.Marshal(chatRequest{Model: c.Config.Model, Messages: all})
	wm, err := c.post(body)
	if err != nil {
		return "", err
	}
	if wm.Content == nil {
		return "", nil
	}
	return *wm.Content, nil
}

// ChatTools 带 function calling 的对话：返回模型的文本内容（可为空）与工具调用清单。
// tools 为空时退化为普通 Chat（不发 tools 字段——部分服务端拒绝空数组）。
func (c *Client) ChatTools(system string, msgs []Message, tools []Tool) (string, []ToolCall, error) {
	if c.Config.Key == "" {
		return "", nil, fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	all := []wireMessage{{Role: "system", Content: &system}}
	for _, m := range msgs {
		all = append(all, toWire(m))
	}
	req := chatRequest{Model: c.Config.Model, Messages: all}
	if len(tools) > 0 {
		for _, t := range tools {
			req.Tools = append(req.Tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
	}
	body, _ := json.Marshal(req)
	wm, err := c.post(body)
	if err != nil {
		return "", nil, err
	}
	m := fromWire(wm)
	return m.Content, m.ToolCalls, nil
}

// ChatJSON 要求 JSON 输出并剥围栏。
func (c *Client) ChatJSON(system string, msgs []Message) (string, error) {
	raw, err := c.Chat(system+"\n输出严格 JSON，不要 markdown 围栏。", msgs)
	if err != nil {
		return "", err
	}
	return stripFence(raw), nil
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}
