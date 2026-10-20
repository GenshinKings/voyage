# AGENTS.md — 前哨 2.0 仓库路由表

> 本文件只做路由，不写规则。干什么活之前，先翻对应文档（Magpie 模式）。

## 改代码之前

- **动任何数据文件（state.json / ledger.json / readings.json / lessons.json）** → 先读 `internal/server/server.go` 头部注释与 `TestLedgerSurvivesRestart`。铁律：**每类数据单一真相源**——ledger=ledger.json、阅读内容=readings.json、其余=state.json；双写副本=重启丢数据（事故 2026-10-20）。
- **改教学/出题/批改的提示词** → 先读 `internal/tutor/tutor.go` 的 teachSystem 与 `internal/exercise/exercise.go`。铁律：收尾标记单独成行；加深追问必须单独成条等回答；练习只考课上讲过的内容。
- **改调度/策略（bandit/Elo/bias）** → 先读 `学习知识库/20261020-前哨个人自适应策略v1设计.md`（在仓库上级目录）。铁律：RL 管决策 LLM 管生成；全部策略参数落 state.json 可审计。
- **改前端 index.html** → 先读本文件「前端纪律」。

## 前端纪律（LESSONS.md 的硬规则转正区）

1. 禁止对 index.html 做连续多次小补丁——结构改动一律全量重写（增量补丁互相覆盖污染，事故 c49e470）。
2. 禁止用 requestAnimationFrame 做必须执行的绘制/逻辑——后台页会被节流静默跳过；用同步执行+setTimeout 布局兜底（事故 88f03fb）。
3. 任何渲染失败必须用户可见（toast/文字），禁止裸 catch 吞掉（事故 88f03fb：力导向 NaN 静默白屏）。
4. 复杂数值计算（物理模拟/插值）必须数学上证明有界（位移 clamp/退火），不接受「调参数试试」。
5. bash heredoc 禁止携带中文与复杂转义——写文件一律用 Write 工具（多次命中）。

## 数据纪律

1. 履历（ledger.json）是核心资产：Record 即刻落盘；启动显式恢复；改动必须有跨重启回归测试（TestLedgerSurvivesRestart）。
2. 新增持久化数据先回答：单一真相源是哪个文件？谁覆盖谁？重启/并发时谁赢？回答不了就不许写。

## 测试纪律

1. 修过的 bug 必须留回归测试（例：TestLedgerSurvivesRestart 之于履历丢失）。
2. 回退测试：把旧代码放回去，新测试必须真挂——挂不了说明测试没测到。
3. 没跑的验证诚实标注 Not run，禁止用「应该没问题」替代执行证据。

## 晋升通道（Magpie 模式）

教训记入 `LESSONS.md`（一句规则+原因+commit 证据+Seen N×）。**同一条教训在 3 个不同工作日的记录中出现 → 转正为本文件的硬规则条目**，LESSONS.md 中该条归档。人（用户）保留否决权。
