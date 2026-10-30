package server

import (
	"fmt"
	"strings"
)

// exerciseMaterial 决定「这次出题能拿什么真实材料」。
//
// 架构体检 2026-10-30：抓取（ghfetch）抓来的热门项目只沉淀进孵化池 Readings，
// 而出题取材只读本地沙箱目录——两条数据流从没接上，于是「从真实项目出发教学」
// 在出题这一步永远拿不到东西。这里做兜底：
//  1. 本地目录有代码 → 用它（最贴近学习者本人）
//  2. 否则孵化池里有项目 → 用项目名/简介/导读当材料
//  3. 都没有 → 空串，出题退回通用例子
func (s *Server) exerciseMaterial() string {
	if m := learnerMaterial(s.SandboxDir); m != "" {
		return m
	}
	return poolMaterial(s.Readings, 3)
}

// poolMaterial 把孵化池里未读的项目压成一段出题材料。
func poolMaterial(rs []Reading, limit int) string {
	if limit <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range rs {
		if r.Done || strings.TrimSpace(r.Title) == "" {
			continue
		}
		b.WriteString("◆ " + r.Title + "\n")
		if r.URL != "" {
			b.WriteString("  来源：" + r.URL + "\n")
		}
		if br := strings.TrimSpace(r.Brief); br != "" {
			b.WriteString("  导读：" + clipLines(br, 6) + "\n")
		} else if c := strings.TrimSpace(r.Content); c != "" {
			b.WriteString("  摘要：" + clipLines(c, 6) + "\n")
		}
		n++
		if n >= limit {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		return ""
	}
	return out + fmt.Sprintf("\n（取材自动生成：你孵化池里 %d 个还没读的项目；出题请引用它们的真实场景，不要空谈概念）", n)
}

// clipLines 取前 n 行，压掉多余空行——材料要短，题面才有空间。
func clipLines(s string, n int) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, n)
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		out = append(out, ln)
		if len(out) >= n {
			break
		}
	}
	return strings.Join(out, " / ")
}
