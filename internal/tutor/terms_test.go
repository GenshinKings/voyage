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

// 2026-11-04：学生实测「学完还是晕，只会和助教写出一样的代码」。
// 这条必须写死在提示词里：不通过就不许给 PASS。
func TestTeachSystemRequiresBlankRewrite(t *testing.T) {
	for _, want := range []string{
		"空白重写关",
		"合上讲义",
		"不通过就不许输出 [PASS]",
		"能跑但靠抄的，不算过",
	} {
		if !strings.Contains(teachSystem, want) {
			t.Errorf("teachSystem 里少了「%s」", want)
		}
	}
}

// 学生 2026-11-04：助教不要过多赘述已完成的内容（进度-学会的）。
func TestTeachSystemDoesNotRecap(t *testing.T) {
	for _, want := range []string{
		"不复述已完成",
		"不是讲稿",
		"开场最多一句指路",
		"已经发生过的内容」删掉",
	} {
		if !strings.Contains(teachSystem, want) {
			t.Errorf("teachSystem 里少了「%s」", want)
		}
	}
}
