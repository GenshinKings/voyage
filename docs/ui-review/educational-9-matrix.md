# 教育学九条 × 界面对账（小穗四轮·三 + 2026-10-27 实测复查）

> **2026-10-27 复查纪律**：每一条按「页面取证/真实点击」复核，不按 commit message 记账。发现 P0-2 点击链路两处回归已修（见文末事故段）；第 4/5 条的次数类 UI 承诺未上线，如实标注，不注水。旧里标了「本轮新增」但页面无落点的条目已按现状改回。

| # | 理论 | 已有机制（前几轮） | 本轮状态（实测） | 截图证据 |
|---|---|---|---|---|
| 1 | 认知负荷 Sweller（样例效应） | 开场四段模板、单步一动作、单块≤5 行（f3dddde）；teachSystem 加「先给可运行样例再让学习者改它」 | prompt 红线已入（9e958b4）；截图=真实教学会话行为，待下一次真学自然产生——**不造数据** | — |
| 2 | 检索练习 Roediger | 判定式检查题「贴输出」 | 每节课 ≥1 次检索已成立 | 前轮 after-P1-verdict-exit |
| 3 | 间隔重复 | P0-1 复习并入三步（efd940e）+ 错题回归 wrongHits | 代码就绪；当前数据 0 张到期卡（第 0 步占位确证过代码，诚实不造 due） | — |
| 4 | ZPD Vygotsky | FSRS + PracBias 楼梯默存；Elo/bandit 实验默认关 | **部分未回票**：难度带约束在题面生效；「当前档位/近 10 题正确率」进度页 badge 数据下发侧未接，下一轮补；实验开关默认关已可证 | after-R4b-settings-lab |
| 5 | SDT Deci&Ryan | 自主=我会/深/卡住三选（84fa5e9+258767d） | 自主 ✓；**胜任/归属计数**（「第 N 次通过」「你第 N 次做出这类作品」）未上线，下一轮补 | after-R4b-home-full 底部三选按钮可见 |
| 6 | 心流 Csikszentmihalyi | 每步时长样本 StepMins | 本轮把三步卡每步「约 8/5/20 分钟」标注**真正补进 UI**（旧矩阵声称过但无可见落点） | after-R4b-home-full |
| 7 | Fogg B=MAP | 首页单主按钮+上次停在顶部 | 两分钟版第四档 + keep=1 真减步折叠——**点击链路修好后实测可用**（选 2 分钟 → 其余步 dim+「留给明天」） | after-R4b-fold-2min + after-R4b-home-full |
| 8 | Dweck 成长型思维 | 未达标→一句最小提示+关联环节（f3dddde） | 反馈含「具体差在哪一步」、禁「天赋/聪明」评价词——prompt 红线入 teachSystem（9e958b4），行为层待真实使用验证 | — |
| 9 | 防习得性无助 | PracBias 楼梯 ±40 降升机制（历史已在） | 「连续 2 次同类错→降一档给更小的同类题」入 teachSystem（9e958b4） | — |

## 事故段（2026-10-27 复查发现并修复）

1. **P0 级回归：四档预算按钮全灭**。`function renderTodaySteps(){` 作用域未闭合，把 `moodPick/moodRow` 吞进嵌套作用域——内联 `onclick="moodPick(...)"` 解析失败（主世界探测 `window.moodPick===undefined`），首页四档按钮点击均静默无反应。修复：拆出 `renderPlanBody()`，`moodPick/moodRow` 回顶层。
2. **真减步恒不折叠**。`todaySteps()` 中 `mood==='2'` 的 `mood` 被同名全局函数 `async function mood(wantMore)` 的 hoisting 结果遮蔽（恒为函数 → 恒 false）→ `keep=steps.length`。修复：局部显式 `localStorage.getItem('voyage.mood')` 读取。
3. **修复验证**（临时数据副本 + 7433 端口实测）：选「两分钟版」后组标签出现「· 两分钟版：只做这一步就好」，练/副本两步 dim + 「留给明天」=真减步成立（截图 after-R4b-fold-2min）。

## 拍摄方式（真实数据零污染）

真实数据复制到临时目录起独立端口（7433）→ 浏览器截图 → 副本即弃；真实 `outpost2-data/` 全程零写入。截图中偶现的「请求失败」toast 系副本 LLM 网络不可达所致，与被验收功能无关。
