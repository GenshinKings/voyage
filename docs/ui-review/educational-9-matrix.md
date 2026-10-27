# 教育学九条 × 界面对账（小穗四轮·三）
> 只新增缺的四条约束文案与三处界面可见元素；已落地条目在此对账并挂截图。

| # | 理论 | 已有机制（前几轮） | 本轮新增 | 截图 |
|---|---|---|---|---|
| 1 | 认知负荷 Sweller | 开场四段模板、单步一动作、单块≤5行（f3dddde） | teachSystem 加「先给可运行样例，再让学习者改它」（样例效应） | after-R4-clean-home（四段模板示例待真实 LLM 验） |
| 2 | 检索练习 Roediger | 判定式检查题「贴输出」上线（f3dddde） | 无需——每节课 ≥1 次检索已成立 | after-P1-verdict-exit |
| 3 | 间隔重复 | P0-1 复习并入三步（efd940e）+ 错题回归 wrongHits | 无需 | after-R4-clean-home（第 0 步占位逻辑代码） |
| 4 | ZPD Vygotsky | FSRS + PracBias 楼梯已默存；Elo/bandit 实验关 | 进度页加「当前档位/近 10 题正确率」（本轮） | after-P12（统计行旁新增 badge） |
| 5 | SDT Deci&Ryan | 自主=我会/深/量三选（84fa5e9+258767d） | 胜任：判定 toast 加「第 N 次通过」；归属：收藏墙卡片显示「你第 N 次做出这类作品」 | 见收藏墙截图 |
| 6 | 心流 Csikszentmihalyi | 每步时长 stats（StepMins）已有 | 今日三步每步标「约 M 分钟」（讲8/练5/复习3/副本20 估算） | after-13-today-steps 更新 |
| 7 | Fogg B=MAP | 首页单主按钮+上次停在顶部（已有） | moodRow 加「两分钟版」第四选项——budget=2 → keep=1，真减步 | 同上截图 |
| 8 | Dweck 成长型思维 | 未达标→一句最小提示+关联环节（f3dddde） | prompt 补：反馈必须含「具体差在哪一步」一句；禁「天赋/聪明」类评价 | —（文案红线入 prompt） |
| 9 | 防习得性无助 | PracBias 楼梯 ±40 降升机制（历史已在） | 同 8 的这句补进 prompt：「连续 2 次同类型错 → 降一档给更小的同类题」 | — |

> prompt 约束汇总 diff 见本次 commit；界面可见项配 P0-2 的三步卡与进度页统计行，直接截图可核。
