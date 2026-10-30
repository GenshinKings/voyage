# 变更日志

格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号用 [语义化版本](https://semver.org/lang/zh-CN/)。

规则：**每次对外可见的变化都要在这里留一条**，否则迭代几轮之后没人记得为什么这么改。

## [0.1.1] - 2026-10-30

这一版全部围绕一个判断：**「助教会不会教」必须能被证明，而不是靠感觉。**

### 新增

- **教学指标**（`internal/tutor/stats.go`）：独立通过率、错误类型分布、退档后回升率三个数落盘 `stats.json`；`/api/stats` 接口可直接取。此前这三个数一个都没有。
- **结构判定**：判错时不再只回过/不过，而是带 `cause`（错在哪类）、`where`（卡在哪个具体点）、`action`（下一步做的一件小事，不给答案）。`Verdict` 同步增加 `Where`/`Action`。
- **弃学防线**（`internal/server/comeback.go`）：隔 3 天没学，生成一次召回文案——不催、不问原因、只给一个 5 分钟的小目标，只说一次。

### 变更

- **助教对话链路接入 ctx**：`tutor.Send` / `StartOrResume` / `followup` / `judgeStructured` / `runAgent` 全部收 `context.Context`，用户关页面即停止生成。后台例行任务（curate / dungeon / project brief / self-iterate）显式传 `Background`。
- **错误出口统一 JSON**：36 处 `http.Error` 改为 `writeErr`，格式 `{"error": "..."}`；前端 `api()` 同时兼容非 JSON 响应。此前用户看不到任何具体原因，只会看到笼统的「请求失败」。
- **出题取材接入孵化池**：本地没有代码时用孵化池里未读项目兜底（此前抓来的热门项目只沉淀、不参与教学）。

### 修复

- 关联图在面板不可见时抛「画布无尺寸」：改为重试 + 待绘标记，切回标签页补画。
- 关卡验收 `Expect` 由「全部命中」改为「命中任意一个」。

### 已知缺口

- 项目通关（`/api/run/*`）还没有前端界面。
- `Expect` 为空的关卡仍需模型或人工判定。
## [0.1.0] - 2026-10-30

第一个可引用的版本。定位：带着「没人指导的学习者」从零做出一个开源项目。

### 新增

- **项目通关**（`internal/run`）：四档梯度（单文件小工具 → 带存储 → 接外部接口 → 有人真的在用），每档六个关卡，一次只给当前一关，过不了不解锁下一关。全部过关产出可直接贴进简历的「交付证明」。
- **四个接口**：`/api/run/templates`、`/api/run/start`、`/api/run/submit`、`/api/run/state`；进度随 `state.json` 落盘。
- **架构闸门**（`internal/arch`）：三条硬规矩进 `go test`——不许反向依赖 `internal/server`、`server` 里直接操作文件系统只能减不能增（基线 14 次）、不许 dot import。
- **LLM 可取消**：`ChatCtx` / `ChatToolsCtx` / `ChatJSONCtx` + `ChatWith` / `ChatJSONWith` 助手；出题与评卷全链路接入 `r.Context()`，用户关页面后调用即停，不再空烧 token。

### 变更

- **存储锁改为实例级**：`store.StateMutex` / `WriteMutex` 两个包级全局锁收进 `store.Store` 实例，`Server` 持有；删掉无人使用的 `saveMutex`。
- **出题取材支持多语言**：原先只认 `.go`，现在支持 `.py/.ts/.tsx/.vue/.java/.kt/.rs/.c/.cpp/.cs/.rb/.php/.sh/.sql` 等，并自动跳过测试文件。
- **出题取材接上孵化池**：本地没有代码时，用孵化池里未读项目的标题/来源/导读兜底——此前抓取来的热门项目只沉淀、不参与教学。

### 修复

- 出题材料在空目录/只有测试文件时被误判为「有材料」的边界问题。
- 关卡验收的 `Expect` 由「全部命中」改为「命中任意一个」（不同语言的测试输出格式不同）。

### 已知缺口

- 前端尚未接入项目通关。
- `Expect` 为空的关卡仍需模型或人工判定。
- 助教对话链路（`tutor`）尚未接 `r.Context()`。
