# LESSONS.md — 前哨 2.0 错题本

> 记录格式：一句规则 | 原因 | 证据（commit/文件）| Seen N×。
> 同一条教训在 3 个不同工作日出现 → 转正进 AGENTS.md 硬规则区（见晋升通道）。
> 只记「修完又返工/不该合/验证太糙/静默失败」类教训；一次性笔误不入。

## 2026-10-20

1. 数据必须单一真相源 | Ledger 双写不一致：Record 只落 ledger.json，state.json 副本从未更新，重启加载只信 state.json → 用户履历被吞 | commit 32886bc + TestLedgerSurvivesRestart | Seen 1×
2. JSON 文件顶层形状必须与加载目标一致 | readings.json 写成 {"readings":[...]} 而加载目标是数组 → 静默加载失败，字段为 nil | commit 50ece24 | Seen 1×
3. 同一文件禁止连续增量补丁 | index.html 五六个补丁互相覆盖污染，关联图问题查了一小时，最终全量重写一次成型 | commit c49e470 | Seen 1×
4. rAF 不可依赖 | 后台页/iframe 页 requestAnimationFrame 被节流暂停，绘制闭包静默不执行；改 await rAF 更是永久卡死 | commit 88f03fb | Seen 1×
5. 物理模拟必须有界 | 力导向逐对直接位移无上限，第 1 轮迭代坐标冲到 -5.5e6、第 2 轮 NaN，fill(NaN) 静默不画；改标准 FR（合力累加+退火位移 clamp）数学保证收敛 | commit 88f03fb | Seen 1×
6. 双端语义抄形不抄义会翻车 | 抄 DeepSeek 侧栏抄成课程排期（欠账清单）而非会话历史（存货），违背宪法案例零；被用户两次纠偏（c49e470 / 2c4a361）| Seen 2×
7. LLM 输出必须容错+重试 | 拆课 JSON 键漂移（nodes/knowledge_points/lessons）、格式偶发不合法 → 三 key 容错 + 解析失败自动重试一次；归一化失败回退原名 | commit 61f0c13 | Seen 1×
8. 决策与生成必须分离 | 让 LLM 直接决定「出哪题/多难」会超纲+难度漂移；策略层（bandit/Elo/bias）出结构化指令包，LLM 只在约束内生成 | commit 0c8ec9a | Seen 1×
