package tutor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voyage/internal/llm"
)

// ---------- capClient：捕获 system、按序弹出回复（区别于 tutor_test.go 的轮转 mock） ----------

type capClient struct {
	replies   []string // 普通调用弹出一条；空则报错
	systems   []string // 捕获每次调用的 system prompt
	normOut   string   // 归一化应答（ChatJSON 且 system 含「归一化」时返回）
	decompose string   // 课程分解应答（StartSubject 专用，不进弹出队列）
}

func (c *capClient) Chat(system string, _ []llm.Message) (string, error) {
	c.systems = append(c.systems, system)
	if len(c.replies) == 0 {
		return "", fmt.Errorf("mock 无更多回复")
	}
	r := c.replies[0]
	c.replies = c.replies[1:]
	return r, nil
}

func (c *capClient) ChatJSON(system string, msgs []llm.Message) (string, error) {
	c.systems = append(c.systems, system)
	if strings.Contains(system, "归一化") && c.normOut != "" {
		return c.normOut, nil
	}
	if strings.Contains(system, "课程设计师") {
		if c.decompose == "" {
			return "", fmt.Errorf("mock 无分解应答")
		}
		return c.decompose, nil
	}
	return c.Chat(system, msgs)
}

// newLessonTutor 建一门单课科目并返回 tutor/lesson/mock——省去每个用例重复样板。
// StartSubject 的归一化/分解走 capClient 专用字段，不消耗弹出队列。
func newLessonTutor(t *testing.T, budget string, cc *capClient) (*Tutor, Lesson) {
	t.Helper()
	cc.decompose = `{"nodes":[{"title":"甲","brief":"会甲"}]}`
	if cc.normOut == "" {
		cc.normOut = `{"subject":"科目Z"}` // 归一化不耗弹出队列
	}
	tu := &Tutor{Dir: t.TempDir(), Client: cc, TimeBudget: budget}
	cur, err := tu.StartSubject("科目Z")
	if err != nil {
		t.Fatal(err)
	}
	return tu, cur.Lessons[0]
}

// ---------- (1) 时间预算分支：开场与 Send 的约束真的不同 ----------

func TestTimeBudgetChangesOpening(t *testing.T) {
	want := map[string]string{
		"2":  "两分钟版",
		"10": "只有 10 分钟",
		"30": "半小时",
	}
	seen := map[string]string{}
	for budget, keyword := range want {
		cc := &capClient{replies: []string{"开场"}}
		tu, lesson := newLessonTutor(t, budget, cc)
		if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
			t.Fatal(err)
		}
		sys := cc.systems[len(cc.systems)-1] // StartOrResume 是最近一次调用
		if !strings.Contains(sys, keyword) {
			t.Errorf("budget=%s 的开场 system 应含 %q", budget, keyword)
		}
		seen[budget] = sys
		// 预算段之外，各档都带样例档引导（首触默认）
		if !strings.Contains(sys, "引导档位·样例（第 1 档）") {
			t.Errorf("budget=%s 首触应注入样例档", budget)
		}
	}
	// 90/未填：三个关键词都不出现
	for _, budget := range []string{"90", ""} {
		cc := &capClient{replies: []string{"开场"}}
		tu, lesson := newLessonTutor(t, budget, cc)
		if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
			t.Fatal(err)
		}
		sys := cc.systems[0]
		for _, kw := range want {
			if strings.Contains(sys, kw) {
				t.Errorf("budget=%q 不应含预算约束 %q", budget, kw)
			}
		}
		seen[budget] = sys
	}
	// 「真的不同」：2/10/30 两两互异；90 与空走同一 default 分支（设计使然，允许相同）
	keys := []string{"2", "10", "30"}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if seen[keys[i]] == seen[keys[j]] {
				t.Errorf("预算 %q 与 %q 的开场 system 相同——分支没生效", keys[i], keys[j])
			}
		}
	}
	if seen["90"] != seen[""] {
		t.Error("90 与未填应同为无预算段（default 分支）")
	}
}

func TestTimeBudgetChangesSend(t *testing.T) {
	cc := &capClient{replies: []string{"开场", "回复10"}}
	tu, lesson := newLessonTutor(t, "10", cc)
	if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
		t.Fatal(err)
	}
	if _, err := tu.Send(context.Background(), lesson, "答"); err != nil {
		t.Fatal(err)
	}
	sendSys := cc.systems[len(cc.systems)-1] // Send 是最近一次调用
	if !strings.Contains(sendSys, "只有 10 分钟") || !strings.Contains(sendSys, "一句话讲完") {
		t.Fatalf("Send 的 10 分钟约束应生效: %q", sendSys)
	}

	cc2 := &capClient{replies: []string{"开场", "回复90"}}
	tu2, lesson2 := newLessonTutor(t, "90", cc2)
	if _, _, err := tu2.StartOrResume(context.Background(), lesson2); err != nil {
		t.Fatal(err)
	}
	if _, err := tu2.Send(context.Background(), lesson2, "答"); err != nil {
		t.Fatal(err)
	}
	if s := cc2.systems[1]; strings.Contains(s, "一句话讲完") {
		t.Fatalf("90 档 Send 不应有 10 分钟约束: %q", s)
	}
	if cc2.systems[1] == sendSys {
		t.Fatal("10 与 90 的 Send system 不应相同")
	}
}

// ---------- (2) 渐隐引导档位：连 3 过升、2 错/卡住降 ----------

// setupAtGuide 让「人」到达指定引导档位（跨课程累计——每课一次判定；连 3 过升一档）。
// 返回科目全部课（未消耗的从指定档继续用）。
func setupAtGuide(t *testing.T, target string) (*Tutor, []Lesson, *capClient) {
	t.Helper()
	need := map[string]int{"1": 0, "2": 3, "3": 6}[target]
	total := need + 4 // 额外 4 节留给降档/卡住/半懂用例
	nodes := ""
	for i := 0; i < total; i++ {
		nodes += fmt.Sprintf(`{"title":"第%d点","brief":"b"},`, i+1)
	}
	nodes = strings.TrimSuffix(nodes, ",")
	cc := &capClient{normOut: `{"subject":"科目Z"}`}
	cc.decompose = `{"nodes":[` + nodes + `]}`
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	cur, err := tu.StartSubject("科目Z")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < need; i++ {
		l := cur.Lessons[i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "好\n[PASS]")
		if _, err := tu.Send(context.Background(), l, "答"); err != nil {
			t.Fatal(err)
		}
	}
	return tu, cur.Lessons, cc
}

func TestGuideAscendsAfter3Passes(t *testing.T) {
	cc := &capClient{normOut: `{"subject":"科目Z"}`}
	cc.decompose = `{"nodes":[{"title":"A","brief":"b"},{"title":"B","brief":"b"},{"title":"C","brief":"b"},{"title":"D","brief":"b"},{"title":"E","brief":"b"},{"title":"F","brief":"b"}]}`
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	cur, err := tu.StartSubject("科目Z")
	if err != nil {
		t.Fatal(err)
	}
	// 每课一次判定：3 连过 → 样例升半提示
	for i := 0; i < 3; i++ {
		l := cur.Lessons[i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "好\n[PASS]")
		v, err := tu.Send(context.Background(), l, "答")
		if err != nil {
			t.Fatal(err)
		}
		if v.Kind != "pass" {
			t.Fatalf("第 %d 课应 pass: %+v", i+1, v)
		}
		wantGuide := 1
		if i == 2 {
			wantGuide = 2
		}
		if v.Guide != wantGuide {
			t.Fatalf("第 %d 课后档位应 %d，got %d", i+1, wantGuide, v.Guide)
		}
	}
	if tu.GuideLevel != 2 {
		t.Fatalf("人级档位应 2，got %d", tu.GuideLevel)
	}
	// 再 3 连过 → 半提示升提问
	for i := 3; i < 6; i++ {
		l := cur.Lessons[i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "更好\n[PASS]")
		v, _ := tu.Send(context.Background(), l, "答")
		// 第 4 课（累计 4 连过，含新的连续 3 过）即触发第二次升档；此后封顶提问档
		if v.Guide != 3 {
			t.Fatalf("第 %d 课后应升到提问档，got %d", i+1, v.Guide)
		}
	}
	// 档位注入随人级变化：第 7 课（新知识点）开场即用提问档
	if cur2 := tu.Curriculum(); len(cur2.Lessons) < 6 {
		t.Fatal("课表应 6 课")
	}
}

func TestGuideDescendsAfter2Fails(t *testing.T) {
	tu, lessons, cc := setupAtGuide(t, "3") // 提问档
	// 连续 2 课判定失败（带 [CAUSE] 标签）→ 退回半提示
	for i := 0; i < 2; i++ {
		l := lessons[6+i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "不对——[CAUSE:concept|没懂映射方向]")
		v, err := tu.Send(context.Background(), l, "错答")
		if err != nil {
			t.Fatal(err)
		}
		if v.Kind != "" {
			t.Fatalf("无 [PASS] 不应判定: %+v", v)
		}
	}
	if tu.GuideLevel != 2 {
		t.Fatalf("2 错应降到半提示档，got %d", tu.GuideLevel)
	}
	// 再 2 错 → 回样例档（保底）
	for i := 0; i < 2; i++ {
		l := lessons[8+i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "[CAUSE:slip|拼写]")
		if _, err := tu.Send(context.Background(), l, "错答"); err != nil {
			t.Fatal(err)
		}
	}
	if tu.GuideLevel != 1 {
		t.Fatalf("4 错应回样例档（保底），got %d", tu.GuideLevel)
	}
}

func TestGuideStuckImmediatelyDemotes(t *testing.T) {
	tu, lessons, cc := setupAtGuide(t, "2") // 半提示档
	l := lessons[3]
	cc.replies = append(cc.replies, "开场占位")
	if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	cc.replies = append(cc.replies, "给完整样例：先建 main.go……")
	if _, err := tu.Send(context.Background(), l, "(我卡住了) 完全不会"); err != nil {
		t.Fatal(err)
	}
	if tu.GuideLevel != 1 {
		t.Fatalf("卡住应立即退回样例档，got %d", tu.GuideLevel)
	}
	if s := cc.systems[len(cc.systems)-1]; !strings.Contains(s, "我卡住了") {
		t.Fatalf("卡住轮 system 应含退档提示: %q", s)
	}
	// 卡住轮同时应注入降档后的样例话术
	if s := cc.systems[len(cc.systems)-1]; !strings.Contains(s, "引导档位·样例（第 1 档）") {
		t.Fatalf("降档后应按样例档注入: %q", s)
	}
}

func TestGuidePartialResetsStreak(t *testing.T) {
	tu, lessons, cc := setupAtGuide(t, "2") // 半提示档（3 连过攒的）
	// 半懂：清零人级连败但不升降
	l := lessons[3]
	cc.replies = append(cc.replies, "开场占位")
	if _, _, err := tu.StartOrResume(context.Background(), l); err != nil {
		t.Fatal(err)
	}
	cc.replies = append(cc.replies, "半对\n[PASS:partial]")
	v, _ := tu.Send(context.Background(), l, "半答")
	if v.Kind != "partial" {
		t.Fatalf("应判 partial: %+v", v)
	}
	if tu.GuideLevel != 2 || tu.GuideStreak != [2]int{0, 0} {
		t.Fatalf("partial 应清零连击保持档位: guide=%d streak=%v", tu.GuideLevel, tu.GuideStreak)
	}
	// 清零后 2 连过不升档（差一次）
	for i := 0; i < 2; i++ {
		l2 := lessons[4+i]
		cc.replies = append(cc.replies, "开场占位")
		if _, _, err := tu.StartOrResume(context.Background(), l2); err != nil {
			t.Fatal(err)
		}
		cc.replies = append(cc.replies, "过\n[PASS]")
		if _, err := tu.Send(context.Background(), l2, "过答"); err != nil {
			t.Fatal(err)
		}
	}
	if tu.GuideLevel != 2 {
		t.Fatalf("2 过不应升档，got %d", tu.GuideLevel)
	}
}

// ---------- (3) 收尾判定、盲区自查、追问判真会 ----------

func TestPassParsingKeepsBlindSpotLine(t *testing.T) {
	cc := &capClient{replies: []string{"开场"}}
	tu, lesson := newLessonTutor(t, "90", cc)
	if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
		t.Fatal(err)
	}
	// 盲区自查行在 [PASS] 标记之前——必须原样保留给学习者决定
	blind := "【还有没讲的：复合映射的结合律——要继续学吗？】\n关键点你都答对了。\n[PASS]"
	cc.replies = append(cc.replies, blind)
	v, err := tu.Send(context.Background(), lesson, "答")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "pass" {
		t.Fatalf("应判 pass: %+v", v)
	}
	if !strings.Contains(v.Reply, "【还有没讲的：复合映射的结合律") {
		t.Fatalf("盲区自查行不应被吞: %q", v.Reply)
	}
	if s := tu.Load(lesson.ID); s.Verdict != "pass" {
		t.Fatalf("会话判定应 pass: %+v", s.Verdict)
	}
}

func TestPartialMarkerWins(t *testing.T) {
	cc := &capClient{replies: []string{"开场"}}
	tu, lesson := newLessonTutor(t, "90", cc)
	if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
		t.Fatal(err)
	}
	// 两个标记同时出现（模型口嗨）：半懂优先
	cc.replies = append(cc.replies, "大体对\n[PASS]\n但有一处\n[PASS:partial]")
	v, _ := tu.Send(context.Background(), lesson, "答")
	if v.Kind != "partial" {
		t.Fatalf("partial 应优先于 pass: %+v", v)
	}
}

func TestAskWhyFlowPassAndFuzzy(t *testing.T) {
	cc := &capClient{replies: []string{"开场", "收尾判定\n[PASS]"}}
	tu, lesson := newLessonTutor(t, "90", cc)
	if _, _, err := tu.StartOrResume(context.Background(), lesson); err != nil {
		t.Fatal(err)
	}
	if _, err := tu.Send(context.Background(), lesson, "答"); err != nil {
		t.Fatal(err)
	}
	// 首次通过 → 追问挂起
	tu.AskRealUse(lesson.ID)
	s := tu.Load(lesson.ID)
	if !s.AskWhy {
		t.Fatal("AskRealUse 后应挂起 AskWhy")
	}
	last := s.Messages[len(s.Messages)-1]
	if !strings.Contains(last.Content, "你为什么会这么写") {
		t.Fatalf("应追加追问消息: %q", last.Content)
	}
	// 答得上 → fu-pass（AskWhy 清除，判定保持 pass）
	cc.replies = append(cc.replies, "因为映射要求陪域整体被覆盖，所以是满射\n[FU-PASS]")
	v, err := tu.Send(context.Background(), lesson, "因为函数要求每个输入有唯一输出")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "fu-pass" {
		t.Fatalf("应判 fu-pass: %+v", v)
	}
	if s = tu.Load(lesson.ID); s.AskWhy || s.Verdict != "pass" {
		t.Fatalf("fu-pass 后应清挂起保持判定: %+v", s)
	}
	// 普通 FU-FUZZY 路径（另起会话验证）：判定降 partial
	cc2 := &capClient{replies: []string{"开场", "收尾\n[PASS]"}}
	tu2, lesson2 := newLessonTutor(t, "90", cc2)
	if _, _, err := tu2.StartOrResume(context.Background(), lesson2); err != nil {
		t.Fatal(err)
	}
	if _, err := tu2.Send(context.Background(), lesson2, "答"); err != nil {
		t.Fatal(err)
	}
	tu2.AskRealUse(lesson2.ID)
	cc2.replies = append(cc2.replies, "说不上来？那从更小的题开始：只判断 f(x)=2x 是不是映射。\n[FU-FUZZY]")
	v2, err := tu2.Send(context.Background(), lesson2, "呃……不知道，看着像")
	if err != nil {
		t.Fatal(err)
	}
	if v2.Kind != "fu-fuzzy" {
		t.Fatalf("应判 fu-fuzzy: %+v", v2)
	}
	if s := tu2.Load(lesson2.ID); s.AskWhy || s.Verdict != "partial" {
		t.Fatalf("fu-fuzzy 后判定应降 partial 并清挂起: askWhy=%v verdict=%q", s.AskWhy, s.Verdict)
	}
	// 不存在的会话：AskRealUse 静默返回（不 panic）
	tu.AskRealUse("no-such-id")
}

// ---------- 零覆盖函数补测：Reflect / SelfIterate / Dungeon / Hatch / Direction / 其他 ----------

func TestReflectParsesAndSanitizes(t *testing.T) {
	// 无 LLM 报错
	if _, err := (&Tutor{Dir: t.TempDir()}).Reflect("数据", "清单"); err == nil {
		t.Fatal("无 LLM 应报错")
	}
	// 正常解析 + 清洗：空观察剔除、非法 priority 归 mid
	cc := &capClient{replies: []string{`{"observations":[
		{"observation":" FSRS 讲不清 ","gap":"缺例子","suggestion":"加 worked example","priority":"high"},
		{"observation":"","gap":"空","suggestion":"剔除我","priority":"high"},
		{"observation":"出题偏难","gap":"难度带未接","suggestion":"接 Elo","priority":"超纲词"}
	]}`}}
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	obs, err := tu.Reflect("今日数据", "旧清单")
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("空观察应剔除，剩 2 条: %+v", obs)
	}
	if obs[0].Observation != "FSRS 讲不清" || obs[0].Priority != "high" {
		t.Fatalf("应 trim 并保留 high: %+v", obs[0])
	}
	if obs[1].Priority != "mid" {
		t.Fatalf("非法 priority 应归 mid: %+v", obs[1])
	}
	// 坏 JSON
	cc2 := &capClient{replies: []string{`not json`}}
	if _, err := (&Tutor{Dir: t.TempDir(), Client: cc2}).Reflect("d", "e"); err == nil || !strings.Contains(err.Error(), "反思 JSON 解析失败") {
		t.Fatalf("坏 JSON 应报解析失败，got %v", err)
	}
}

func TestSelfIterateRequiresToolsAndRuns(t *testing.T) {
	obs := Observation{Observation: "卡", Gap: "缺", Suggestion: "改"}
	if _, _, err := (&Tutor{Dir: t.TempDir()}).SelfIterate(obs, nil); err == nil {
		t.Fatal("无 LLM 应报错")
	}
	cc := &capClient{}
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	if _, _, err := tu.SelfIterate(obs, nil); err == nil || !strings.Contains(err.Error(), "没有可用工具") {
		t.Fatalf("无工具应报错，got %v", err)
	}
	// mock 未实现 ToolClient → runAgent 降级普通 Chat（覆盖 fallback 路径）
	cc.replies = []string{"实现报告：改了 internal/x，测试绿。"}
	report, _, err := tu.SelfIterate(obs, []Tool{{Name: "read_file"}})
	if err != nil || !strings.Contains(report, "实现报告") {
		t.Fatalf("降级路径应可用: %q %v", report, err)
	}
	if s := cc.systems[0]; !strings.Contains(s, "低峰时段自主迭代") || !strings.Contains(s, "只改 internal/") {
		t.Fatalf("自迭代纪律应注入: %q", s)
	}
}

func TestDungeonStartAndSend(t *testing.T) {
	// 无 LLM
	if _, _, err := (&Tutor{Dir: t.TempDir()}).DungeonStart("ctx", "repo", "brief"); err == nil {
		t.Fatal("无 LLM 应报错")
	}
	// match=false → 返回 missing 文案
	cc := &capClient{replies: []string{`{"match":false,"missing":["FSRS"],"reason":"还缺核心概念"}`}}
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	d, missing, err := tu.DungeonStart("已学：映射", "todo-app", "导读")
	if err != nil || d != nil || !strings.Contains(missing, "还缺核心概念") || !strings.Contains(missing, "FSRS") {
		t.Fatalf("不匹配应返回 missing: %q %v", missing, err)
	}
	// match=true → 副本建立（Sealed、档位、开场）
	cc.replies = []string{`{"match":true,"project":"TaskFlow","stars":9,"req":[{"n":"映射"},{"n":"单射","miss":true}],"stages":[{"title":"骨架","goal":"建目录"},{"title":"核心","goal":"实现主流程"}],"opening":"欢迎，冒险者。"}`}
	d, missing, err = tu.DungeonStart("已学", "repo", "brief")
	if err != nil || missing != "" || d == nil {
		t.Fatalf("match=true 应建副本: %v %q", err, missing)
	}
	if !d.Sealed() {
		t.Fatal("active 副本应处于 Sealed 状态")
	}
	if d.Stars != 3 || d.Tier != TierStretch {
		t.Fatalf("stars 应钳制到 3、缺 1 项应拉伸档: stars=%d tier=%d", d.Stars, d.Tier)
	}
	if !strings.Contains(d.Messages[0].Content, "欢迎，冒险者。") {
		t.Fatalf("自定义开场应保留: %q", d.Messages[0].Content)
	}
	// 设计不完整（无关卡）→ 报错
	cc.replies = []string{`{"match":true,"project":"X","stages":[]}`}
	if _, _, err := tu.DungeonStart("c", "r", "b"); err == nil || !strings.Contains(err.Error(), "无关卡") {
		t.Fatalf("无关卡应报错，got %v", err)
	}
	// 坏 JSON
	cc.replies = []string{`{bad`}
	if _, _, err := tu.DungeonStart("c", "r", "b"); err == nil || !strings.Contains(err.Error(), "副本设计解析失败") {
		t.Fatalf("坏 JSON 应报解析失败，got %v", err)
	}
	// DungeonSend：推进与通关
	cc.replies = []string{"第一步先建 main.go", "过关！\n[STAGE-DONE]", "恭喜，通关！\n[CLEAR]"}
	if _, err := tu.DungeonSend(d, "开始", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := tu.DungeonSend(d, "做完了", nil); err != nil {
		t.Fatal(err)
	}
	if d.Cur != 1 || !d.Stages[0].Done {
		t.Fatalf("STAGE-DONE 应推进游标: cur=%d", d.Cur)
	}
	if _, err := tu.DungeonSend(d, "最后一关完成", nil); err != nil {
		t.Fatal(err)
	}
	if d.Status != "cleared" || d.Sealed() {
		t.Fatal("CLEAR 应通关揭晓")
	}
	for _, st := range d.Stages {
		if !st.Done {
			t.Fatal("通关应全关 done")
		}
	}
	// 已结束的副本再发送 → 报错
	if _, err := tu.DungeonSend(d, "再来", nil); err == nil || !strings.Contains(err.Error(), "副本已结束") {
		t.Fatalf("已结束副本应报错，got %v", err)
	}
	// 默认开场（LLM 没给 opening）
	cc.replies = []string{`{"match":true,"project":"Y","stages":[{"title":"一","goal":"做什么"}]}`}
	d2, _, err := tu.DungeonStart("c", "r", "b")
	if err != nil || !strings.Contains(d2.Messages[0].Content, "第一关：做什么") {
		t.Fatalf("无 opening 应给默认开场: %q %v", d2.Messages[0].Content, err)
	}
}

func TestHatchAndTierOf(t *testing.T) {
	if _, _, err := (&Tutor{Dir: t.TempDir()}).Hatch("c", "b"); err == nil {
		t.Fatal("无 LLM 应报错")
	}
	cc := &capClient{replies: []string{`{"stars":0,"req":[{"n":"a"},{"n":"b"},{"n":"c","miss":true},{"n":"d","miss":true},{"n":"e","miss":true}],"impact":" 改变 x 的底座 "}`}}
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	m, impact, err := tu.Hatch("已学", "导读")
	if err != nil {
		t.Fatal(err)
	}
	if m.Stars != 2 || m.Tier != TierLeap || impact != "改变 x 的底座" {
		t.Fatalf("stars 钳制/Tier 跃迁/impact trim: %+v %q", m, impact)
	}
	// 空 req 报错
	cc.replies = []string{`{"stars":2,"req":[]}`}
	if _, _, err := tu.Hatch("c", "b"); err == nil || !strings.Contains(err.Error(), "无需求点") {
		t.Fatalf("空 req 应报错，got %v", err)
	}
	// TierOf 边界
	if TierOf(nil) != TierConsolidate || TierOf([]ReqNode{{N: "a"}}) != TierConsolidate {
		t.Fatal("0 缺=巩固")
	}
	if TierOf([]ReqNode{{Miss: true}}) != TierStretch || TierOf([]ReqNode{{Miss: true}, {Miss: true}}) != TierStretch {
		t.Fatal("1-2 缺=拉伸")
	}
	if TierOf([]ReqNode{{Miss: true}, {Miss: true}, {Miss: true}}) != TierLeap {
		t.Fatal("3+ 缺=跃迁")
	}
}

func TestDirectionSendProposeAffirmVoid(t *testing.T) {
	if _, _, err := (&Tutor{Dir: t.TempDir()}).DirectionSend(""); err == nil {
		t.Fatal("空输入应报错")
	}
	cc := &capClient{replies: []string{
		"想确认一下……\n[PROPOSE]\n{\"subject\":\"摄影\",\"lessons\":[{\"title\":\"构图\",\"brief\":\"会构图\"}]}",
	}}
	tu := &Tutor{Dir: t.TempDir(), Client: cc}
	reply, p, err := tu.DirectionSend("我想学摄影")
	if err != nil || p == nil || p.Subject != "摄影" || len(p.Lessons) != 1 {
		t.Fatalf("应解析提案: %q %+v %v", reply, p, err)
	}
	// 确认词短路：不再调 LLM（mock 再无回复也不报错）
	affirm, p2, err := tu.DirectionSend("就这个，可以了")
	if err != nil || p2 == nil || !strings.Contains(affirm, "同步到学习") {
		t.Fatalf("确认应短路返回: %q %+v %v", affirm, p2, err)
	}
	if len(cc.systems) != 1 {
		t.Fatalf("确认不应触发 LLM（调用 %d 次）", len(cc.systems))
	}
	// [PROPOSE-VOID] 清提案
	cc.replies = []string{"明白，那是测试\n[PROPOSE-VOID]"}
	if _, p3, err := tu.DirectionSend("刚才是测试"); err != nil || p3 != nil {
		t.Fatalf("VOID 应清提案: %+v %v", p3, err)
	}
	// 持久化往返 + Reset
	if tu.LoadDirection() == nil {
		t.Fatal("方向会话应落盘")
	}
	tu.ResetDirection()
	if tu.LoadDirection() != nil {
		t.Fatal("Reset 后应无会话")
	}
}

func TestProjectBriefCurateReviewNotes(t *testing.T) {
	dir := t.TempDir()
	cc := &capClient{replies: []string{"报告A", "报告B", "诊断C"}}
	tu := &Tutor{Dir: dir, Client: cc}
	// 工具缺失各自报错
	if _, err := tu.ProjectBrief("ctx", "repo", nil); err == nil {
		t.Fatal("ProjectBrief 无工具应报错")
	}
	if _, err := tu.CurateOnce("ctx", nil); err == nil || !strings.Contains(err.Error(), "没有可用工具") {
		t.Fatal("CurateOnce 无工具应报错")
	}
	// mock 未实现 ToolClient → 降级 Chat，工具参数仅影响纪律注入
	if got, err := tu.ProjectBrief("ctx", "repo", []Tool{{Name: "gh_browse"}}); err != nil || got != "报告A" {
		t.Fatalf("ProjectBrief: %q %v", got, err)
	}
	if got, err := tu.CurateOnce("ctx", []Tool{{Name: "gh_browse"}}); err != nil || got != "报告B" {
		t.Fatalf("CurateOnce: %q %v", got, err)
	}
	if s := cc.systems[1]; !strings.Contains(s, "进货") {
		t.Fatalf("进货纪律应注入: %q", s)
	}
	// ReviewNotes：无笔记报错；有笔记出报告
	if _, err := tu.ReviewNotes("ctx"); err == nil || !strings.Contains(err.Error(), "还没有笔记") {
		t.Fatalf("空 notes 应报错，got %v", err)
	}
	notesDir := filepath.Join(dir, "notes")
	os.MkdirAll(notesDir, 0o755)
	os.WriteFile(filepath.Join(notesDir, "a.md"), []byte("# 笔记\n映射是特殊的函数"), 0o644)
	tu.NotesDir = notesDir
	got, err := tu.ReviewNotes("ctx")
	if err != nil || got != "诊断C" {
		t.Fatalf("ReviewNotes: %q %v", got, err)
	}
	// 巡检输出为空报错
	cc.replies = []string{"   "}
	if _, err := tu.ReviewNotes("ctx"); err == nil || !strings.Contains(err.Error(), "巡检输出为空") {
		t.Fatalf("空输出应报错，got %v", err)
	}
}
