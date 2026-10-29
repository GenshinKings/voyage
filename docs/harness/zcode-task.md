# 发给 ZCode 的任务前缀（复制这段，再换掉 <> 部分）

```
Harness rules for this task (follow strictly):

1. PROPOSE first, max 15 lines: what you will build, what you will NOT build,
   and the exact commands/actions that prove it works. No code in this turn.
2. Wait for approval. Then APPLY: implement step by step, running the proof
   commands after each step. If a command fails, fix it before moving on.
3. Do not write long reviews or essays. If you need to explain, use <= 8 lines.
4. Never claim "done" without pasting the actual output of the proof commands.
5. Commit messages: `roundN-P?: <what changed> -- <what was verified>`.
6. ARCHIVE: at the end, write 2-4 lines: which rule this task confirmed,
   which pitfall it hit, and where the docs were updated.

Task: <一句话说清这次要什么>
Not doing: <明确不做的>
Proof: <命令/操作 + 期望结果>
```

## 为什么这么写

- ZCode 默认输出极长（实测一次 30–40 分钟），不设上限会把时间全吃掉
- 它容易在「需要权限」的对话框上停下，所以第 2 条要写「等批准」而不是「自己开工」
- 不接受口头「已完成」，只接受命令输出，这条最省扯皮
- ARCHIVE 是最容易被跳过的一步，必须写进前缀里
