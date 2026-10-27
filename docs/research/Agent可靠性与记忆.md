# Agent 可靠性与记忆 · 三问三答（2024–2026 检索整理）

> 检索日期 2026-10-28。范围：2024–2026 公开论文（arXiv/ACL/NeurIPS）与一线工程博客。每条结论挂出处；文末收敛为 10 条可落地规则。纯文档，未改代码。

---

## Q1 长期记忆如何不漂移、不把旧结论当新事实

**共识机制（论文）**

1. **先分清三种失败点，再谈治理**。SSGM（Stability and Safety-Governed Memory）把演化中的记忆拆出三个失败点：**摄入期记忆投毒（memory poisoning）**、**整合期语义漂移（semantic drift）**、记忆图拓扑导致的知识泄露；治理手段是写入准入+整合约束+图结构治理，而不是「事后清一遍」。
   —— *Governing Evolving Memory in LLM Agents*, arXiv:2603.11768（2026-03）`https://arxiv.org/abs/2603.11768`
2. **冲突消解必须是显式操作，不是隐式覆盖**。Mem0 的写入管道把每条新事实判别为 **ADD / UPDATE / DELETE / NOOP** 四种操作之一：与已有记忆矛盾时走 UPDATE（保留时间线）或 DELETE，重复信息 NOOP 跳过——这直接回答「旧结论当新事实」：矛盾进来时旧条目被点名更新，而不是并排放着。
   —— *Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory*, arXiv:2504.19413（2025-04）`https://arxiv.org/abs/2504.19413`
3. **记忆条目是活的，不是死的**。A-MEM（Zettelkasten 式）：每条记忆带结构化属性（摘要/标签/链接），新记忆到来时**回写更新旧条目的属性并动态建链**——旧结论的「适用范围」随证据演化，而不是僵死在写入时刻。
   —— *A-MEM: Agentic Memory for LLM Agents*, arXiv:2502.12110（2025-02）
4. **用 RL 训「记忆管理」本身**。Memory-R1 用强化学习训练记忆管理器做 add/update/delete/检索决策——管理策略是被优化的对象，不是手写启发式。
   —— *Memory-R1*, arXiv:2508.19828（2025-08）`https://arxiv.org/html/2508.19828v1`
5. **时效衰减要被感知**。STALE benchmark 专门测「agent 知不知道自己的记忆已经过期」；RMM（Reflective Memory Management, ACL 2025）做前瞻+后瞻记忆管理，处理被遗忘/过时条目；「Learning to Forget」用睡眠式巩固通过**主动遗忘**消解记忆间干扰（proactive interference）。
   —— STALE（benchmark，2025）；*Reflective Memory Management*（ACL 2025）；*Learning to Forget: Sleep-Inspired Memory Consolidation*（2025）
6. **分层是骨架**。MemGPT 的 OS 式分层（主上下文+外部存储+自调度换页）是 2025 层级记忆（如 Hierarchical Memory, arXiv:2507.22925）的共同底座：**工作结论放上层可变，事实档案放下层只追加**。
   —— *MemGPT*, arXiv:2310.08560（2023-10）；*Hierarchical Memory for High-Efficiency Long-Term Reasoning*, arXiv:2507.22925

**工程侧呼应**：本仓库（voyage）的先验库/教训日志纪律——「条目级增改、禁止整篇重写」——正是 SSGM 语义漂移治理的朴素实现：漂移最容易发生在「整段重写时丢掉约束」。

---

## Q2 工具调用如何不盲目重试、不误判

1. **盲目重试是可观测的失败模式**：tool-call 参数采样会陷进局部极小——每次重试重新采样出**几乎相同的参数**，同样的错吃 N 遍。工程对策是**硬阻断重复同参调用**（内容指纹比对），并把错误回喂模型要求改写参数。
   —— NousResearch/hermes-agent issue #67829 `https://github.com/NousResearch/hermes-agent/issues/67829`
2. **错误必须分类后才决定重试策略**：网络类→指数退避重试；参数/校验类→把**结构化错误信息**回喂 LLM 让它改参数（self-correct），而不是原样再发；权限类→不重试，升级处理。多个工程博客收敛到同一结论："don't blindly retry the same call; send the error back to the LLM for reformulation."
   —— *4 Fault Tolerance Patterns Every AI Agent Needs*（dev.to）；*Why LLM agents break when you give them tools*（dev.to）；检索日 2026-10-28
3. **熔断按依赖失败率，不按单次失败**：对 LLM 供应商/外部依赖设失败率熔断（连续 N 次失败→该工具/供应商本轮下线），配合超时与状态跟踪，防 retry storm。
   —— *When Agents Fail: Retry Logic, Circuit Breakers, and Dead-Letter Queues*（supergood.solutions, 2026）；*Agent Reliability Patterns for Avoiding Runaway Calls*（LinkedIn, 2026-01）
4. **验证不足是最大类失败之一**：MAST 失败分类学（NeurIPS 2025）统计多 agent 系统失败分布——规范问题 ~41%、agent 间失配 ~37%、**任务验证不足 ~21–31%**（含「验证机制缺失 ~9.2%」「验证流程漏掉关键错误 ~13.6%」「过早停止」）；对策是把「执行完毕」与「验证完毕」拆开成两个动作。
   —— *Why Do Multi-Agent LLM Systems Fail?*, arXiv:2503.13657 `https://arxiv.org/abs/2503.13657`
5. **回读验证（read-back）**：写操作完成后用一次独立的读操作核对落盘结果再宣布成功——这是 MAST「验证类失败」在单 agent 场景的最小实现；对不可回读的外部操作，用 dead-letter/人工确认队列。
   —— 同上 MAST + durable execution 模式（vadim.blog《Durable Execution for LLM Agents》，2025）
6. **自验证要选择性使用**：Self-Verification Dilemma 指出自验证有真实成本且常冗余——用经验池判断哪些步骤值得验证、哪些跳过；「事事自检」和「从不自检」都是错的。
   —— *Self-Verification Dilemma: Experience-Driven Suppression*, arXiv:2602.03485（2026-02）

---

## Q3 如何让 agent 主动报告不确定而非编造

1. **「我不知道」是可训练的一等答案**：R-Tuning（Negation-Aware Fine-Tuning）证明模型连「我无法回答」的判断都能训——把不可答问题识别为独立能力而非副作用。
   —— *R-Tuning: Instructing Large Language Models to Say 'I Don't Know'*, arXiv:2311.09576（2023-11，NAACL 2024）
2. **校准知识边界后弃答**：Don't Hallucinate, Abstain 用校准方法划出模型知识边界，边界外直接弃答。
   —— arXiv:2402.00367（2024-02）`https://www.alphaxiv.org/abs/2402.00367`
3. **有统计保证的弃答阈值**：Conformal Abstention 给出基于保形预测（conformal prediction）的弃答程序——错误率有形式化界，不是拍脑袋阈值。
   —— *Mitigating LLM Hallucinations via Conformal Abstention*, arXiv:2405.01563（2024-05）
4. **奖励结构决定诚实度**：训练/评测里「自信地答错」必须比「弃答」罚得更重，给恰当的「I don't know」记正分——二值判分（90%+ 为对的基准）天然惩罚不确定性表达，这是编造的激励根源。2025-12 的行为校准 RL 工作把弃答作为一等训练目标（"I don't know" 从失败变成协调信号）。
   —— Genta 综述《Why Do LLMs Hallucinate》（2025-10）`https://genta.dev/resources/why-do-llms-hallucinate`；*Mitigating LLM Hallucination via Behaviorally Calibrated RL*（ResearchGate, 2025-12）
5. **agent 场景的最小实现**：把「可观测证据（工具读数/检索片段）」与「模型推断」在输出里强制分离；无证据支撑的回答必须附带「未知 + 下一步验证动作」而非流畅补全（OpenAI 社区对 agent 不确定性度量与 selective answering 的工程讨论，2025-10：`https://community.openai.com/t/measuring-an-agent-s-own-uncertainty-to-reduce-hallucinations-project-summary-discussion/1364118`）。

---

## 10 条可落地规则

| # | 规则 | 出处 |
|---|---|---|
| 1 | **新事实入库前过操作判别器**：ADD/UPDATE/DELETE/NOOP 显式四选一；与旧条目矛盾时更新旧条目并盖时间戳，禁止新旧并存 | Mem0 2504.19413；Memory-R1 2508.19828 |
| 2 | **记忆条目级增改，禁整篇重写**——漂移藏在「重写时丢掉的约束」里 | SSGM 2603.11768；本仓 ACE 纪律 |
| 3 | **每条记忆带三件套：来源+时间戳+置信度**；检索注入时过期条目只能以「旧结论（截至 X 月）」名义出现，不得当新事实 | STALE；RMM（ACL 2025） |
| 4 | **分域分库**：用户事实 / 任务结论 / 系统先例分层存放，写入路径互不串（上层可变、下层只追加） | MemGPT 2310.08560；Hierarchical Memory 2507.22925 |
| 5 | **工具失败先分类再行动**：网络→退避重试；参数→结构化错误回喂改参；权限→升级，不重试 | dev.to 容错模式（2025）；hermes-agent #67829 |
| 6 | **同参重试硬阻断**：对工具名+参数做指纹比对，重复同参调用直接拒绝并强制改变策略 | hermes-agent #67829（blind retry） |
| 7 | **依赖熔断按失败率**：同一工具/供应商连续 N 次失败→本轮下线+dead-letter，防 retry storm | supergood.solutions（2026）；vadim.blog |
| 8 | **写后必回读**：「执行完毕」与「验证完毕」是两个动作；不可回读的外部操作走人工确认队列 | MAST 2503.13657（验证类失败 21–31%） |
| 9 | **证据与推断强制分离输出**：无证据支撑时输出「未知 + 下一步验证动作」，弃答是合格答案 | 2402.00367；2405.01563；R-Tuning 2311.09576 |
| 10 | **罚错结构校准**：评测与训练里「自信答错」罚重于「弃答」，「我不知道」记正分 | Genta（2025-10）；BC-RL（2025-12） |

> 落地顺序建议：先做 5/6/7（工具层护栏，收益最快、纯工程）→ 再做 1/3（记忆写入判别器+三件套，改 schema）→ 最后 9/10（输出契约与评测罚分，牵动产品话术）。8/2/4/9 与本仓库已有实践（教训日志、先例库、双写纪律）天然对齐。
