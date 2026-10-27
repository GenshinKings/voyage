package server

// statecache.go — 小穗四轮·性能1+3：
// ① 序列化缓存：state 变更置 dirty（bytes 失效），请求只在 dirty 时重新 marshal；
//    响应带 ETag，客户端 If-None-Match 命中且缓存新鲜即 304。
// ② 落盘去抖：saveStateSoon 把窗口内多次变更合并成一次真实 IO（300ms）；
//    关键路径（verdict/trophy/pause/budget/export/resume）用 saveStateNow 立即刷。
// 磁盘安全不降级：save() 仍是 .tmp+rename 原子替换+.bak（五轮①），断电永不截断。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const flushDelay = 300 * time.Millisecond

var stateCacheLock sync.Mutex

var stateCache struct {
	dirty bool        // 内存改过、尚未（或还在延迟）落盘
	timer *time.Timer // pending 合并落盘
	bytes []byte      // 最近一次完整序列化（与内存一致）
	etag  string
}

// saveStateSoon 全部调用点的默认入口（性能1+3：置脏 + 300ms 合并落盘）。
func (s *Server) saveStateSoon() { s.markStateDirty() }

func (s *Server) markStateDirty() {
	stateCacheLock.Lock()
	stateCache.dirty = true
	stateCache.bytes = nil // 缓存失效
	stateCache.etag = ""
	if stateCache.timer == nil {
		stateCache.timer = time.AfterFunc(flushDelay, func() { s.flushState() })
	}
	stateCacheLock.Unlock()
}

func (s *Server) flushState() {
	stateCacheLock.Lock()
	d := stateCache.dirty
	stateCache.timer = nil
	stateCacheLock.Unlock()
	if !d {
		return
	}
	stateMutex.Lock()
	s.save("state.json", s)
	stateCacheLock.Lock()
	still := stateCache.dirty // flush 期间又被 mark（新变更不丢，补一轮）
	stateCache.dirty = false
	if still {
		stateCache.timer = time.AfterFunc(flushDelay, func() { s.flushState() })
	}
	stateCacheLock.Unlock()
	stateMutex.Unlock()
}

// serveStateCache 用缓存字节应答 /api/state：dirty 时重建序列化， witnessing ETag/If-None-Match→304。
func (s *Server) serveStateCache(w http.ResponseWriter, r *http.Request, build func() (any, error)) {
	stateCacheLock.Lock()
	if stateCache.bytes == nil { // dirty 或首次：与内存重建一致
		stateCacheLock.Unlock()
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
		stateCacheLock.Lock()
		stateCache.bytes = bb
		stateCache.etag = etagB
	}
	b := stateCache.bytes
	etag := stateCache.etag
	stateCacheLock.Unlock()

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}
