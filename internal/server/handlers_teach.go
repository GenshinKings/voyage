package server

import (
	"net/http"

	"voyage/internal/teach"
)

// registerTeachHandlers 教学技能（Agent Skills 规范）的只读接口。
//
// 渐进披露：/api/skills 只回元信息（name+description），正文要用时再按名字取，
// 免得把五份教学法全塞进前端或一次塞进模型上下文。
func (s *Server) registerTeachHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/skills", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			metas, err := teach.Metas()
			if err != nil {
				writeErr(w, 500, "技能包读取失败")
				return
			}
			writeJSON(w, map[string]any{"skills": metas})
			return
		}
		sk, ok, err := teach.Get(name)
		if err != nil {
			writeErr(w, 500, "技能包读取失败")
			return
		}
		if !ok {
			writeErr(w, 404, "没有这个技能")
			return
		}
		writeJSON(w, map[string]any{"skill": sk})
	})
}
