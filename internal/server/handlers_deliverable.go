package server

import (
	"net/http"
	"strings"
	"time"

	"voyage/internal/run"
)

// 交付履历（P4）：把「做过什么 + 当时怎么判断的」导出成别人能看懂的东西。
//
// 为什么不是简历：简历写「熟悉 Go」，雇主要看的是「哪件事你判断对了」。
// 这里只导出可核对的记录——每关的目标、验收、产物、自述不确定、事后对账、复盘。

// Deliverable 一次完成的闯关快照。
type Deliverable struct {
	Title      string               `json:"title"`
	Tier       string               `json:"tier"`
	Started    string               `json:"started"`
	FinishedAt string               `json:"finishedAt"`
	Stages     []run.Stage          `json:"stages"`
	Results    map[string]string    `json:"results"`
	Uncertain  map[string]string    `json:"uncertain"`
	Reconcile  map[string]string    `json:"reconcile"`
	Retro      map[string]run.Retro `json:"retro"`
	Hit        float64              `json:"judgementHit"`
}

const deliverableFile = "deliverables.json"

// registerDeliverableHandlers 装配交付履历接口。
func (s *Server) registerDeliverableHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/deliverables", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "只支持 GET")
			return
		}
		list := s.loadDeliverables()
		writeJSON(w, map[string]any{"items": list, "count": len(list)})
	})

	// 导出 markdown：可以直接贴进 GitHub 或发给雇主
	mux.HandleFunc("/api/deliverables/md", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "只支持 GET")
			return
		}
		md := renderDeliverablesMD(s.loadDeliverables())
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		if _, err := w.Write([]byte(md)); err != nil {
			s.httpErr(w, r, err, 500)
		}
	})
}

// saveDeliverable 把一次完成的闯关记进履历（同一 Title+Started 只记一次）。
func (s *Server) saveDeliverable(r *run.Run) {
	if r == nil {
		return
	}
	list := s.loadDeliverables()
	// 同一关的履历要能被后续的对账/复盘更新：先找有没有同一条，有就替换
	idx := -1
	for i, d := range list {
		if d.Title == r.Title && d.Started == r.Started {
			idx = i
			break
		}
	}
	d := Deliverable{
		Title:      r.Title,
		Tier:       run.TierName(r.Tier),
		Started:    r.Started,
		FinishedAt: time.Now().Format(time.RFC3339),
		Stages:     r.Stages,
		Results:    r.Results,
		Uncertain:  r.Uncertain,
		Reconcile:  r.Reconcile,
		Retro:      r.Retro,
		Hit:        r.JudgementHit(),
	}
	if idx >= 0 {
		list[idx] = d
	} else {
		list = append(list, d)
	}
	s.save(deliverableFile, list)
}

// loadDeliverables 读履历（没有就空列表）。
func (s *Server) loadDeliverables() []Deliverable {
	var list []Deliverable
	if !s.load(deliverableFile, &list) {
		return []Deliverable{}
	}
	return list
}

// renderDeliverablesMD 生成 markdown 履历。
func renderDeliverablesMD(list []Deliverable) string {
	var b strings.Builder
	b.WriteString("# 交付履历\n\n")
	b.WriteString("> 由 voyage 生成：每一条都能核对——目标、验收标准、产物、我当时的判断、事后对账。\n\n")
	if len(list) == 0 {
		b.WriteString("（还没有完成的关卡。做完一关并复盘后，这里会自动出现记录。）\n")
		return b.String()
	}
	for _, d := range list {
		b.WriteString("## " + d.Title + "\n\n")
		b.WriteString("- 档位：" + d.Tier + "\n")
		b.WriteString("- 时间：" + d.Started + " → " + d.FinishedAt + "\n")
		if d.Hit >= 0 {
			b.WriteString("- 判断命中率：" + pct(d.Hit) + "（自述不确定的地方，事后真出问题的比例）\n")
		}
		b.WriteString("\n")
		for _, st := range d.Stages {
			out := strings.TrimSpace(d.Results[st.ID])
			if out == "" {
				continue
			}
			b.WriteString("### " + st.Goal + "\n\n")
			b.WriteString("**验收标准**：" + st.Check + "\n\n")
			b.WriteString("**我交的东西**：\n```\n" + clip(out, 1200) + "\n```\n\n")
			if u := strings.TrimSpace(d.Uncertain[st.ID]); u != "" {
				b.WriteString("**我当时不确定**：" + u + "\n\n")
			}
			if rc := d.Reconcile[st.ID]; rc != "" {
				word := "其实没问题"
				if rc == "was-problem" {
					word = "后来真出过问题（判断命中）"
				}
				b.WriteString("**事后对账**：" + word + "\n\n")
			}
			if rt, ok := d.Retro[st.ID]; ok {
				b.WriteString("**复盘**：" + rt.Kind + " —— " + rt.Note + "\n\n")
			}
		}
	}
	b.WriteString("---\n\n生成时间：" + time.Now().Format("2006-01-02 15:04") + "\n")
	return b.String()
}

func pct(v float64) string {
	n := int(v*100 + 0.5)
	return itoa(n) + "%"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…（截断）"
}
