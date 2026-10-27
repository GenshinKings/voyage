package tutor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"voyage/internal/llm"
	"voyage/internal/profile"
	"voyage/internal/reflib"
)

// mockClient 确定性 LLM：replies[0]=课程分解 JSON（ChatJSON 用），此后按调用次序轮转。
// normOut=科目归一化应答（不设则 echo 输入→归一化解析失败回退原名）。
type mockClient struct {
	replies []string
	n       int
	normOut string
}

func (m *mockClient) Chat(system string, msgs []llm.Message) (string, error) {
	m.n++
	if m.n < len(m.replies) {
		return m.replies[m.n], nil
	}
	return m.replies[len(m.replies)-1], nil
}
func (m *mockClient) ChatJSON(system string, msgs []llm.Message) (string, error) {
	if strings.Contains(system, "归一化") {
		if m.normOut != "" {
			return m.normOut, nil
		}
		return msgs[len(msgs)-1].Content, nil
	}
	return m.replies[0], nil
}

func TestSubjectDecomposeAndFlow(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{
		`{"nodes":[{"title":"映射的定义","brief":"能说出映射与函数的关系"},{"title":"单射与满射","brief":"能判别两类映射"},{"title":"复合映射","brief":"能计算 g∘f"}]}`,
		"开场讲解+检查题：映射和函数什么关系？",
		"答对了！加深一下：类型论里的映射呢？\n[PASS]",
		"你说得对，类型论的映射就是函数的推广。还想问什么都可以。",
	}}
	tu := &Tutor{Dir: dir, Client: mc}

	cur, err := tu.StartSubject("映射")
	if err != nil {
		t.Fatal(err)
	}
	if len(cur.Lessons) != 3 {
		t.Fatalf("应分解出 3 个知识点: %d", len(cur.Lessons))
	}
	if cur.Lessons[0].Status != "active" {
		t.Fatal("第一个应为 active")
	}
	cur2, _ := tu.StartSubject("映射")
	if len(cur2.Lessons) != 3 {
		t.Fatal("同科目应幂等")
	}
	today := tu.TodayLesson()
	if today.ID != cur.Lessons[0].ID {
		t.Fatalf("今日知识点错误: %+v", today)
	}
	s, isNew, err := tu.StartOrResume(*today)
	if err != nil || !isNew {
		t.Fatalf("开场失败: %v isNew=%v", err, isNew)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("开场应 2 条消息: %d", len(s.Messages))
	}
	v, err := tu.Send(*today, "函数是特殊的映射：定义域和陪域都是数集")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "pass" {
		t.Fatalf("应判定 pass: %+v", v)
	}
	if !strings.Contains(v.Reply, "[PASS]") {
		t.Fatal("回复应含 [PASS] 标记")
	}
	tu.MarkLessonDone(today.ID)
	cur3 := tu.Curriculum()
	if cur3.Lessons[0].Status != "done" {
		t.Fatal("课程状态应 done")
	}
	if next := tu.TodayLesson(); next == nil || next.ID == today.ID {
		t.Fatalf("今日应推进: %+v", next)
	}
	// 收尾后不锁对话：继续输入走陪聊（不重复判定、不出收尾标记）
	v2, _ := tu.Send(*today, "再讲讲类型论？")
	if v2.Kind != "" {
		t.Fatalf("收尾后陪聊不应再判定: %+v", v2)
	}
	if s := tu.Load(today.ID); s == nil || s.Messages[len(s.Messages)-1].Content != mc.replies[3] {
		t.Fatalf("陪聊回复应落盘: %+v", s)
	}
}

// 答错第一次不给 [PASS]（mock：第一次追问，第二次才 PASS）。
func TestRetryBeforePass(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{
		`{"nodes":[{"title":"A","brief":"b"}]}`,
		"开场+检查题",
		"不对，误区在……再试一次？", // 第 1 答 → 追问
		"这次对了\n[PASS]",  // 第 2 答 → PASS
	}}
	tu := &Tutor{Dir: dir, Client: mc}
	cur, _ := tu.StartSubject("科目Y")
	lesson := cur.Lessons[0]
	if _, _, err := tu.StartOrResume(lesson); err != nil {
		t.Fatal(err)
	}
	v1, _ := tu.Send(lesson, "乱答一")
	if v1.Kind != "" {
		t.Fatalf("第一次应继续对话不判定: %+v", v1)
	}
	if s := tu.Load(lesson.ID); len(s.Messages) != 4 {
		t.Fatalf("追问后应 4 条消息: %d", len(s.Messages))
	}
	v2, _ := tu.Send(lesson, "答对二")
	if v2.Kind != "pass" {
		t.Fatalf("第二次应 pass: %+v", v2)
	}
}

// 会话跨天持久化（重启恢复）。
func TestSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{
		`{"nodes":[{"title":"A","brief":"b"}]}`,
		"开场",
	}}
	tu := &Tutor{Dir: dir, Client: mc}
	cur, _ := tu.StartSubject("科目X")
	lesson := cur.Lessons[0]
	if _, _, err := tu.StartOrResume(lesson); err != nil {
		t.Fatal(err)
	}
	loaded := tu.Load(lesson.ID)
	if loaded == nil || len(loaded.Messages) != 2 {
		t.Fatalf("会话应持久化: %+v", loaded)
	}
	tu2 := &Tutor{Dir: dir, Client: mc}
	if l := tu2.Load(lesson.ID); l == nil || len(l.Messages) != 2 {
		t.Fatal("重启后应恢复会话")
	}
	_ = os.RemoveAll(dir)
}

// 科目归一化：口语→规范学科；单科目在学：开新课=替换旧科目。
func TestStartSubjectNormalizeAndReplace(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{
		normOut: `{"subject":"摄影"}`,
		replies: []string{
			`{"nodes":[{"title":"构图","brief":"能构图"},{"title":"用光","brief":"能用自然光"}]}`,
		},
	}
	tu := &Tutor{Dir: dir, Client: mc}
	cur, err := tu.StartSubject("拍照")
	if err != nil {
		t.Fatal(err)
	}
	if len(cur.Subjects) != 1 || cur.Subjects[0] != "摄影" {
		t.Fatalf("拍照应归一化为单科目摄影: %v", cur.Subjects)
	}
	if len(cur.Lessons) != 2 || cur.Lessons[0].Subject != "摄影" {
		t.Fatalf("摄影课程应替换式建立: %+v", cur.Lessons)
	}
	// 再开「摄影」→ 归一化后同名 → 幂等
	mc2 := &mockClient{normOut: `{"subject":"摄影"}`}
	cur2, err := (&Tutor{Dir: dir, Client: mc2}).StartSubject("摄影")
	if err != nil {
		t.Fatal(err)
	}
	if len(cur2.Lessons) != 2 {
		t.Fatalf("同名科目应幂等: %d", len(cur2.Lessons))
	}
	// 换科目=替换：课程表只剩新科目。替换前先给旧科目开会话（验证会话文件保留）
	if _, _, err := tu.StartOrResume(cur.Lessons[0]); err != nil {
		t.Fatal(err)
	}
	mc3 := &mockClient{
		normOut: `{"subject":"烹饪"}`,
		replies: []string{`{"nodes":[{"title":"刀工","brief":"会切"}]}`},
	}
	cur3, err := (&Tutor{Dir: dir, Client: mc3}).StartSubject("做饭")
	if err != nil {
		t.Fatal(err)
	}
	if len(cur3.Subjects) != 1 || cur3.Subjects[0] != "烹饪" {
		t.Fatalf("替换后应只剩烹饪: %v", cur3.Subjects)
	}
	if len(cur3.Lessons) != 1 || cur3.Lessons[0].Subject != "烹饪" {
		t.Fatalf("课程表应只剩新科目: %+v", cur3.Lessons)
	}
	// 旧科目会话文件保留（历史可从沉淀回看）
	if tu.Load(cur.Lessons[0].ID) == nil {
		t.Fatal("替换后旧科目会话文件应保留")
	}
}

// 归一化失败回退原名。
func TestNormalizeFallback(t *testing.T) {
	mc := &mockClient{normOut: `这不是JSON`}
	tu := &Tutor{Dir: t.TempDir(), Client: mc}
	if got := tu.normalizeSubject("摄影"); got != "摄影" {
		t.Fatalf("解析失败应回退原名: %s", got)
	}
	tu2 := &Tutor{Dir: t.TempDir()}
	if got := tu2.normalizeSubject("拍照"); got != "拍照" {
		t.Fatalf("无 LLM 应回退原名: %s", got)
	}
}

func TestWithAgentsProfileInjection(t *testing.T) {
	dir := t.TempDir()
	agentsPath := filepath.Join(dir, "AGENTS.md")
	profPath := profile.Ensure(dir)

	// 纯模板（无正文）+ 无 AGENTS → system 原样
	tu := &Tutor{Dir: dir, ProfilePath: profPath}
	if got := tu.withAgents("SYS"); got != "SYS" {
		t.Fatalf("空画像+无约束应原样返回: %q", got)
	}

	// 有 AGENTS、画像为空 → 只注入约束
	if err := os.WriteFile(agentsPath, []byte("不要超纲"), 0o644); err != nil {
		t.Fatal(err)
	}
	tu.AgentsPath = agentsPath
	if got := tu.withAgents("SYS"); !strings.Contains(got, "不要超纲") || strings.Contains(got, "后端工程师") {
		t.Fatalf("应只注入约束: %q", got)
	}

	// 两者都有 → 约束在前、画像其后、system 最后
	if err := profile.Save(profPath, profile.Title+"\n\n- 后端工程师\n"); err != nil {
		t.Fatal(err)
	}
	got := tu.withAgents("SYS")
	ia, ip := strings.Index(got, "不要超纲"), strings.Index(got, "后端工程师")
	if ia < 0 || ip < 0 || ia > ip {
		t.Fatalf("约束应先于画像: %q", got)
	}
	if !strings.HasSuffix(got, "SYS") {
		t.Fatal("system prompt 应保持在最后")
	}
}

func TestWithAgentsRefLibInjection(t *testing.T) {
	dir := t.TempDir()
	refDir := reflib.Ensure(dir)
	tu := &Tutor{Dir: dir, ProfilePath: profile.Ensure(dir), RefDir: refDir}
	// 空参考库 → 不注入
	if got := tu.withAgents("SYS"); strings.Contains(got, "教学参考库") {
		t.Fatalf("空参考库不应注入: %q", got)
	}
	// 入库一篇 → 注入标题与摘要
	if err := reflib.Add(refDir, reflib.Doc{ID: "20261024-demo", Title: "安全审计 Skill", Digest: "六阶段流水线"}, "# 原文"); err != nil {
		t.Fatal(err)
	}
	got := tu.withAgents("SYS")
	if !strings.Contains(got, "教学参考库") || !strings.Contains(got, "《安全审计 Skill》") || !strings.Contains(got, "六阶段流水线") {
		t.Fatalf("应注入参考库清单: %q", got)
	}
	if !strings.HasSuffix(got, "SYS") {
		t.Fatal("system prompt 应保持在最后")
	}
}

func TestDistillProfile(t *testing.T) {
	dir := t.TempDir()
	mc := &mockClient{replies: []string{profile.Title + "\n\n- 后端工程师，目标转 FDE\n- 每周 ~7 小时\n"}}
	tu := &Tutor{Dir: dir, Client: mc, ProfilePath: profile.Ensure(dir)}
	sess := &DirectionSession{Messages: []llm.Message{
		{Role: "user", Content: "我是后端工程师，想转 FDE"},
		{Role: "assistant", Content: "每周能投入多少时间？"},
		{Role: "user", Content: "每周大概 7 小时"},
	}}
	if err := tu.DistillProfile(sess); err != nil {
		t.Fatal(err)
	}
	if got := profile.Load(tu.ProfilePath); !strings.Contains(got, "FDE") {
		t.Fatalf("蒸馏画像应落盘: %q", got)
	}
	if err := tu.DistillProfile(nil); err == nil {
		t.Fatal("无会话记录应报错")
	}
}
