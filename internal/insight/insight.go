// Package insight —— 重要发现要留住，不能只沉在对话里。
//
// 学生实测 2026-11-02：助教当场说出「线上访问的路径可能被中间层（网关/平台）先接管，
// 你看到的响应未必来自你的进程」——这种是能反复用的真规律，
// 不记下来等于每次重新踩。
//
// 和 weakpoints 的区别：weakpoints 记「他哪里错了」，insight 记「世界是怎么运转的」。
package insight

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Item 一条发现。
type Item struct {
	ID   string    `json:"id"`
	Text string    `json:"text"` // 一句话规律
	From string    `json:"from"` // 从哪节课/哪次发现来的
	At   time.Time `json:"at"`
}

// Store 落盘位置。
const File = "insights.json"

// Load 读全部。
func Load(dir string) []Item {
	raw, err := os.ReadFile(filepath.Join(dir, File))
	if err != nil {
		return nil
	}
	var items []Item
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	return items
}

// Add 加一条；同一句话（去空白后哈希相同）只留一条，避免重复让清单变成噪声。
func Add(dir, text, from string) (Item, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, false, nil
	}
	items := Load(dir)
	id := hash(text)
	for _, it := range items {
		if it.ID == id {
			return it, false, nil
		}
	}
	it := Item{ID: id, Text: text, From: strings.TrimSpace(from), At: time.Now()}
	items = append(items, it)
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return Item{}, false, err
	}
	if err := os.WriteFile(filepath.Join(dir, File), raw, 0o644); err != nil {
		return Item{}, false, err
	}
	return it, true, nil
}

// Recent 最近 n 条（新的在后）。
func Recent(dir string, n int) []Item {
	items := Load(dir)
	if len(items) > n {
		items = items[len(items)-n:]
	}
	return items
}

func hash(s string) string {
	h := sha1.Sum([]byte(strings.Join(strings.Fields(s), "")))
	return hex.EncodeToString(h[:6])
}

// Lines 从助教回复里抽出所有 [INSIGHT:...] 标记的内容。
func Lines(reply string) []string {
	var out []string
	rest := reply
	for {
		i := strings.Index(rest, "[INSIGHT:")
		if i < 0 {
			break
		}
		rest = rest[i+len("[INSIGHT:"):]
		j := strings.Index(rest, "]")
		if j < 0 {
			break
		}
		line := strings.TrimSpace(rest[:j])
		if line != "" {
			out = append(out, line)
		}
		rest = rest[j+1:]
	}
	return out
}
