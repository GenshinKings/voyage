# 第七轮：记忆巩固（借脑科学的三条功能，不模拟神经元）

主人提议：按脑科学成果做一套 AI 记忆与「智慧」系统。本文把它收窄成可做、可测的一小块。

**明确不做**：不复刻神经元、不接意识研究、不追 860 亿这个数。借的是功能架构：工作记忆容量有限、海马快写、睡眠重放。

---

## 一、数据结构（新文件，不动 state.json）

`outpost2-data/memory/memories.json`

```json
{
  "version": 1,
  "entries": [
    {
      "id": "m-20261028-01",
      "text": "主人 10-28 起每晚去京东做 1.5 小时临时工，40 元，说发了工资给 token 充值",
      "kind": "fact",              // fact | preference | commitment | impression | conclusion
      "source": "session:1026-ythono-01@2026-10-28",
      "ts": "2026-10-28T19:54:00+08:00",
      "confidence": 0.9,           // 0~1，LLM 给初值，被引用/被证实时上调
      "hits": 0,
      "lastUsedAt": "",
      "status": "active"           // active | cold | superseded
    }
  ]
}
```

**三件套是硬要求**：`source` / `ts` / `confidence` 缺一不许入库。

## 二、夜间巩固（低峰任务）

触发：每天低峰时段一次，或手动 `/api/memory/consolidate`。

流程：
1. 取当天所有会话的原始消息（只取用户侧 + 带事实的助教确认句）
2. 与已有 active 条目一起喂给 LLM，要求输出操作列表：`ADD / UPDATE / DELETE / NOOP`
   - 与旧条目矛盾 → **UPDATE 旧条目**（旧值进 `history`，不并存两条）
   - 重复信息 → NOOP
3. 产出 5~7 条（少于 5 条就明确写"今天没什么值得记的"，禁止凑数）
4. 每条必须带 source（哪一场会话、哪一句）、ts、confidence

## 三、开场引用（把记忆变成教学动作）

每天早上第一节开场时：
- 检索与该知识点相关的 active 条目（time decay：越久越靠后）
- 最多引用 **1 条**，写成自然句：「你上次说……」
- 若条目 `ts` 超过 30 天且 confidence < 0.6，必须写成「旧结论（截至 X 月）……现在可能变了」
- 引用后 `hits+1`，`lastUsedAt=now`，confidence +0.05（上限 0.99）

## 四、衰减与遗忘（关键，防记忆变垃圾场）

- 30 天未被引用 且 confidence < 0.5 → status = cold（不删，检索默认不取）
- 被新事实取代 → status = superseded，检索永不返回
- `pinned: true` 的条目不衰减（用于身份/长期承诺）

## 五、验收标准（缺一不算完成）

1. 造两天假对话，跑巩固：产出 5~7 条，每条 source/ts/confidence 齐全
2. 第二天开场**确实**引用了其中一条，且引用文本能在条目里逐字找到
3. 注入一条与旧条目矛盾的新事实：旧条目被 UPDATE（history 有旧值），库里不同时存在两条相反说法
4. 把某条 ts 改成 40 天前且 confidence 0.4，再跑检索：它不出现在结果里（进了 cold）
5. 巩固任务失败时不影响主流程（只记日志，不阻塞今天三步）

## 六、纪律

- 记忆只写 `outpost2-data/memory/`，不动 state.json
- 一切 LLM 输出必须过 schema 校验，校验失败则丢弃该条并记日志（不允许"半条记忆"入库）
- 每项单独 commit，commit message 标 `第七轮-P0-x`
