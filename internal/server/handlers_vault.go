package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"voyage/internal/notes"
)

// 笔记仓库（Obsidian vault）对接。
//
// 问题：voyage 的笔记工作区固定在数据目录的 notes/，而学习者的笔记在 Obsidian 库里，
// 于是「笔记工作区」永远显示 0 篇、巡检永远说「还没有笔记」。
//
// 取舍：只读挂载——把 NotesDir 指到使用者选的仓库，但不写、不删、不改名。
// 配置落在 vault.json（不含任何凭据），换仓库只要改一个路径。

// vaultConf 笔记仓库配置。
type vaultConf struct {
	Path string `json:"path"` // 绝对路径；空表示用默认 notes/
}

// registerVaultHandlers 装配「笔记仓库」设置接口。
func (s *Server) registerVaultHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/vault", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{
				"path":      s.VaultPath(),
				"notes":     len(notes.List(s.NotesDir)),
				"defaultOf": s.defaultNotesDir,
			})
		case http.MethodPost:
			var req struct {
				Path string `json:"path"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeErr(w, 400, "请求格式不对")
				return
			}
			p := strings.TrimSpace(req.Path)
			if p != "" {
				abs, err := filepath.Abs(p)
				if err != nil {
					writeErr(w, 400, "路径不对："+err.Error())
					return
				}
				if !notes.ValidDir(abs) {
					writeErr(w, 400, "这个目录不存在："+abs)
					return
				}
				p = abs
			}
			s.setVaultPath(p)
			count := len(notes.List(s.NotesDir))
			writeJSON(w, map[string]any{"ok": true, "path": s.VaultPath(), "notes": count})
		default:
			writeErr(w, 405, "只支持 GET / POST")
		}
	})
}

// VaultPath 当前生效的仓库路径（空串表示用默认 notes/）。
func (s *Server) VaultPath() string {
	s.st.State.Lock()
	defer s.st.State.Unlock()
	return s.vaultPath
}

// setVaultPath 切换仓库并落盘；空路径回退到默认 notes/。
func (s *Server) setVaultPath(p string) {
	s.st.State.Lock()
	s.vaultPath = p
	defaultDir := s.defaultNotesDir
	tutor := s.Tutor
	s.st.State.Unlock()

	target := p
	if target == "" {
		target = defaultDir
	}
	s.NotesDir = target
	if tutor != nil {
		tutor.NotesDir = target // 巡检读的是 Tutor 的字段，这里要同步，否则改了仓库巡检还看旧目录
	}
	s.save(vaultFile, vaultConf{Path: p})
}

const vaultFile = "vault.json"

// loadVaultConf 启动时恢复仓库设置。
func (s *Server) loadVaultConf() {
	var conf vaultConf
	if !s.load(vaultFile, &conf) {
		return
	}
	if conf.Path == "" {
		return
	}
	if !notes.ValidDir(conf.Path) {
		return // 仓库被挪走/删掉：静默退回默认目录，不让启动失败
	}
	s.vaultPath = conf.Path
	s.NotesDir = conf.Path
	if s.Tutor != nil {
		s.Tutor.NotesDir = conf.Path
	}
}
