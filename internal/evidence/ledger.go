// Package evidence — 履历层（宪法 §3.4）：知识点 × 深度 × 时间 × 来源，纯事实陈列。
//
// 深度三层（单向升级，不降级——履历不撤回已达成的事实）：
//   known: 知道（概念通过检索练习/口试）
//   used : 用过（练习/沙盒中实际使用）
//   done : 做过（真实产出物/部署品中作为组件存在）
//
// 履历只陈列事实，不打分不排名（宪法 1.4 负清单：不搞百分比掌握度表演）。
// 导出：JSON（机器）+ HTML（对外证据页雏形，P33 完整版在此迭代）。
package evidence

import (
	"fmt"
	"html"
	"io"
	"sort"
	"time"
)

type Depth int

const (
	Known Depth = iota + 1 // 知道
	Used                   // 用过
	Done                   // 做过
)

func (d Depth) String() string {
	switch d {
	case Known:
		return "知道"
	case Used:
		return "用过"
	case Done:
		return "做过"
	}
	return "?"
}

// Point 一条知识点履历。
type Point struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Depth    Depth     `json:"depth"`               // 当前最高深度
	FirstAt  time.Time `json:"firstAt"`             // 首次学习
	LastAt   time.Time `json:"lastAt"`              // 最近一次深化
	Source   string    `json:"source"`              // 在哪学的（课节/沙盒/产出物 id 或 URL）
	Evidence string    `json:"evidence,omitempty"`  // 证据链接（验收记录/commit/口试录像）
}

// Ledger 履历账本。
type Ledger struct {
	Points map[string]*Point `json:"points"`
}

func New() *Ledger { return &Ledger{Points: map[string]*Point{}} }

// Record 记录一次学习事件：新知识点按深度建档；已存在的只升不降，刷新 LastAt。
// 升级为 Used/Done 时应提供 evidence 链接。
func (l *Ledger) Record(id, title string, depth Depth, at time.Time, source, evidence string) {
	p, ok := l.Points[id]
	if !ok {
		l.Points[id] = &Point{ID: id, Title: title, Depth: depth, FirstAt: at, LastAt: at, Source: source, Evidence: evidence}
		return
	}
	if depth > p.Depth {
		p.Depth = depth
		if evidence != "" {
			p.Evidence = evidence
		}
	}
	if source != "" {
		p.Source = source
	}
	p.LastAt = at
}

// List 全部知识点按深度降序、同级按最近优先。
func (l *Ledger) List() []Point {
	out := make([]Point, 0, len(l.Points))
	for _, p := range l.Points {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth > out[j].Depth
		}
		return out[i].LastAt.After(out[j].LastAt)
	})
	return out
}

// Counts 分层计数（给履历页的克制头部：三个数字，不搞百分比）。
func (l *Ledger) Counts() (known, used, done int) {
	for _, p := range l.Points {
		switch p.Depth {
		case Known:
			known++
		case Used:
			used++
		case Done:
			done++
		}
	}
	return
}

// WriteHTML 对外证据页（极简、纯事实、无装饰——宪法 1.4 禁表演）。
func (l *Ledger) WriteHTML(w io.Writer, owner string, now time.Time) {
	known, used, done := l.Counts()
	fmt.Fprintf(w, `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<title>%s 的学习履历</title>
<style>body{font-family:system-ui,sans-serif;max-width:760px;margin:40px auto;padding:0 20px;color:#1a1a1a;line-height:1.6}
h1{font-size:28px}.counts{color:#555;margin-bottom:24px}.counts b{font-size:22px;color:#101B29}
.item{margin:14px 0;padding:10px 14px;border-left:3px solid #ccc}
.item.d3{border-color:#101B29}.item.d2{border-color:#5a7a99}.item.d1{border-color:#b8c4d0}
.meta{color:#777;font-size:13px}a{color:#2a5885}</style></head><body>
<h1>%s 的学习履历</h1><div class="counts">做过 <b>%d</b> · 用过 <b>%d</b> · 知道 <b>%d</b>　（更新于 %s）</div>
`, html.EscapeString(owner), html.EscapeString(owner), done, used, known, now.Format("2006-01-02"))
	for _, p := range l.List() {
		ev := ""
		if p.Evidence != "" {
			ev = fmt.Sprintf(` · <a href="%s">证据</a>`, html.EscapeString(p.Evidence))
		}
		fmt.Fprintf(w, `<div class="item d%d"><b>%s</b>　<span class="meta">%s · 首学 %s · 最近 %s · 来源 %s%s</span></div>`+"\n",
			int(p.Depth), html.EscapeString(p.Title), p.Depth.String(),
			p.FirstAt.Format("2006-01-02"), p.LastAt.Format("2006-01-02"),
			html.EscapeString(p.Source), ev)
	}
	fmt.Fprint(w, "</body></html>")
}
