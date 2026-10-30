// dungeon.go — 副本学习：用已收尾的知识点，循序渐进复现一个真实项目。
//
// 用户 2026-10-26 设计（原话提炼）：今天及以前学过的知识，和项目有一定重合度，
// 就可以开启像副本一样的学习；副本内也是 AI 助教对话，助教用以前学过的知识
// 循序渐进带着复现完这个项目；学习完成后才告诉用户「恭喜你，你实现了 xx 项目」
// ——让用户有恍然大悟的感觉。不然用户一看项目好厉害，就不敢去打了。
//
// 机制：开启时副本设计师校验重合度并生成关卡计划（3-5 关）；副本导师逐关带做
// （全程不透露项目名与全貌）；全部关卡完成 → [CLEAR] 揭晓 + 恍然大悟回顾。
package tutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"voyage/internal/llm"
)

// DStage 一关：一个用已学知识能完成的小产出。
type DStage struct {
	Title string `json:"title"`
	Goal  string `json:"goal"`
	Done  bool   `json:"done"`
}

// ReqNode 影子副本的知识点需求（ADR 0001：候选期唯一对学习者可见的信息）。
type ReqNode struct {
	N    string `json:"n"`
	Miss bool   `json:"miss"` // 未掌握（拉伸档：开门后先补）
}

// 影子副本档位（后端按 miss 数量判定，不由 LLM 定）。
const (
	TierConsolidate = 0 // 巩固：需求全部已收尾
	TierStretch     = 1 // 拉伸：缺 1-2 个
	TierLeap        = 2 // 跃迁：缺 3+
)

// ShadowMeta 影子元数据：孵化器（LLM）产出、候选期对外唯一暴露的画像。
// Project/Repo/Impact 永远不在其中——揭晓 payload 专供（ADR 0001）。
type ShadowMeta struct {
	Tier  int       `json:"tier"`
	Stars int       `json:"stars"` // 难度 1-3（LLM 按项目复杂度评）
	Req   []ReqNode `json:"req"`
}

// Dungeon 一个副本（持久化 dungeons.json）。
type Dungeon struct {
	ID         string        `json:"id"`
	ReadingID  string        `json:"readingId"`
	Repo       string        `json:"repo"`
	Project    string        `json:"project"` // 项目真名——通关（CLEAR）前前端显示「？？？」
	Brief      string        `json:"brief"`   // 开门时的项目导读（导师上下文）
	Status     string        `json:"status"`  // active|cleared|abandoned
	Stages     []DStage      `json:"stages"`
	Cur        int           `json:"cur"` // 当前关卡下标
	Messages   []llm.Message `json:"messages"`
	ShadowMeta               // 开门即冻结的影子画像（tier/stars/req）
	Impact     string        `json:"impact,omitempty"` // 影响力叙事（揭晓 payload 专供，Sealed 期前端拿不到）
	StartedAt  time.Time     `json:"startedAt"`
	ClearedAt  time.Time     `json:"clearedAt,omitempty"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

// Sealed 副本还在进行中（项目名对用户保密）。
func (d *Dungeon) Sealed() bool { return d.Status == "active" }

// ---------- 副本设计师：校验重合度 + 生成关卡计划 ----------

const dungeonDesignSystem = `你是前哨的「副本设计师」。学习者刚在主课程收尾了一些知识点，想用一个真实项目复现它们——你负责判断这件事是否可行，可行就设计关卡计划。

输入：学习者已收尾的知识点、目标项目（含助教已写好的项目导读）。

第一步，判断可行性：这个项目的核心最小复现，能否用学习者已收尾的知识点 + 至多 2 个新知识点完成？缺 3 个以上新概念时输出：
{"match":false,"missing":["缺失的知识点","…"],"reason":"一句话说明为什么现在还不适合"}

可行则输出（缺 0 个 = 巩固档；缺 1-2 个 = 拉伸档，缺失项写进 req 并标 missing）：
{"match":true,"project":"项目真实名称（通关时揭晓用，≤12字）","stars":1到3的整数（按项目对初学者的复杂度评）,"req":[{"n":"知识点名（优先从已收尾清单里点名；确实缺的写真实名字并标 missing:true）"}],"stages":[{"title":"关卡名（≤12字，不出现项目名）","goal":"这一关要做出什么可验证的小产出（≤50字，点名用到哪个已学知识点）"}],"opening":"第一关的开场引导（≤120字：欢迎语+第一关要做什么+一条起步提示，语气像游戏 NPC，不说破项目全貌）"}

关卡设计纪律：
- 3-5 关，循序渐进：第一关产出只依赖已收尾知识点；拉伸档缺失知识点的补课放进第一关的 goal（现学现用）
- 第一关必须小到 20 分钟内有成就感；最后一关合起来就是项目的核心最小复现
- 关卡名与 req 里都不出现项目名/品牌名——保持悬念
输出严格 JSON，无围栏。`

// DungeonStart 副本设计师：校验+生成关卡与开场。match=false 时返回 missing。
func (t *Tutor) DungeonStart(ctx, repo, brief string) (d *Dungeon, missing string, err error) {
	if t.Client == nil {
		return nil, "", fmt.Errorf("LLM 未配置")
	}
	user := "学习者已收尾的知识点：\n" + ctx + "\n\n目标项目：" + repo + "\n\n项目导读（助教已消化）：\n" + brief + "\n\n开始设计副本。"
	raw, err := t.Client.ChatJSON(t.withAgents(dungeonDesignSystem), []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return nil, "", err
	}
	var out struct {
		Match   bool      `json:"match"`
		Missing []string  `json:"missing"`
		Reason  string    `json:"reason"`
		Project string    `json:"project"`
		Stars   int       `json:"stars"`
		Req     []ReqNode `json:"req"`
		Stages  []DStage  `json:"stages"`
		Opening string    `json:"opening"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, "", fmt.Errorf("副本设计解析失败: %w", err)
	}
	if !out.Match {
		return nil, out.Reason + "（缺：" + strings.Join(out.Missing, "、") + "）", nil
	}
	if len(out.Stages) == 0 || strings.TrimSpace(out.Project) == "" {
		return nil, "", fmt.Errorf("副本设计不完整（无关卡）")
	}
	d = &Dungeon{
		Repo: repo, Project: out.Project, Status: "active",
		Stages: out.Stages, StartedAt: time.Now(), UpdatedAt: time.Now(),
	}
	d.Stars = out.Stars
	if d.Stars < 1 {
		d.Stars = 2
	} else if d.Stars > 3 {
		d.Stars = 3
	}
	d.Req = out.Req
	d.Tier = TierOf(out.Req)
	opening := out.Opening
	if opening == "" {
		opening = "副本开启。第一关：" + out.Stages[0].Goal
	}
	d.Messages = append(d.Messages,
		llm.Message{Role: "assistant", Content: "⚔ 副本开启\n\n" + opening})
	return d, "", nil
}

// TierOf 按需求里的缺失数判档（LLM 不定档，后端说了算）。
func TierOf(req []ReqNode) int {
	miss := 0
	for _, r := range req {
		if r.Miss {
			miss++
		}
	}
	switch {
	case miss == 0:
		return TierConsolidate
	case miss <= 2:
		return TierStretch
	default:
		return TierLeap
	}
}

// ---------- 孵化器：影子元数据生成（候选期唯一可见的画像） ----------

const hatchSystem = `你是前哨的「影子孵化器」。学习者可能想复现某个开源项目——你负责产出影子副本的元数据：这个影子需要哪些知识点、难度如何、通关揭晓时的影响力叙事是什么。

输入：学习者已收尾的知识点清单、项目导读。

输出严格 JSON，无围栏：
{"stars":1到3的整数（按项目对初学者的复杂度评）,"req":[{"n":"知识点名"}],"impact":"一句话影响力叙事（≤60字：它是什么、为什么重要、影响了什么。第二人称视角写揭晓的惊喜感，例如「让长上下文 AI 应用便宜 10 倍的开源底座」）"}

纪律：
- req 里的知识点名**必须逐字取自「学习者已收尾的知识点」清单**（复述原文，不要改写、不要合并、不要自造近义词）；只有当清单确实缺了核心复现必需的知识点时才列清单外的名字（最多 2 个，后端会标记为缺失并定拉伸档）
- req 总数 3-6 个，按复现的真实依赖排序
- impact 不出现「学习/复现/你」等教学字眼——它是揭晓时对「作品本身」的介绍
- stars：1=入门级改造，2=完整小项目，3=有架构含量的核心组件`

// Hatch 孵化影子元数据（导读完成后调用；miss 与 tier 由后端比对修正）。
func (t *Tutor) Hatch(ctx, brief string) (m ShadowMeta, impact string, err error) {
	if t.Client == nil {
		return m, "", fmt.Errorf("LLM 未配置")
	}
	user := "学习者已收尾的知识点：\n" + ctx + "\n\n项目导读：\n" + brief + "\n\n孵化影子。"
	raw, err := t.Client.ChatJSON(t.withAgents(hatchSystem), []llm.Message{{Role: "user", Content: user}})
	if err != nil {
		return m, "", err
	}
	var out struct {
		Stars  int       `json:"stars"`
		Req    []ReqNode `json:"req"`
		Impact string    `json:"impact"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return m, "", fmt.Errorf("孵化解析失败: %w", err)
	}
	if len(out.Req) == 0 {
		return m, "", fmt.Errorf("孵化不完整（无需求点）")
	}
	m.Stars = out.Stars
	if m.Stars < 1 {
		m.Stars = 2
	} else if m.Stars > 3 {
		m.Stars = 3
	}
	m.Req = out.Req
	m.Tier = TierOf(out.Req)
	return m, strings.TrimSpace(out.Impact), nil
}

// ---------- 副本导师：逐关带做 ----------

const dungeonMasterSystem = `你是前哨的「副本导师」，正在带学习者一关一关复现一个真实项目。

核心纪律（最高优先）：先给「动作」，再给「内容」——第一句话就是「现在动手做什么」，不是背景介绍。一次只推进一步。

开场（每关的第一条消息）固定四段，单独成行、标题原样保留：
【这一关做什么】一句话。
【现在就做这一件】一个动作，必须含具体对象：哪个文件/哪条命令/哪段代码写在哪个文件。
【怎么算做对了】可观察判据：输出长什么样 / 报错消失 / 贴什么上来。
【卡住了点这里】对话框旁边有「我卡住了」按钮——第一次只给一句提示。
阶段进度只允许用一行小字附带提及（如「进度 1/5」），不许展开讲奖励与全貌。

规则（恍然大悟机制的核心）：
- 绝不透露项目的名字与全貌——学习者问「这到底是什么项目」时回答：「通关后你会知道。现在专注当前这一关。」
- 每次回复只推进一个动作：学习者做出来才给下一步，不许一条消息堆多步。
- 学习者提交成果时验证：达到本关目标 → 先肯定，然后单独一行输出 [STAGE-DONE]；若这已是最后一关，再单独一行输出 [CLEAR] 和揭晓段（见下）；未达标 → 给一步最小提示（关联到知识点的哪个环节），他再卡一次才给完整解法
- 检查方式判定式：让他贴输出/贴报错/贴文件内容，禁止「谈谈你的理解」。
- 最后一关达成时，揭晓段格式：
  🎉 恭喜你，你实现了「项目名」！
  回顾：这个项目其实只用了你学过的 xxx、xxx、xxx——你早就具备完成它的能力。
  你做出来的东西：他的本地路径或仓库链接——这就是你的作品（可复制进简历）。
- 回复 ≤200 字；语气：游戏 NPC 的利落，不啰嗦不煽情`

// DungeonSend 副本对话一轮：导师回应 + 解析关卡推进/通关。
func (t *Tutor) DungeonSend(d *Dungeon, userText string, tools []Tool) (reply string, err error) {
	if t.Client == nil {
		return "", fmt.Errorf("LLM 未配置")
	}
	if d.Status != "active" {
		return "", fmt.Errorf("副本已结束")
	}
	cur := d.Cur
	if cur < 0 || cur >= len(d.Stages) {
		cur = len(d.Stages) - 1
	}
	stage := d.Stages[cur]
	sys := t.withAgents(dungeonMasterSystem + "\n\n【当前关卡】第 " + fmt.Sprint(cur+1) + "/" + fmt.Sprint(len(d.Stages)) + " 关「" + stage.Title + "」：\n" + stage.Goal + "\n\n【项目导读（只给你参考，不透露给学习者）】\n" + d.Brief)
	d.Messages = append(d.Messages, llm.Message{Role: "user", Content: userText})
	reply, _, err = t.runAgent(context.Background(), sys, d.Messages, tools)
	if err != nil {
		return "", err
	}
	reply = strings.TrimSpace(reply)
	d.Messages = append(d.Messages, llm.Message{Role: "assistant", Content: reply})
	if strings.Contains(reply, "[CLEAR]") {
		d.Status = "cleared"
		d.ClearedAt = time.Now()
		for i := range d.Stages {
			d.Stages[i].Done = true
		}
	} else if strings.Contains(reply, "[STAGE-DONE]") && cur < len(d.Stages)-1 {
		d.Stages[cur].Done = true
		d.Cur = cur + 1
	}
	d.UpdatedAt = time.Now()
	return reply, nil
}

// briefOfProject 副本携带的项目导读（开启时存入第一条消息的元数据太隐晦，直接存副本）。
