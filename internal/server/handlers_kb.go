// handlers_kb.go — 知识与资料路由：notes/reflib/学习者画像/AGENTS 约束（第八轮-P1-2 自 server.go 按域拆出，只搬移不改逻辑）

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"voyage/internal/notes"
	"voyage/internal/profile"
	"voyage/internal/reflib"
)

// registerKBHandlers 装配本域路由（Handlers 调用一次）。
func (s *Server) registerKBHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/notes", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{"list": notes.List(s.NotesDir)}
		if s.FetchConf != nil {
			resp["noteRun"], resp["noteReport"] = s.FetchConf.NoteRun, s.FetchConf.NoteReport
		}
		writeJSON(w, resp)
	})

	mux.HandleFunc("/api/notes/review", func(w http.ResponseWriter, r *http.Request) {
		if err := s.RunNoteReview(); err != nil {
			s.httpErr(w, r, err, 400)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "note": "助教正在巡检笔记，报告稍后出现在这里"})
	})

	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if err := os.WriteFile(s.AgentsPath, []byte(req.Content), 0o644); err != nil {
				s.httpErr(w, r, err, 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		writeJSON(w, map[string]any{"content": s.readAgents()})
	})

	mux.HandleFunc("/api/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if err := profile.Save(s.ProfilePath, req.Content); err != nil {
				s.httpErr(w, r, err, 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return
		}
		writeJSON(w, map[string]any{"content": profile.Load(s.ProfilePath)})
	})

	mux.HandleFunc("/api/reflib", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, reflib.LoadIndex(s.RefDir))
	})

}

// noteContext 笔记巡检的教学上下文（科目/各知识点状态与简介）。
func (s *Server) noteContext() string {
	var b strings.Builder
	if s.Tutor != nil {
		cur := s.Tutor.Curriculum()
		if len(cur.Subjects) > 0 {
			fmt.Fprintf(&b, "- 在学科目：%s\n", strings.Join(cur.Subjects, "、"))
		}
		for _, l := range cur.Lessons {
			tag := "已收尾"
			if l.Status == "active" {
				tag = "进行中"
			}
			fmt.Fprintf(&b, "- 《%s》（%s）：%s\n", l.Title, tag, l.Brief)
		}
	}
	if b.Len() == 0 {
		return "（还没有开课——笔记检查仅按通用学习笔记标准）"
	}
	return b.String()
}

// RunNoteReview 触发一次笔记巡检（异步；与进货共用互斥锁）。结果落 FetchConfig 并主动可见。
// noteReviewCore 笔记巡检同步核（日期已做则跳过）。
func (s *Server) noteReviewCore() error {
	today := time.Now().Format("2006-01-02")
	if s.FetchConf != nil && s.FetchConf.NoteRun == today {
		return nil
	}
	if !s.beginStep() {
		return nil
	}
	defer s.endStep()
	// 仓库可能刚切过：巡检必须跟着 s.NotesDir 走，否则永远读默认 notes/
	s.Tutor.NotesDir = s.NotesDir
	// 先看索引再挑几篇细读：上千篇的库全翻一遍既慢又抓不到重点
	idx := notes.Build(s.NotesDir)
	s.saveNoteIndex(idx)
	picks := make([]notes.File, 0, 12)
	for _, e := range idx.Entries {
		if len(picks) >= 12 {
			break
		}
		picks = append(picks, notes.File{Name: e.Path})
	}
	report, err := s.Tutor.ReviewPicked(s.noteContext(), picks, noteIndexForPrompt(idx, 60))
	if s.FetchConf != nil {
		s.FetchConf.NoteRun = today
		if err != nil {
			s.FetchConf.NoteReport = "巡检失败: " + err.Error()
		} else {
			s.FetchConf.NoteReport = report
		}
	}
	s.saveFetchConf()
	return err
}

// RunNoteReview 触发一次笔记巡检（异步）。
func (s *Server) RunNoteReview() error {
	if s.Tutor == nil {
		return fmt.Errorf("LLM 未配置")
	}
	go s.noteReviewCore()
	return nil
}

func (s *Server) readAgents() string {
	b, err := os.ReadFile(s.AgentsPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// defaultAgentsMD 首启写入的用户可编辑约束模板（每次 LLM 调用热读，改完即生效）。
const defaultAgentsMD = `# AGENTS.md — AI 助教行为约束

> 本文件在**每次** AI 调用时注入，改完保存即生效（无需重启）。
> 与系统默认纪律冲突时，以本文件为准。删掉本文件则回到默认纪律。

## 教学边界
- 只教当前知识点课上讲过的内容；对话没教到的概念不出题、不默认学习者会。
- 每条回复 ≤250 字，永远以让学习者动一次脑/动一次手收尾。
- 语气温和不评判，不用考试腔，多用生活化类比。

## 收尾规则
- [PASS] 与 [PASS:partial] 必须单独成行。
- 加深追问必须单独成条并等学习者回答；不在同一条消息里既提问又收尾。
- 收尾自查：还有没讲到也没被问到的要点 → 先输出【还有没讲的：…要继续学吗？】。

## 出题与批改
- 只出实操题（动手算 / 写代码 / 场景决策），不出背诵默写题。
- 语境式：旧概念在新题路径中被自然调用，不在题面重新讲解。
- 批改先说对错再给一步提示，不直接报答案。

## 我的自定义约束
（在下面添加你自己的要求，例如：讲解尽量引用我的行业背景；少用数学符号；全程中文……）
`
