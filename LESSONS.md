# LESSONS.md — 前哨 2.0 错题本

> 记录格式：一句规则 | 原因 | 证据（commit/文件）| Seen N×。
> 同一条教训在 3 个不同工作日出现 → 转正进 AGENTS.md 硬规则区（见晋升通道）。
> 只记「修完又返工/不该合/验证太糙/静默失败」类教训；一次性笔误不入。

## 2026-10-28

14. 改写既有判定链时，多返回值函数的接值次序必须对照原调用点 | 第八轮-P0-1 重写降档分支写成 `_, ev := ParseCause(reply)`——reason 被丢进 _、拿证据文本做白名单校验恒 false，带 [CAUSE] 的失败轮不再降档；已合入 master 才被既有测试 TestGuideDescendsAfter2Fails 抓住 | 修法：`reason, _ := ParseCause(reply)` 对照原调用点；教训回流见 commit fix(第八轮-P0-1) | Seen 1×

15. 提交链里测试退出码会被管道吃掉 | `go test ... | tail -2 && git commit` 两次带病提交（tail 的退出码代表整条管道）| 修法：全量检查单独跑、失败用 `grep -E "FAIL|panic"` 显式过滤而非 head 截断（head 还会截掉 FAIL 行制造全绿假象）| Seen 2×

## 2026-10-25

12. 外部页面抓取必须配「真连诊断测试」| trending 解析对 fixture 全绿但真连 0 条——GitHub 把 repo 链接的尾斜杠去掉了（hrefRe 假设 /"结尾），且 svg path 数据里的数字会污染星数提取 | 修法：live_test.go（GHFETCH_LIVE=1 环境开关）+ 失败时 dump 真实页面对比 + 正则放宽（article 类含 Box-row 即可、尾斜杠可选、</svg> 之后再取数）| Seen 1×

13. tool 帧的 content 永远不能是空串 | LLM API（DeepSeek）要求 tool 消息 content 必须是字符串——工具返回 "" 被 Message.Content omitempty 序列化成 null →「content should be a string or a list」400 | 修法：runAgent 里空结果替换为「（工具执行成功但无输出）」；并在 API 错误里附消息序列指纹（system,user,assistant(+tc2),tool…）排障 | Seen 1×

## 2026-10-23

10. 横切机制接入必须以「调用点清单」收口 | v4.2 称「tutor/exercise 全调用点接入」AGENTS.md 约束，但 exercise.Generate 的 ChatJSON 漏包 withAgents——出题调用一直在约束外裸奔，直到本次接画像注入（同一个汇聚点）才发现；根因：两包各有一份 withAgents，没有唯一汇聚点 | commit 本次 | Seen 1×

11. push 挂死≠网络问题，先查 credential helper 链 | 系统级 gitconfig 配的是 helper-selector（弹 GUI 窗问用哪个真 helper），后台运行时窗口没人点→push 永久挂起且无任何输出；修法：`git config --global credential.helper manager` 绕开 selector。另：GitHub 直连 443 会被重置，push 必须走 clash 代理（127.0.0.1:7897，`-c http.proxy`），POST 上传常被节点卡死→后台循环重试（代理/直连交替）最终能过 | commit 本次 | Seen 1×

## 2026-10-20（续）

9. 关键配置文件的消失要立刻可感知：llm.json 从工作目录消失（原因未明）导致 LLM 全线瘫痪才发现；恢复源=1.x SQLite settings 表。教训：Serve 启动时若 LLM 配置缺失应在方向卡/toast 常驻提示，而不是等出题 400 |Seen 1×

## 2026-10-20

1. 数据必须单一真相源 | Ledger 双写不一致：Record 只落 ledger.json，state.json 副本从未更新，重启加载只信 state.json → 用户履历被吞 | commit 32886bc + TestLedgerSurvivesRestart | Seen 1×
2. JSON 文件顶层形状必须与加载目标一致 | readings.json 写成 {"readings":[...]} 而加载目标是数组 → 静默加载失败，字段为 nil | commit 50ece24 | Seen 1×
3. 同一文件禁止连续增量补丁；复杂 JS 生成禁止走 python heredoc | index.html 五六个补丁互相覆盖污染（c49e470）；heredoc 的 \ 转义坍缩两次把 JS 的 ' 吃成 '（本次：openReview 评分按钮行字符串断裂→整页脚本瘫痪白屏「加载中」，桌面端用户直接撞上）| 修法：node --check 提取 <script> 做语法门（app_check.js），JS 内嵌 HTML 一律双引号外层+Edit 工具直改 | Seen 2×
4. rAF 不可依赖 | 后台页/iframe 页 requestAnimationFrame 被节流暂停，绘制闭包静默不执行；改 await rAF 更是永久卡死 | commit 88f03fb | Seen 1×
5. 物理模拟必须有界 | 力导向逐对直接位移无上限，第 1 轮迭代坐标冲到 -5.5e6、第 2 轮 NaN，fill(NaN) 静默不画；改标准 FR（合力累加+退火位移 clamp）数学保证收敛 | commit 88f03fb | Seen 1×
6. 双端语义抄形不抄义会翻车 | 抄 DeepSeek 侧栏抄成课程排期（欠账清单）而非会话历史（存货），违背宪法案例零；被用户两次纠偏（c49e470 / 2c4a361）| Seen 2×
7. LLM 输出必须容错+重试 | 拆课 JSON 键漂移（nodes/knowledge_points/lessons）、格式偶发不合法 → 三 key 容错 + 解析失败自动重试一次；归一化失败回退原名 | commit 61f0c13 | Seen 1×
8.5 视觉风格以用户参考图为准绳：知识图谱=小灰点+细字标签+若有若无的边，hover 才聚焦邻域变蓝（Obsidian/Magpie 极简派）——渐变球体+点阵背景+粗边那套被用户否决重做 | commit 待查 | Seen 1×

8. 决策与生成必须分离 | 让 LLM 直接决定「出哪题/多难」会超纲+难度漂移；策略层（bandit/Elo/bias）出结构化指令包，LLM 只在约束内生成 | commit 0c8ec9a | Seen 1×
