# 变更日志

格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号用 [语义化版本](https://semver.org/lang/zh-CN/)。

规则：**每次对外可见的变化都要在这里留一条**，否则迭代几轮之后没人记得为什么这么改。

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
