package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"voyage/internal/llm"
)

// 笔记打分与整库体检（InkPal 侧边栏用）。
//
// 设计取舍：voyage 不直接读硬盘上的 vault——插件把「目录 + 摘要」传上来即可。
// 这样少一个路径配置、少一处越权读文件的口子，也方便以后挪到远程。

const inkpalScoreSystem = `你是笔记评卷人。给学习者的这篇笔记打三项分（每项 0~5 的整数）：
- concept：概念是否准确（有错就低分）
- structure：结构是否清楚（能不能看出层级和因果）
- reviewability：以后能不能靠它复习（有没有例子、结论、边界）

再给最多 3 条advice，每条必须具体到「改哪一句/补什么」，不要写「多复习」「注意细节」这种空话。
summary 一句话说这篇的整体水平。

输出严格 JSON（无围栏）：
{"concept":4,"structure":3,"reviewability":2,"advice":["..."],"summary":"..."}`

const inkpalVaultSystem = `你是学习仓库的体检员。学习者给你一份笔记清单（路径 + 标题 + 摘要），你要找出：
- 内容问题：明显错的说法、说不清的地方
- 结构问题：重复的笔记、孤岛笔记（没有任何链接指向）、该合并的主题
- 该补的双链：哪两篇应该互相链接，为什么

issue 每条要说清「哪个文件 + 什么问题」，suggestion 给一个能立刻做的动作。
links 只给最有价值的，最多 5 条。

输出严格 JSON（无围栏）：
{"summary":"...","issues":[{"path":"...","issue":"...","suggestion":"..."}],"links":[{"from":"...","to":"...","why":"..."}]}`

type noteMeta struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Excerpt string `json:"excerpt"`
}

// Score 一篇笔记的三项分。
type Score struct {
	Concept       int      `json:"concept"`
	Structure     int      `json:"structure"`
	Reviewability int      `json:"reviewability"`
	Advice        []string `json:"advice"`
	Summary       string   `json:"summary"`
}

func (s *Server) registerInkpalVaultHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/inkpal/score", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Content) == "" {
			writeErr(w, 400, "需要 content")
			return
		}
		if !s.hasModel() {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个就能用")
			return
		}
		sc, err := s.scoreNote(r.Context(), req.Path, req.Content)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, sc)
	})

	mux.HandleFunc("/api/inkpal/vault-review", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			Vault string     `json:"vault"`
			Notes []noteMeta `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Notes) == 0 {
			writeErr(w, 400, "需要 notes（笔记清单）")
			return
		}
		if !s.hasModel() {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个就能用")
			return
		}
		out, err := s.reviewVault(r.Context(), req.Vault, req.Notes)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, out)
	})
}

func (s *Server) hasModel() bool {
	return s.Tutor != nil && s.Tutor.Client != nil
}

// scoreNote 让模型给一篇笔记打三项分。
func (s *Server) scoreNote(ctx context.Context, path, content string) (Score, error) {
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, inkpalScoreSystem,
		[]llm.Message{{Role: "user", Content: "笔记：" + path + "\n\n" + content}})
	if err != nil {
		return Score{}, err
	}
	var sc Score
	if err := json.Unmarshal([]byte(raw), &sc); err != nil {
		return Score{}, err
	}
	sc.Concept = clampScore(sc.Concept)
	sc.Structure = clampScore(sc.Structure)
	sc.Reviewability = clampScore(sc.Reviewability)
	if len(sc.Advice) > 3 {
		sc.Advice = sc.Advice[:3]
	}
	return sc, nil
}

// vaultReview 整库体检结果。
type vaultReview struct {
	Summary string `json:"summary"`
	Issues  []struct {
		Path       string `json:"path"`
		Issue      string `json:"issue"`
		Suggestion string `json:"suggestion"`
	} `json:"issues"`
	Links []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Why  string `json:"why"`
	} `json:"links"`
	NoteCount int `json:"noteCount"`
}

// reviewVault 汇总整库问题与建议双链。
func (s *Server) reviewVault(ctx context.Context, vault string, notes []noteMeta) (vaultReview, error) {
	// 清单太长会挤掉推理空间：限 120 篇、每篇摘要限 200 字
	const maxNotes = 120
	if len(notes) > maxNotes {
		notes = notes[:maxNotes]
	}
	var b strings.Builder
	b.WriteString("仓库：" + vault + "\n\n笔记清单：\n")
	for _, n := range notes {
		ex := strings.TrimSpace(n.Excerpt)
		if r := []rune(ex); len(r) > 200 {
			ex = string(r[:200])
		}
		b.WriteString("- " + n.Path + " | " + n.Title + " | " + ex + "\n")
	}
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, inkpalVaultSystem,
		[]llm.Message{{Role: "user", Content: b.String()}})
	if err != nil {
		return vaultReview{}, err
	}
	var out vaultReview
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return vaultReview{}, err
	}
	if len(out.Links) > 5 {
		out.Links = out.Links[:5]
	}
	out.NoteCount = len(notes)
	return out, nil
}

func clampScore(v int) int {
	if v < 0 {
		return 0
	}
	if v > 5 {
		return 5
	}
	return v
}
