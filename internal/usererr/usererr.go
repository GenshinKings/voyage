// Package usererr — 第九轮-P0-1：错误分两层。
//
// User：短、人话、带下一步——允许回到界面。
// Detail：原始报错、路径、HTTP 码、响应片段——只进日志与 /api/diagnostics，不返回给前端。
// 孩子看到「outpost2-data/llm.json」只会更慌，也不会因此修好任何东西。
package usererr

import "errors"

// E 两层错误：Error() 返回用户文案，Detail() 返回技术细节。
type E struct {
	user    string
	detail  string
	wrapped error
}

func (e *E) Error() string  { return e.user }
func (e *E) Detail() string { return e.detail }

// Unwrap 保持包装链可见（errors.Is/As 能穿透到内层错误）。
func (e *E) Unwrap() error { return e.wrapped }

// New 构造两层错误。user 必须是能对孩子说的话；detail 放技术现场。
func New(user, detail string) *E { return &E{user: user, detail: detail, wrapped: errors.New(detail)} }

// DetailOf 取技术细节：包装链里有 *E → 它的 detail；普通错误 → err.Error() 本身
// （调用方照常记日志，行为与分层改造前一致）。
func DetailOf(err error) (string, bool) {
	var e *E
	if errors.As(err, &e) {
		return e.detail, true
	}
	return err.Error(), false
}
