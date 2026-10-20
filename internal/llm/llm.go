// Package llm — 前哨 2.0 的 LLM 客户端（DeepSeek/OpenAI 兼容 chat API）。
// 配置：outpost2-data/llm.json（baseUrl/key/model）——已从 1.x 迁移。
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	Config Config
	HTTP   *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{Config: cfg, HTTP: &http.Client{Timeout: 180 * time.Second}}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat 多轮对话（system + 历史 + 本题）。
func (c *Client) Chat(system string, msgs []Message) (string, error) {
	if c.Config.Key == "" {
		return "", fmt.Errorf("LLM 未配置（outpost2-data/llm.json）")
	}
	all := append([]Message{{Role: "system", Content: system}}, msgs...)
	body, _ := json.Marshal(chatRequest{Model: c.Config.Model, Messages: all})
	url := c.Config.BaseURL + "/chat/completions"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Config.Key)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var lr chatResponse
	if err := json.Unmarshal(b, &lr); err != nil {
		return "", fmt.Errorf("响应解析失败（HTTP %d）: %w", resp.StatusCode, err)
	}
	if lr.Error != nil && lr.Error.Message != "" {
		return "", fmt.Errorf("LLM API 错误: %s", lr.Error.Message)
	}
	if len(lr.Choices) == 0 {
		return "", fmt.Errorf("LLM 返回为空（HTTP %d）", resp.StatusCode)
	}
	return lr.Choices[0].Message.Content, nil
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
