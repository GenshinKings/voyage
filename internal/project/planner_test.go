package project

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func fakeChat(out string, err error) ChatFunc {
	return func(ctx context.Context, system, user string) (string, error) { return out, err }
}

func TestPlanNextNeedsBrief(t *testing.T) {
	p, err := PlanNext(context.Background(), fakeChat(`{"why":"因为"}`, nil), State{})
	if err == nil {
		t.Fatal("没有 brief 应该报错")
	}
	_ = p
}

func TestPlanNextParsesAndDefaultsWhy(t *testing.T) {
	p, err := PlanNext(context.Background(), fakeChat("好的：\n{\"brief\":\"做一个能暂停的番茄钟\",\"why\":\"\"}\n", nil), State{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Brief == "" {
		t.Error("brief 该解析出来")
	}
	if p.Why == "" {
		t.Error("why 空时要给默认说明，不能留白")
	}
}

func TestPlanNextSendsLearnerState(t *testing.T) {
	var got string
	chat := func(ctx context.Context, system, user string) (string, error) {
		got = user
		return `{"brief":"做A","why":"因为B"}`, nil
	}
	_, err := PlanNext(context.Background(), chat, State{
		Goal:       "能独立写小工具",
		CurrentLsn: "番茄钟怎么记住还剩多久",
		Checklist:  []string{"点暂停后时间不减少"},
		Weak:       []string{"暂停后时间会跳"},
		Passed:     []string{"计数器"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"能独立写小工具", "番茄钟", "点暂停后时间不减少", "暂停后时间会跳", "计数器"} {
		if !strings.Contains(got, want) {
			t.Errorf("发给教练的状态里少了 %q", want)
		}
	}
}

func TestPlanNextPropagatesError(t *testing.T) {
	_, err := PlanNext(context.Background(), fakeChat("", errors.New("llm 挂了")), State{})
	if err == nil {
		t.Error("模型报错必须传上来，不许假装备好了安排")
	}
}

// 侧栏只该看到一个副本：未过关的那个。这条规则落在 Status/Blocking 上，
// 这里用一次完整流程验一遍。
func TestOnlyCurrentRaidVisible(t *testing.T) {
	root := t.TempDir()
	mk := func(title string) string {
		r := README{Title: title, Goal: "g", Steps: []string{"s"},
			Checks: []CheckItem{
				{Cmd: "ls", Want: "x", Note: "1"},
				{Cmd: "ls", Want: "x", Note: "2"},
				{Cmd: "ls", Want: "x", Note: "3"},
			}}
		d, err := Create(root, r)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	d1 := mk("old-one")
	if _, err := Submit(d1, true, nil, "3/3"); err != nil {
		t.Fatal(err)
	}
	d2 := mk("current-one")
	list := ListDirs(root)
	var unfinished []Status
	for _, s := range list {
		if s.State != StatusPassed {
			unfinished = append(unfinished, s)
		}
	}
	if len(unfinished) != 1 || unfinished[0].Dir != d2 {
		t.Errorf("界面上只该有一个「未完成」副本，实际 %+v", unfinished)
	}
	// 过关的历史仍然要能查到（进历史，不占位）
	if len(list) != 2 {
		t.Errorf("已过关的该留在历史里，实际 %d 条", len(list))
	}
}
