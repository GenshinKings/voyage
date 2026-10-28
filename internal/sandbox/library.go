// library.go — 内置教学案例库（AI 造坑 v0：手工精修模板；后续由 AI 按薄弱域定向生成，闸门同此）。
//
// 每个案例的铁律：bug 教学性明确（修法唯一且合理）、验收确定性（Check 只认修复结果）、
// 不泄露 bug 位置（Brief 只讲任务不讲错因）。
package sandbox

import (
	"strings"
)

// Library 内置案例库（key=案例 id）。
func Library() map[string]*BreakCase {
	return map[string]*BreakCase{
		"bc-mcp-typo":    caseMCPTypo(),
		"bc-cache-bound": caseCacheBoundary(),
		"bc-queue-logic": caseQueueLogic(),
	}
}

// ---- 案例 1：MCP 工具注册表的标识符错（typo，知道级） ----

func caseMCPTypo() *BreakCase {
	broken := `package main

// MCP 工具注册表：名字 → 处理器。
// 预期：注册两个工具，列出时按名字排序输出。

import (
	"fmt"
	"sort"
)

type Registry struct {
	handlers map[string]func(string) string
}

func NewRegistry() *Registry {
	return &Registry{handlers: map[string]func(string) string{}}
}

func (r *Registry) Register(name string, fn func(string) string) {
	r.handlers[name] = fn
}

func (r *Registry) List() []string {
	names := []string{}
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}


func main() {
	r := NewRegistry()
	r.Register("echo", func(s string) string { return s })
	r.Register("upper", strings.ToUpperShim)
	fmt.Println(r.List())
}
`
	fixed := strings.ReplaceAll(broken, "appand", "append")
	fixed = strings.Replace(fixed, "strings.ToUpperShim", `func(s string) string { return strings.ToUpper(s) }`, 1)
	return &BreakCase{
		ID: "bc-mcp-typo", Title: "修好 MCP 工具注册表", Domain: "mcp", Kind: BugTypo,
		Brief: "这段 MCP 工具注册表代码编译不过、逻辑也没跑通。任务：让它编译通过且 List() 按「大写转换、回声」的顺序输出两个工具名。改完保存，跑验收。",
		Files: map[string]string{"main.go": broken},
		Check: func(files map[string]string) CheckResult {
			src := files["main.go"]
			switch {
			case strings.Contains(src, "appand"):
				return CheckResult{Passed: false, Detail: "仍存在未定义标识符 appand（提示：标准库的追加函数叫什么？）"}
			case strings.Contains(src, "ToUpperShim"):
				return CheckResult{Passed: false, Detail: "strings.ToUpperShim 不存在——upper 工具应直接用 strings.ToUpper 包装"}
			case !strings.Contains(src, "strings.ToUpper"):
				return CheckResult{Passed: false, Detail: "upper 工具应使用 strings.ToUpper"}
			case !strings.Contains(src, "sort.Strings"):
				return CheckResult{Passed: false, Detail: "List() 应保持排序输出"}
			default:
				return CheckResult{Passed: true, Detail: "编译问题清零，注册表按序输出 ✓"}
			}
		},
		KnowledgePoint: "kp-go-stdlib",
	}
}

// ---- 案例 2：缓存 LRU 的边界错（boundary，用过级） ----

func caseCacheBoundary() *BreakCase {
	broken := `package main

// 定容缓存：超过容量时逐出最旧的键。
// 预期：容量 2 时，put a、put b、put c 后 get a 应未命中（a 被逐出）。

import "fmt"

type Cache struct {
	capacity int
	order    []string // 插入序
	data     map[string]string
}

func NewCache(capacity int) *Cache {
	return &Cache{capacity: capacity, data: map[string]string{}}
}

func (c *Cache) Put(k, v string) {
	if _, exists := c.data[k]; !exists {
		c.order = append(c.order, k)
	}
	c.data[k] = v
	// 逐出超容量的最旧条目
	for len(c.order) > c.capacity {
		delete(c.data, c.order[0])
		c.order = c.order[:len(c.order)]
	}
}

func (c *Cache) Get(k string) (string, bool) {
	v, ok := c.data[k]
	return v, ok
}

func main() {
	c := NewCache(2)
	c.Put("a", "1")
	c.Put("b", "2")
	c.Put("c", "3")
	v, ok := c.Get("a")
	fmt.Println("get a:", v, ok) // 预期: "" false（a 已被逐出）
	v, ok = c.Get("c")
	fmt.Println("get c:", v, ok) // 预期: "3" true
}
`
	return &BreakCase{
		ID: "bc-cache-bound", Title: "缓存逐出的 off-by-one", Domain: "boundary", Kind: BugBoundary,
		Brief: "这个定容缓存的逐出逻辑有边界 bug：容量 2 时放进第三个键，最旧的没有被逐出（或者逐出了不该逐出的）。任务：修到注释里两条预期都成立。",
		Files: map[string]string{"main.go": broken},
		Check: func(files map[string]string) CheckResult {
			src := files["main.go"]
			if strings.Contains(src, "c.order[:len(c.order)]") {
				return CheckResult{Passed: false, Detail: "逐出切片了空尾巴——被删的键是 order[0]，切片长度也应减一"}
			}
			if !strings.Contains(src, "c.order[:len(c.order)-1]") && !strings.Contains(src, "c.order[1:]") {
				return CheckResult{Passed: false, Detail: "逐出后应同步收缩 order 切片"}
			}
			if !strings.Contains(src, `delete(c.data, c.order[0])`) {
				return CheckResult{Passed: false, Detail: "应删除最旧键 order[0] 对应的数据"}
			}
			return CheckResult{Passed: true, Detail: "边界修复 ✓（容量 2 时第三键正确逐出最旧）"}
		},
		KnowledgePoint: "kp-boundary-lru",
	}
}

// ---- 案例 3：任务队列的逻辑反转（logic，用过级） ----

func caseQueueLogic() *BreakCase {
	broken := `package main

// 任务队列：只派发给「空闲」的 worker；没有空闲 worker 时任务排队等待。
// 预期：两个 worker 都忙时，新任务进入 pending 而不是被派发。

import "fmt"

type Worker struct {
	ID   string
	Busy bool
}

type Queue struct {
	Workers   []Worker
	Pending   []string
}

func (q *Queue) Submit(task string) {
	for i := range q.Workers {
		if q.Workers[i].Busy { // 找一个忙的 worker 派发
			q.Workers[i].Busy = false
			fmt.Printf("task %s -> worker %s\n", task, q.Workers[i].ID)
			return
		}
	}
	q.Pending = append(q.Pending, task)
}

func main() {
	q := Queue{Workers: []Worker{{"w1", true}, {"w2", true}}}
	q.Submit("t1") // 两个 worker 都忙，t1 应进入 pending
	fmt.Println("pending:", len(q.Pending))
}
`
	return &BreakCase{
		ID: "bc-queue-logic", Title: "派发条件的逻辑反转", Domain: "logic", Kind: BugLogic,
		Brief: "这个任务队列的派发条件写反了：它把任务派给了「忙」的 worker，还顺手把人家置空。任务：改成只派给空闲 worker，全忙时任务进 pending。",
		Files: map[string]string{"main.go": broken},
		Check: func(files map[string]string) CheckResult {
			src := files["main.go"]
			switch {
			case strings.Contains(src, "if q.Workers[i].Busy {"):
				return CheckResult{Passed: false, Detail: "派发条件仍是「忙」——应该找的是空闲（!Busy）的 worker"}
			case !strings.Contains(src, "!q.Workers[i].Busy"):
				return CheckResult{Passed: false, Detail: "派发条件应为 !Busy"}
			case strings.Contains(src, "Busy = false"):
				return CheckResult{Passed: false, Detail: "派发后 worker 应置为忙（Busy = true），不是置空"}
			default:
				return CheckResult{Passed: true, Detail: "逻辑修复 ✓（空闲派发，全忙入队）"}
			}
		},
		KnowledgePoint: "kp-condition-logic",
	}
}
