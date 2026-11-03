package tutor

import (
	"strings"
	"testing"
)

// 2026-11-03：学生第二课就被「状态机」三个字砸懵了。
// 这两条必须写死在提示词里，不然模型换一轮就忘。
func TestTeachSystemTermsComeLast(t *testing.T) {
	for _, want := range []string{
		"术语后置",
		"知识点用它要干的事来命名",
		"术语最后才出现",
	} {
		if !strings.Contains(teachSystem, want) {
			t.Errorf("teachSystem 里少了「%s」", want)
		}
	}
}

func TestTeachSystemRequiresChecklist(t *testing.T) {
	for _, want := range []string{
		"必须掌握清单",
		"3~6 条",
		"可考",
		"这节课真讲过的",
	} {
		if !strings.Contains(teachSystem, want) {
			t.Errorf("teachSystem 里少了「%s」", want)
		}
	}
}

// 术语后置不许退化成「不许用术语」——他还是要知道名字的，只是在最后。
func TestTeachSystemStillTeachesTheTerm(t *testing.T) {
	if !strings.Contains(teachSystem, "先给一句大白话解释") {
		t.Error("要求术语出现时必须先解释，这条不能被省掉")
	}
}
