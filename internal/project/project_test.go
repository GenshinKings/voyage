package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodJSON = `{"title":"番茄钟","goal":"一个能倒计时的番茄钟",
"steps":["建 main.go","写倒计时循环"],
"checks":[
 {"cmd":"go run main.go","want":"25:00","note":"能跑起来并显示初始时间"},
 {"cmd":"go run main.go","want":"24:59","note":"一秒后时间在减少"},
 {"cmd":"go test ./...","want":"ok","note":"测试通过"}
],"terms":[]}`

func TestValidateRequiresExecutableChecks(t *testing.T) {
	r, err := Parse(goodJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(r); err != nil {
		t.Fatalf("好题不该被拒：%v", err)
	}
	// 缺命令的验收必须被拒 —— 否则「检查完成情况」变成助教说了算
	bad := r
	bad.Checks = []CheckItem{{Cmd: "", Want: "x", Note: "空命令"}}
	if err := Validate(bad); err == nil {
		t.Error("缺命令的验收应被拒")
	}
	// 少于 3 条也拒
	bad2 := r
	bad2.Checks = r.Checks[:2]
	if err := Validate(bad2); err == nil {
		t.Error("少于 3 条验收应被拒")
	}
}

func TestParseToleratesFences(t *testing.T) {
	r, err := Parse("好的：\n```json\n" + goodJSON + "\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "番茄钟" {
		t.Errorf("标题解析错：%q", r.Title)
	}
}

func TestCreateWritesReadmeAndSkeleton(t *testing.T) {
	root := t.TempDir()
	r, _ := Parse(goodJSON)
	dir, err := Create(root, r)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(b)
	for _, want := range []string{"番茄钟", "怎么算做完", "go run main.go", "该看到"} {
		if !strings.Contains(md, want) {
			t.Errorf("README 里少了 %q", want)
		}
	}
	// 骨架只在不存在时写：不许覆盖他写了一半的代码
	main := filepath.Join(dir, "main.go")
	if _, err := os.Stat(main); err != nil {
		t.Fatal("应该有骨架 main.go")
	}
	_ = os.WriteFile(main, []byte("package main\n// 他写的东西\n"), 0o644)
	if _, err := Create(root, r); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(main)
	if !strings.Contains(string(b2), "他写的东西") {
		t.Error("重建时不许覆盖学习者已经写的代码")
	}
}

func TestDescriptiveWantsRejected(t *testing.T) {
	r, _ := Parse(goodJSON)
	bad := r
	bad.Checks = append([]CheckItem{}, r.Checks...)
	bad.Checks[0].Want = "出现25:00这样的起始时间"
	if err := Validate(bad); err == nil {
		t.Error("描述句当期望必须被拒，否则验收永远 0 条过")
	}
	bad2 := r
	bad2.Checks = append([]CheckItem{}, r.Checks...)
	bad2.Checks[0].Want = "倒计时 25:00"
	if err := Validate(bad2); err == nil {
		t.Error("期望里带空格（描述性前缀+真值）必须被拒")
	}
}

func TestRunOnlyWhitelisted(t *testing.T) {
	dir := t.TempDir()
	r := README{Checks: []CheckItem{
		{Cmd: "rm -rf /", Want: "x", Note: "危险命令"},
		{Cmd: "ls", Want: "README", Note: "能列目录"},
	}}
	res := Run(dir, r)
	if res[0].Error == "" {
		t.Error("非白名单命令必须被拦")
	}
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644)
	res2 := Run(dir, README{Checks: []CheckItem{{Cmd: "ls", Want: "README", Note: "列目录"}}})
	if !res2[0].Pass {
		t.Errorf("ls 应该能看到 README：got=%q err=%q", res2[0].Got, res2[0].Error)
	}
}

func TestGenerateRejectsBadModel(t *testing.T) {
	chat := func(ctx context.Context, system, user string) (string, error) {
		return "我不会出题", nil
	}
	if _, err := Generate(context.Background(), chat, "做个番茄钟"); err == nil {
		t.Error("模型输出不可解析时应该报错，不许静默生成空项目")
	}
}
