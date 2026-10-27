package tutor

import "testing"

// 第六轮-P0-2：误因标签解析审计测试。
func TestParseCause(t *testing.T) {
	cases := []struct {
		name, reply, reason, evidence string
	}{
		{"标准标签", "先说结论……\n[CAUSE:condition|他写的 a==b 应该是 a!=b]", "condition", "他写的 a==b 应该是 a!=b"},
		{"无标签", "正常教学回复，没有判定。", "", ""},
		{"标签后有余文", "[CAUSE:concept|没懂] 后面还有一行 [PASS:partial]", "concept", "没懂"},
		{"无证据", "差在哪一步说清了就行。[CAUSE:env]", "env", ""},
		{"缺右括号", "[CAUSE:slip|拼写错了", "", ""},
		{"类别空格", "……[CAUSE: slip | 多打了分号 ]", "slip", "多打了分号"},
	}
	for _, c := range cases {
		r, e := ParseCause(c.reply)
		if r != c.reason || e != c.evidence {
			t.Errorf("%s: ParseCause→(%q,%q) want (%q,%q)", c.name, r, e, c.reason, c.evidence)
		}
		// 合法性联动
		if r != "" && !ValidCause(r) && c.name == "标准标签" {
			t.Errorf("%s: %q 应为合法类别", c.name, r)
		}
	}
	if ValidCause("cheating") {
		t.Error("白名单外类别不应通过")
	}
}
