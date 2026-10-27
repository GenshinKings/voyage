package tutor

import "testing"

// 第六轮-P0-1 判据审计测试：3/2 阈值与封顶保底。
func TestNextGuide(t *testing.T) {
	cases := []struct {
		name                                string
		level, passStreak, failStreak, want int
	}{
		{"首触默认样例", 0, 0, 0, GuideSample},
		{"连过两次样例不升", GuideSample, 2, 0, GuideSample},
		{"连过三次升半提示", GuideSample, 3, 0, GuideHint},
		{"再过三次升提问", GuideHint, 3, 0, GuideAsk},
		{"提问封顶", GuideAsk, 6, 0, GuideAsk},
		{"提问连败两次退半提示", GuideAsk, 0, 2, GuideHint},
		{"半提示再败两次回样例", GuideHint, 0, 2, GuideSample},
		{"样例保底不再降", GuideSample, 0, 5, GuideSample},
		{"乱入负数钳制", -2, 0, 0, GuideSample},
	}
	for _, c := range cases {
		if got := NextGuide(c.level, c.passStreak, c.failStreak); got != c.want {
			t.Errorf("%s: NextGuide(%d,%d,%d)=%d want %d", c.name, c.level, c.passStreak, c.failStreak, got, c.want)
		}
	}
}

// GuideName 前端小字用的名字不可漂移。
func TestGuideName(t *testing.T) {
	for l, want := range map[int]string{GuideSample: "样例", GuideHint: "半提示", GuideAsk: "提问", 0: "样例", 9: "样例"} {
		if got := GuideName(l); got != want {
			t.Errorf("GuideName(%d)=%q want %q", l, got, want)
		}
	}
}
