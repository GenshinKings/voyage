package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"voyage/internal/llm"
	"voyage/internal/profile"
	"voyage/internal/teach"
)

// InkPal 桥接：让 Obsidian 插件和 voyage 共用同一个助教。
//
// 为什么在 voyage 侧做：插件自己带一套模型调用（engine/llm.ts）就会各用各的脑子——
// 记忆、学习者画像、教学法全分裂。这里把「大脑」留在 voyage，插件只负责读文件、
// 显示内容、调这两个接口。
//
// 两个接口：
//
//	POST /api/inkpal/chat      {"messages":[{role,content}...]}  → {"reply": "..."}
//	POST /api/inkpal/annotate  {"path":"...","content":"..."}    → {"items":[...]}
const inkpalChatSystem = `你是学习者的助教，正在通过 Obsidian 插件和他对话。回答要短、要具体：
- 优先引用他自己的笔记内容，不要泛泛而谈
- 不确定就说不确定，不要编
- 一次只推进一步，不要一口气讲完`

const inkpalAnnotateSystem = `你是笔记批注员。学习者给你一篇自己写的笔记，你要找出里面「理解错了」「说不清」「漏了前提」的地方，逐条标注。

规则：
- 只标真的有问题的地方，不要为了凑数而标；没问题的笔记就返回空数组
- 每条必须能定位到原文：line 用笔记里的行号（从 1 开始），quote 抄那一行的原文片段（不超过 40 字）
- kind 只能是：concept（概念错）、unclear（说不清）、missing（漏前提）、ok（没问题但值得记下来）
- 必须写 myUnderstanding：把学习者原来的说法按他的意思复述一遍，让未来的他能看到「当时我是这么想的」
- 必须写 correct：一句话说清正确说法，不展开讲
- 不修改原文，只给批注

输出严格 JSON（无围栏）：{"items":[{"line":3,"quote":"...","kind":"concept","myUnderstanding":"...","correct":"..."}]}`

// registerInkpalHandlers 装配 InkPal 桥接路由。
func (s *Server) registerInkpalHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/inkpal/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Messages) == 0 {
			writeErr(w, 400, "需要 messages")
			return
		}
		if s.Tutor == nil || s.Tutor.Client == nil {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个就能用")
			return
		}
		msgs := make([]llm.Message, 0, len(req.Messages))
		for _, m := range req.Messages {
			role := m.Role
			if role != "assistant" {
				role = "user"
			}
			msgs = append(msgs, llm.Message{Role: role, Content: m.Content})
		}
		reply, err := llm.ChatWith(r.Context(), s.Tutor.Client, s.inkpalSystem(), msgs)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, map[string]any{"reply": reply, "sharedTutor": true})
	})

	mux.HandleFunc("/api/inkpal/annotate", func(w http.ResponseWriter, r *http.Request) {
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
		if s.Tutor == nil || s.Tutor.Client == nil {
			writeErr(w, 400, "模型还没接上——去 voyage 设置页配置一个就能用")
			return
		}
		items, err := s.annotateNote(r.Context(), req.Path, req.Content)
		if err != nil {
			s.httpErr(w, r, err, 502)
			return
		}
		writeJSON(w, map[string]any{"path": req.Path, "items": items, "sharedTutor": true})
	})
}

// inkpalSystem 把教学法技能与学习者画像拼进 system，保证插件和 voyage 同一个脑子。
func (s *Server) inkpalSystem() string {
	var b strings.Builder
	b.WriteString(inkpalChatSystem)
	if skills, err := teach.All(); err == nil {
		b.WriteString("\n\n【教学纪律（与 voyage 共用）】\n")
		for _, sk := range skills {
			// 只拼每条的标题句，正文太长会挤掉对话上下文
			if first := firstParagraph(sk.Body); first != "" {
				b.WriteString("- " + sk.Name + "：" + first + "\n")
			}
		}
	}
	if p := strings.TrimSpace(s.readProfile()); p != "" {
		b.WriteString("\n【学习者画像】\n" + p)
	}
	return b.String()
}

// firstParagraph 取正文第一段非标题行。
func firstParagraph(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") || strings.HasPrefix(line, "```") {
			continue
		}
		return line
	}
	return ""
}

// Annotation 一条行级批注（与插件约定的形状）。
type Annotation struct {
	Line            int    `json:"line"`
	Quote           string `json:"quote"`
	Kind            string `json:"kind"`
	MyUnderstanding string `json:"myUnderstanding"`
	Correct         string `json:"correct"`
}

// annotateNote 调模型给笔记做行级批注。
func (s *Server) annotateNote(ctx context.Context, path, content string) ([]Annotation, error) {
	user := "笔记路径：" + path + "\n\n笔记正文：\n" + content
	raw, err := llm.ChatJSONWith(ctx, s.Tutor.Client, inkpalAnnotateSystem,
		[]llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []Annotation `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	items := make([]Annotation, 0, len(out.Items))
	for _, it := range out.Items {
		if it.Line <= 0 || strings.TrimSpace(it.Correct) == "" {
			continue // 定位不到或没给正确说法的，丢掉，别让插件显示半截批注
		}
		if it.Kind == "" {
			it.Kind = "unclear"
		}
		items = append(items, it)
	}
	return items, nil
}

// readProfile 读学习者画像（缺失时返回空串，不阻塞）。
// 走 profile 包而不是自己 os.ReadFile：server 的直接文件操作有预算闸门，
// 存储类读取要收口到已有包（internal/arch 会盯着这件事）。
func (s *Server) readProfile() string {
	if s.ProfilePath == "" {
		return ""
	}
	return profile.Load(s.ProfilePath)
}
