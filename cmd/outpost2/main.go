// outpost2 CLI — 前哨 2.0 内核的命令行出口（引擎拼装为可执行工具）。
//
// 子命令：
//   plan        -in plan.json           生成今日计划（丢清单）
//   evidence    -in events.json         记录学习事件 → 履历（record 模式）
//   evidence    -export -owner 名        导出证据页 HTML
//   kb-import1x -in readingdocs.json    1.x 阅读书架迁入知识库①层
//   kb-export                           导出知识库（CC-BY-SA + 署名）
//   sandbox     -case id -dir ws        开沙盒（写案例文件）
//   sandbox     -case id -dir ws -check 验收修复
//   graduate    -in stats.json          毕业状态检查
//
// 数据持久化：-data 目录（默认 ./outpost2-data）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"outpost2/internal/ability"
	"outpost2/internal/bandit"
	"outpost2/internal/evidence"
	"outpost2/internal/graduation"
	"outpost2/internal/kb"
	"outpost2/internal/sandbox"
	"outpost2/internal/scheduler"
	"time"
)

const dataDir = "outpost2-data"

func die(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}

func loadJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err != nil {
		die(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		die(err)
	}
}

func saveJSON(path string, v any) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		die(err)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println(`outpost2 — 全托管学习机内核
用法: outpost2 <plan|evidence|kb-import1x|kb-export|sandbox|graduate> [flags]`)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "plan":
		cmdPlan(os.Args[2:])
	case "evidence":
		cmdEvidence(os.Args[2:])
	case "kb-import1x":
		cmdKBImport1x(os.Args[2:])
	case "kb-export":
		cmdKBExport()
	case "sandbox":
		cmdSandbox(os.Args[2:])
	case "graduate":
		cmdGraduate(os.Args[2:])
	default:
		die(fmt.Errorf("未知子命令: %s", os.Args[1]))
	}
}

func cmdPlan(args []string) {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	in := fs.String("in", "", "输入 JSON：{deliverables:[{id,title,deadline,progress,actions}], due:[{id,due,title}], energy:0|1|2, extras:[{id,title,domain}], context:\"\"}")
	fs.Parse(args)
	if *in == "" {
		die(fmt.Errorf("需要 -in"))
	}
	var req struct {
		Deliverables []scheduler.Deliverable `json:"deliverables"`
		Due          []scheduler.ReviewCard  `json:"due"`
		Energy       int                     `json:"energy"`
		Extras       []scheduler.Extra       `json:"extras"`
		Context      string                  `json:"context"`
	}
	loadJSON(*in, &req)
	b := bandit.New(timeSeed())
	shape := scheduler.ResolveShape(scheduler.Energy(req.Energy), b, req.Context)
	eff := applyShapeEff(scheduler.Energy(req.Energy), shape)
	plan := scheduler.Plan(now(), req.Deliverables, req.Due, eff, req.Extras)
	plan.Note = "形态:" + string(shape) + " | " + plan.Note
	out, _ := json.MarshalIndent(plan, "", "  ")
	fmt.Println(string(out))
	saveJSON(dataDir+"/last-plan.json", plan)
}

func applyShapeEff(self scheduler.Energy, shape scheduler.PlanShape) scheduler.Energy {
	switch shape {
	case scheduler.ShapeSprint:
		return scheduler.EnergyHigh
	case scheduler.ShapeMaintain:
		return scheduler.EnergyLow
	}
	return self
}

func cmdEvidence(args []string) {
	fs := flag.NewFlagSet("evidence", flag.ExitOnError)
	in := fs.String("in", "", "输入 JSON：[{id,title,depth:1|2|3,source,evidence}]（record 模式）")
	export := fs.Bool("export", false, "导出证据页 HTML")
	owner := fs.String("owner", "学习者", "证据页署名")
	fs.Parse(args)
	lg := loadLedger()
	if *export {
		f, err := os.Create(dataDir + "/evidence.html")
		if err != nil {
			die(err)
		}
		defer f.Close()
		lg.WriteHTML(f, *owner, now())
		fmt.Println("证据页 →", dataDir+"/evidence.html")
		return
	}
	if *in == "" {
		die(fmt.Errorf("record 模式需要 -in"))
	}
	var events []struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Depth    int    `json:"depth"`
		Source   string `json:"source"`
		Evidence string `json:"evidence"`
	}
	loadJSON(*in, &events)
	nowT := now()
	for _, e := range events {
		lg.Record(e.ID, e.Title, evidence.Depth(e.Depth), nowT, e.Source, e.Evidence)
	}
	saveLedger(lg)
	k, u, d := lg.Counts()
	fmt.Printf("履历已更新：知道 %d · 用过 %d · 做过 %d\n", k, u, d)
}

func loadLedger() *evidence.Ledger {
	lg := evidence.New()
	if b, err := os.ReadFile(dataDir + "/ledger.json"); err == nil {
		_ = json.Unmarshal(b, lg)
	}
	return lg
}

func saveLedger(lg *evidence.Ledger) {
	saveJSON(dataDir+"/ledger.json", lg)
}

func cmdKBImport1x(args []string) {
	fs := flag.NewFlagSet("kb-import1x", flag.ExitOnError)
	in := fs.String("in", "", "1.x readingDocs JSON（数组）")
	node := fs.String("node", "", "归入的知识点 id")
	fs.Parse(args)
	if *in == "" || *node == "" {
		die(fmt.Errorf("需要 -in 与 -node"))
	}
	kb := loadKB()
	imported, rejected, errs := kb.ImportReadingDocsV1(*node, mustRead(*in))
	mustSaveKB(kb)
	fmt.Printf("迁入 %d、拒收 %d（红线）\n", imported, rejected)
	for _, e := range errs {
		fmt.Println("  -", e)
	}
}

func cmdKBExport() {
	kb := loadKB()
	f, err := os.Create(dataDir + "/kb-ccby.json")
	if err != nil {
		die(err)
	}
	defer f.Close()
	if err := kb.ExportJSON(f); err != nil {
		die(err)
	}
	attr := kb.Attribution()
	fmt.Printf("知识库导出 → %s（CC-BY-SA-4.0，白名单 %d 条 / 待核 %d 条）\n", dataDir+"/kb-ccby.json", len(attr.Whitelist), len(attr.Graylist))
}

func cmdSandbox(args []string) {
	fs := flag.NewFlagSet("sandbox", flag.ExitOnError)
	caseID := fs.String("case", "", "案例 id（bc-mcp-typo / bc-cache-bound / bc-queue-logic）")
	dir := fs.String("dir", "", "沙盒工作区目录")
	check := fs.Bool("check", false, "验收修复")
	fs.Parse(args)
	if *caseID == "" || *dir == "" {
		die(fmt.Errorf("需要 -case 与 -dir"))
	}
	lib := sandbox.Library()
	c, ok := lib[*caseID]
	if !ok {
		die(fmt.Errorf("未知案例: %s（可用: bc-mcp-typo, bc-cache-bound, bc-queue-logic）", *caseID))
	}
	s, err := sandbox.Create(*dir, c)
	if err != nil {
		// 目录已存在 → 恢复会话（直接复用目录验收）
		if !*check {
			fmt.Println("目录非空——按已有内容验收：")
		}
		s = &sandbox.Session{Case: c, Root: *dir}
	}
	res, err := s.Verify()
	if err != nil {
		die(err)
	}
	if res.Passed {
		fmt.Println("验收通过 ✓", res.Detail)
		fmt.Println("履历联动提示：知识点", c.KnowledgePoint, "可记「用过」（evidence -in 事件 JSON）")
		os.Exit(0)
	}
	fmt.Println("验收未通过 ✗", res.Detail)
	os.Exit(1)
}

func cmdGraduate(args []string) {
	fs := flag.NewFlagSet("graduate", flag.ExitOnError)
	in := fs.String("in", "", "输入 JSON：{domain,dueCount,openBreaks,doneCount,maintainDays,lastGraduated}")
	fs.Parse(args)
	if *in == "" {
		die(fmt.Errorf("需要 -in"))
	}
	var s graduation.DomainStats
	loadJSON(*in, &s)
	state, reason := graduation.Evaluate(s, graduation.DefaultConfig(), now())
	fmt.Printf("[%s] %s\n", state, reason)
}

func loadKB() *kb.KB {
	k, err := kb.LoadKB(dataDir + "/kb.json")
	if err != nil {
		k = kb.New()
	}
	return k
}

func mustSaveKB(k *kb.KB) {
	if err := k.Save(dataDir + "/kb.json"); err != nil {
		die(err)
	}
}

func mustRead(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		die(err)
	}
	return b
}

func now() time.Time { return time.Now() }

func timeSeed() int64 { return now().UnixNano() }

// 引用占位：ability 将在 P31+ 应用层（答题事件）接入；此处保 import 完整性。
var _ = ability.New
