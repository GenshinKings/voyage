package server

import (
	"net/http"
	"strconv"
	"strings"

	"voyage/internal/notes"
)

// 笔记索引接口：给「目录知道什么笔记在哪」用，也避免每次全库重读。
//
// 两个端点：
//   GET  /api/notes/index            读当前索引（没有就现建一次并落盘）
//   POST /api/notes/index/rebuild    强制重建（切了仓库、或笔记大改后）
//
// 索引落盘在数据目录 notes-index.json，可随时删，删了下次自动重建。

const noteIndexFile = "notes-index.json"

func (s *Server) registerNoteIndexHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/notes/index", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeErr(w, 405, "只支持 GET")
			return
		}
		idx, ok := s.loadNoteIndex()
		if !ok || idx.Root != s.NotesDir {
			idx = notes.Build(s.NotesDir)
			s.saveNoteIndex(idx)
		}
		writeJSON(w, idx)
	})

	mux.HandleFunc("/api/notes/index/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, 405, "只支持 POST")
			return
		}
		idx := notes.Build(s.NotesDir)
		s.saveNoteIndex(idx)
		writeJSON(w, map[string]any{"ok": true, "root": idx.Root, "count": len(idx.Entries)})
	})
}

// loadNoteIndex 读已落盘的索引。
func (s *Server) loadNoteIndex() (notes.Index, bool) {
	var idx notes.Index
	if !s.load(noteIndexFile, &idx) {
		return notes.Index{}, false
	}
	if idx.ByDir == nil {
		idx.ByDir = map[string][]string{}
	}
	return idx, true
}

// saveNoteIndex 落盘（失败不影响功能，下次重建即可）。
func (s *Server) saveNoteIndex(idx notes.Index) {
	s.save(noteIndexFile, idx)
}

// noteIndexForPrompt 把索引压成一段「目录 → 笔记」清单，给模型当目录用。
func noteIndexForPrompt(idx notes.Index, max int) string {
	if len(idx.Entries) == 0 {
		return ""
	}
	dirs := make([]string, 0, len(idx.ByDir))
	for d := range idx.ByDir {
		dirs = append(dirs, d)
	}
	// 目录排序：根目录在前，其余按名字
	for i := 0; i < len(dirs); i++ {
		for j := i + 1; j < len(dirs); j++ {
			if dirs[j] < dirs[i] {
				dirs[i], dirs[j] = dirs[j], dirs[i]
			}
		}
	}
	var b strings.Builder
	n := 0
	for _, d := range dirs {
		label := d
		if label == "" {
			label = "（根目录）"
		}
		b.WriteString("[" + label + "]\n")
		for _, p := range idx.ByDir[d] {
			if n >= max {
				b.WriteString("…（还有更多，共 " + strconv.Itoa(len(idx.Entries)) + " 篇）\n")
				return b.String()
			}
			b.WriteString("  - " + p + "\n")
			n++
		}
	}
	return b.String()
}
