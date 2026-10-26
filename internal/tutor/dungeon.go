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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"outpost2/internal/llm"
)

// DStage 一关：一个用已学知识能完成的小产出。
type DStage struct {
	Title string `json:"title"`
	Goal  string `json:"goal"`
	Done  bool   `json:"done"`
}

// Dungeon 一个副本（持久化 dungeons.json）。
type Dungeon struct {
	ID        string        `json:"id"`
	ReadingID string        `json:"readingId"`
	Repo      string        `json:"repo"`
	Project   string        `json:"project"` // 项目真名——通关（CLEAR）前前端显示「？？？」
	Brief     string        `json:"brief"`   // 开启时的项目导读（导师上下文）
	Status    string        `json:"status"`  // active|cleared|abandoned
	Stages    []DStage      `json:"stages"`
	Cur       int           `json:"cur"` // 当前关卡下标
	Messages  []llm.Message `json:"messages"`
	StartedAt time.Time     `json:"startedAt"`
	ClearedAt time.Time     `json:"clearedAt,omitempty"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Sealed 副本还在进行中（项目名对用户保密）。
func (d *Dungeon) Sealed() bool { return d.Status == "active" }

// ---------- 副本设计师：校验重合度 + 生成关卡计划 ----------

const dungeonDesignSystem = `你是前哨的「副本设计师」。学习者刚在主课程收尾了一些知识点，想用一个真实项目复现它们——你负责判断这件事是否可行，可行就设计关卡计划。

输入：学习者已收尾的知识点、目标项目（含助教已写好的项目导读）。

第一步，判断重合度：这个项目里至少一个核心环节，能否用学习者已收尾的知识点完成？重合度不足（需要大量未学概念）时输出：
{"match":false,"missing":["缺失的知识点","…"],"reason":"一句话说明为什么现在还不适合"}

重合度足够则输出：
{"match":true,"project":"项目真实名称（通关时揭晓用，≤12字）","stages":[{"title":"关卡名（≤12字，不出现项目名）","goal":"这一关要做出什么可验证的小产出（≤50字，点名用到哪个已学知识点）"}],"opening":"第一关的开场引导（≤120字：欢迎语+第一关要做什么+一条起步提示，语气像游戏 NPC，不说破项目全貌）"}

关卡设计纪律：
- 3-5 关，循序渐进：每一关的产出都只依赖已收尾知识点 + 前面关卡的结果
- 第一关必须小到 20 分钟内有成就感；最后一关合起来就是项目的核心最小复现
- 关卡名不出现项目名/品牌名——保持悬念
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
		Match   bool     `json:"match"`
		Missing []string `json:"missing"`
		Reason  string   `json:"reason"`
		Project string   `json:"project"`
		Stages  []DStage `json:"stages"`
		Opening string   `json:"opening"`
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
	opening := out.Opening
	if opening == "" {
		opening = "副本开启。第一关：" + out.Stages[0].Goal
	}
	d.Messages = append(d.Messages,
		llm.Message{Role: "assistant", Content: "⚔ 副本开启\n\n" + opening})
	return d, "", nil
}

// ---------- 副本导师：逐关带做 ----------

const dungeonMasterSystem = `你是前哨的「副本导师」，正在带学习者一关一关复现一个真实项目。

规则（恍然大悟机制的核心）：
- 绝不透露项目的名字与全貌——学习者问「这到底是什么项目」时回答：「通关后你会知道。现在专注当前这一关。」
- 每次回复聚焦当前关卡：要做出什么、用哪个他学过的知识点（点名唤起记忆）、卡住了给一步提示（不写完整代码，除非他连错三次）
- 学习者提交成果时验证：达到本关目标 → 先肯定，然后单独一行输出 [STAGE-DONE]；若这已是最后一关，再单独一行输出 [CLEAR] 和揭晓段（见下）；未达标 → 具体指出差在哪（关联到知识点的哪个环节），让他再试
- 最后一关达成时，揭晓段格式：
  🎉 恭喜你，你实现了「项目名」！
  回顾：这个项目其实只用了你学过的 xxx、xxx、xxx——你早就具备完成它的能力。
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
	reply, _, err = t.runAgent(sys, d.Messages, tools)
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
