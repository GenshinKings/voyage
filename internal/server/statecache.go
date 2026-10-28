package server

// statecache.go — 小穗四轮·性能1+3：
// ① 序列化缓存：state 变更置 dirty（bytes 失效），请求只在 dirty 时重新 marshal；
//    响应带 ETag，客户端 If-None-Match 命中且缓存新鲜即 304。
// ② 落盘去抖：saveStateSoon 把窗口内多次变更合并成一次真实 IO（300ms）；
//    （原「关键路径立即刷」的 saveStateNow 已删——第九轮-P1-1：全仓零调用点，
//     关键路径走 saveState 后由 flushDelay 合并落盘，磁盘安全由 .tmp+rename 保证。）
// 磁盘安全不降级：save() 仍是 .tmp+rename 原子替换+.bak（五轮①），断电永不截断。
//
// 第九轮-P0-3：缓存从包级全局改成 Server 实例字段——
// 全局缓存会让多个实例（主要是测试里的临时 Server）互相污染 ETag 与 dirty 状态，
// 且旧实例的定时器会在新测试的临时目录被清掉后才落盘，Windows 上偶发
// 「TempDir RemoveAll cleanup: The directory is not empty」。实例化 + 目录存在性守卫一并解决。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"
)

const flushDelay = 300 * time.Millisecond

// stateCacheT 每个 Server 一份：缓存字节、ETag、挂起的合并落盘。
type stateCacheT struct {
	mu    sync.Mutex
	dirty bool
	timer *time.Timer
	bytes []byte
	etag  string
}

// saveStateSoon 全部调用点的默认入口（性能1+3：置脏 + 300ms 合并落盘）。
func (s *Server) saveStateSoon() { s.markStateDirty() }

func (s *Server) markStateDirty() {
	c := &s.sc
	c.mu.Lock()
	c.dirty = true
	c.bytes = nil // 缓存失效
	c.etag = ""
	if c.timer == nil {
		c.timer = time.AfterFunc(flushDelay, func() { s.flushState() })
	}
	c.mu.Unlock()
}

func (s *Server) flushState() {
	c := &s.sc
	c.mu.Lock()
	d := c.dirty
	c.timer = nil
	c.mu.Unlock()
	if !d {
		return
	}
	// 目录已经不在（测试收尾删了临时目录、或用户手动移走）：放弃这次落盘，
	// 不要让残留定时器往已删除的目录写文件。
	if _, err := os.Stat(s.Dir); err != nil {
		c.mu.Lock()
		c.dirty = false
		c.mu.Unlock()
		return
	}
	stateMutex.Lock()
	s.save("state.json", s)
	c.mu.Lock()
	still := c.dirty // flush 期间又被 mark（新变更不丢，补一轮）
	c.dirty = false
	if still {
		c.timer = time.AfterFunc(flushDelay, func() { s.flushState() })
	}
	c.mu.Unlock()
	stateMutex.Unlock()
}

// Close 停掉挂起的落盘并立即刷一次。测试与优雅退出用。
func (s *Server) Close() {
	c := &s.sc
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	s.flushState()
}

// serveStateCache 用缓存字节应答 /api/state：dirty 时重建序列化，带 ETag/If-None-Match→304。
func (s *Server) serveStateCache(w http.ResponseWriter, r *http.Request, build func() (any, error)) {
	c := &s.sc
	c.mu.Lock()
	if c.bytes == nil { // dirty 或首次：与内存重建一致
		c.mu.Unlock()
		val, err := build()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		bb, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		sum := sha1.Sum(bb)
		etagB := `"` + hex.EncodeToString(sum[:]) + `"`
		c.mu.Lock()
		c.bytes = bb
		c.etag = etagB
	}
	b := c.bytes
	etag := c.etag
	c.mu.Unlock()

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}
