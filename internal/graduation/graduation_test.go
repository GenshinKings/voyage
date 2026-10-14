package graduation

import (
	"testing"
	"time"
)

func TestGraduationStateMachine(t *testing.T) {
	cfg := DefaultConfig()
	cases := []struct {
		name string
		s    DomainStats
		want State
	}{
		{"没做过东西谈不上毕业", DomainStats{DoneCount: 1, MaintainDays: 30}, Learning},
		{"还有到期卡", DomainStats{DoneCount: 5, DueCount: 3, MaintainDays: 30}, Learning},
		{"还有未修的坑", DomainStats{DoneCount: 5, OpenBreaks: 1, MaintainDays: 30}, Learning},
		{"保养期未满", DomainStats{DoneCount: 5, MaintainDays: 6}, Maintaining},
		{"全部清零毕业", DomainStats{DoneCount: 5, MaintainDays: 14}, Graduated},
	}
	for _, c := range cases {
		got, _ := Evaluate(c.s, cfg, time.Now())
		if got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

// 毕业单向门：毕业后哪怕数据被清，也不打回 Learning。
func TestGraduatedIsOneWay(t *testing.T) {
	s := DomainStats{DoneCount: 5, MaintainDays: 30, LastGraduated: "2027-01-11"}
	got, reason := Evaluate(s, DefaultConfig(), time.Now())
	if got != Graduated {
		t.Fatalf("毕业后应锁定: %s", got)
	}
	if reason == "" {
		t.Fatal("毕业应附带人话说明")
	}
}

// 超过 MinDone 的加倍达标也应毕业（上限不是上限）。
func TestOverAchieverGraduates(t *testing.T) {
	s := DomainStats{DoneCount: 12, MaintainDays: 60}
	got, _ := Evaluate(s, DefaultConfig(), time.Now())
	if got != Graduated {
		t.Fatalf("超量达标应毕业: %s", got)
	}
}
