// diagnostics.go — 第九轮-P0-1：错误两层的出口侧。
//
// handler 出错时：用户拿到人话（err.Error() 或 usererr 的 User 层），
// 技术细节（路径/HTTP 码/响应片段）只进日志 + 本诊断环，经 /api/diagnostics 可查。
package server

import (
	"log"
	"net/http"
	"sync"
	"time"

	"voyage/internal/usererr"
)

// apiDiag 一次接口错误的技术现场（前端永不直接看到 Detail）。
type apiDiag struct {
	At     time.Time `json:"at"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Code   int       `json:"code"`
	User   string    `json:"user"`
	Detail string    `json:"detail"`
}

var (
	apiDiagMu    sync.Mutex
	apiDiagRing  []apiDiag // 进程内环形：只留最近 50 条
	apiDiagNoted bool      // 防重入：诊断端点自己的错误不再入环
)

// httpErr handler 的统一错误出口：用户拿人话，细节进日志与诊断环。
func (s *Server) httpErr(w http.ResponseWriter, r *http.Request, err error, code int) {
	user := err.Error()
	detail, layered := usererr.DetailOf(err)
	log.Printf("[api] %s %s → %d user=%q detail=%s (分层=%v)", r.Method, r.URL.Path, code, user, detail, layered)
	apiDiagMu.Lock()
	apiDiagRing = append(apiDiagRing, apiDiag{At: time.Now(), Method: r.Method, Path: r.URL.Path,
		Code: code, User: user, Detail: detail})
	if len(apiDiagRing) > 50 {
		apiDiagRing = apiDiagRing[len(apiDiagRing)-50:]
	}
	apiDiagMu.Unlock()
	writeErr(w, code, user)
}

// diagSnapshot 诊断环快照（/api/diagnostics 用）。
func diagSnapshot() []apiDiag {
	apiDiagMu.Lock()
	defer apiDiagMu.Unlock()
	out := make([]apiDiag, len(apiDiagRing))
	copy(out, apiDiagRing)
	return out
}

// llmMissingErr 「模型没接上」的两层错误（多个 handler 共用；细节含数据目录路径，只进日志）。
func llmMissingErr() error {
	return usererr.New("模型还没接上——去设置页配置一个就能用", "LLM 未配置（outpost2-data/llm.json）")
}
