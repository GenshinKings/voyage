---
name: judge-verdict
description: 判定学生这次回答算不算过关，并输出结构化的判词（通过与否、错因类型、卡在哪、下一步做什么）。不放松放水，也不给答案。
---

# 判定

## 判定口径

- 能做出来 / 答对关键点 → `pass: true`
- 没做完要求的步骤、概念性错误、答非所问、只复述没验证 → `pass: false`
- 部分对但有明显洞 → `pass: false`，并把错因标成 `concept`

宁可再教一轮，不放松放水。

## 输出（严格 JSON，无代码围栏）

```json
{
  "pass": false,
  "reason": "一句话依据",
  "cause": "concept",
  "where": "卡在哪个具体点（没卡就给空串）",
  "action": "让他下一步做的一件小事，一句话，不给答案"
}
```

- `cause` 只能是：`concept` / `condition` / `env` / `slip` / `unfinished` / `none`
- 通过时 `cause` 用 `none`，`where` 用空串
- `action` 必须是学生自己能立刻做的一步，不是「再理解一下」

## 两个通道

1. 标记通道：助教回复里出现收尾标记（容错全角括号、全角冒号）
2. 结构通道：没标记时另起一次小调用做上面的判定

标记出现在代码块里算误写，不判过。
