# 简介定稿（小穗，2026-10-28 00:19）

## 仓库 About 描述（主人已选，直接粘贴）

```
AI 当老师，不是当搜索引擎。它先问、再陪做、会记住你错在哪——从第一个能跑的东西走到你自己的作品。Windows 单二进制（Go），数据全在本地。
```

## 英文版（放 README 顶部）

```
An AI that teaches instead of answering. It asks first, builds alongside you, and remembers where you got stuck — from your first working thing to your own work.
```

## README 要补的三处（交给 ZCode）

1. 首屏加「它现在能做什么」三行（动词开头）：
   - 拆课：说一句「我想学 Go」，拆成 4–8 节并说明为什么是这个顺序
   - 陪做：三档引导随你切换（给样例 → 半提示 → 只提问），卡住时只给一个动作
   - 攒证据：每节收尾记入履历，副本产出直接变成简历条目

2. 新增一节「助教会跟着你变」：
   - 连续 3 次通过 → 引导升级（样例 → 半提示 → 只提问）
   - 连续 2 次未过或点「我卡住了」→ 自动退回一档
   - 答错会记原因（概念没懂 / 看漏条件 / 环境问题 / 手滑），下次直接讲上次那个点

3. 把「现在有什么 / 还没有什么」提到截图之后，「还没有」里补：多用户、云同步、macOS/Linux。

## 给 ZCode 的一句话

> 读 docs/ui-review/简介与README增量.md，按定稿描述更新 README 顶部，并补「它现在能做什么」「助教会跟着你变」两节，把「现在有什么/还没有什么」提到截图之后。
