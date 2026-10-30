# InkPal × voyage 桥接

> 这两个软件是**一个助教的两只手**：voyage 管教学与状态，InkPal 管笔记的读写与展示。
> 它们共享同一份学习者画像、长期记忆与教学法技能。

## 为什么不能各配一套模型

插件最初自带 `engine/llm.ts`，自己填 baseUrl / apiKey / model。结果是：

- 你说过的偏好、卡在哪里、改过什么——**voyage 知道，插件不知道**
- 插件给你的批注不会进到 voyage 的指标里，学习进度是断的
- 两个地方各连一次模型，token 花两遍

所以桥接的目标只有一个：**大脑只有一份，留在 voyage。**

## 三个接口

### `POST /api/inkpal/chat`

把对话交给 voyage 的助教。system 里会拼上 teach 技能摘要 + 学习者画像。

```json
// 请求
{"messages":[{"role":"user","content":"…"}]}
// 响应
{"reply":"…","sharedTutor":true}
```

### `POST /api/inkpal/annotate`

行级批注：找出「理解错了 / 说不清 / 漏前提」的地方。

```json
// 请求
{"path":"一些笔记/Golang.md","content":"笔记全文"}
// 响应
{"items":[{"line":3,"quote":"…","kind":"concept",
           "myUnderstanding":"你当时的理解","correct":"正确说法"}]}
```

- `kind`：`concept` / `unclear` / `missing` / `ok`
- **定位不到行号、或没给正确说法的条目会被丢掉**——不给插件显示半截批注

### `POST /api/inkpal/score`

```json
{"path":"…","content":"…"}
→ {"concept":4,"structure":2,"reviewability":2,"advice":["…"],"summary":"…"}
```

三项各 0~5（越界会被钳住），建议最多 3 条，且要求具体到「改哪一句」。

### `POST /api/inkpal/vault-review`

整库体检。插件传「路径 + 标题 + 摘要」清单（限 120 篇），voyage 回总评、问题、建议双链。

```json
{"vault":"Obsidian_WorkAndStudy",
 "notes":[{"path":"…","title":"…","excerpt":"…"}],
 "summary":"…","issues":[{"path":"…","issue":"…","suggestion":"…"}],
 "links":[{"from":"…","to":"…","why":"…"}]}
```

## 取舍

**voyage 不直接读硬盘上的 vault。** 由插件把内容传上来，理由：

1. 少一个路径配置（也就不存在路径配错）
2. 少一处能越权读文件的口子
3. 以后插件跑在别的机器上，只要网络通就能用

代价是整库体检要由插件负责扫目录——那本来就是它的强项。

## 只读原则

插件对笔记**只读不写**。想做双链、加标签这类操作时，路径是：

1. 助教给出「动作清单」（改哪个文件、改成什么）
2. 人在面板里逐条确认
3. 才由插件执行

不做「自动改库」——改错了没有第二个人能看出来。
