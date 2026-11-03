package project

import (
	"os"
	"path/filepath"
	"testing"
)

func setupProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	r := README{Title: "番茄钟", Goal: "g", Steps: []string{"s"},
		Checks: []CheckItem{
			{Cmd: "ls", Want: "README", Note: "能列目录"},
			{Cmd: "ls", Want: "README", Note: "第二条"},
			{Cmd: "ls", Want: "README", Note: "第三条"},
		}}
	dir, err := Create(root, r)
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStatusDefaultsToReady(t *testing.T) {
	dir := setupProject(t)
	if s := LoadStatus(dir); s.State != StatusReady {
		t.Errorf("新项目应是 ready，实际 %q", s.State)
	}
}

func TestSubmitFailGoesBackToWorking(t *testing.T) {
	dir := setupProject(t)
	s, err := Submit(dir, false, []string{"第 1 条"}, "1/3")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != StatusWorking {
		t.Errorf("没过要退回 working，实际 %q", s.State)
	}
	if s.Submits != 1 || s.LastCheck != "1/3" {
		t.Errorf("提交次数/摘要没记上：%+v", s)
	}
	if got := LoadStatus(dir); got.State != StatusWorking {
		t.Error("状态没落盘")
	}
}

func TestSubmitPassLocks(t *testing.T) {
	dir := setupProject(t)
	if _, err := Submit(dir, true, nil, "3/3"); err != nil {
		t.Fatal(err)
	}
	if _, err := Submit(dir, true, nil, "3/3"); err == nil {
		t.Error("过关后不许再提交")
	}
}

func TestBlockingStopsNextRaid(t *testing.T) {
	root := t.TempDir()
	// 空目录：不挡
	if _, blocked := Blocking(root); blocked {
		t.Error("没有项目时不该挡")
	}
	r := README{Title: "A", Goal: "g", Steps: []string{"s"},
		Checks: []CheckItem{
			{Cmd: "ls", Want: "x", Note: "1"}, {Cmd: "ls", Want: "x", Note: "2"}, {Cmd: "ls", Want: "x", Note: "3"},
		}}
	dir, err := Create(root, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, blocked := Blocking(root); !blocked {
		t.Error("有没过关的副本时必须挡住下一个")
	}
	if _, err := Submit(dir, true, nil, "3/3"); err != nil {
		t.Fatal(err)
	}
	if _, blocked := Blocking(root); blocked {
		t.Error("过关后不该再挡")
	}
}

func TestLoadIgnoresNonProjectDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "随便的目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ListDirs(root); len(got) != 0 {
		t.Errorf("没有 .practice.json 的目录不该列出：%+v", got)
	}
}
