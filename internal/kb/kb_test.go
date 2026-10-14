package kb

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 14, 3, 0, 0, 0, time.UTC)

func TestLicenseClassification(t *testing.T) {
	rules := DefaultRules()
	cases := []struct {
		license, url string
		want         LicenseClass
	}{
		{"MIT", "https://github.com/x/y", Whitelist},
		{"Apache-2.0", "https://github.com/x/y", Whitelist},
		{"CC-BY-SA-4.0", "https://en.wikipedia.org/wiki/Map", Whitelist},
		{"arXiv", "https://arxiv.org/abs/2410.03017", Whitelist},
		{"Public Domain", "https://gutenberg.org/ebooks/1", Whitelist},
		{"", "https://go.dev/ref/mem", Graylist},        // 官方文档灰名单
		{"", "https://mdn.example/x", Graylist},          // 未知授权保守
		{"All Rights Reserved", "https://books.example/x", Excluded},
		{"proprietary 教材", "https://x.example", Excluded},
	}
	for _, c := range cases {
		got, _ := Classify(c.license, c.url, rules)
		if got != c.want {
			t.Errorf("Classify(%q,%q)=%d want %d", c.license, c.url, got, c.want)
		}
	}
}

// 授权红线：Excluded 来源拒收（商业教科书只读不存）。
func TestExcludedSourceRejected(t *testing.T) {
	kb := New()
	_, err := kb.AddSource("kp-map", "映射", "https://books.example/map", "映射的定义……", "All Rights Reserved", now)
	if err == nil {
		t.Fatal("商业版权来源应被拒收")
	}
	if !strings.Contains(err.Error(), "只读不存") {
		t.Fatalf("错误信息应含红线说明: %v", err)
	}
	if n := kb.Nodes["kp-map"]; n != nil && len(n.Sources) != 0 {
		t.Fatal("拒收的片段不得入账")
	}
}

// 时效×可信权重：新官方文档 > 旧博客。
func TestWeightOrdering(t *testing.T) {
	kb := New()
	fresh, _ := kb.AddSource("kp-x", "x", "https://go.dev/ref/spec", "spec text", "", now.AddDate(-1, 0, 0))
	stale, _ := kb.AddSource("kp-x", "x", "https://blog.example/post", "blog text", "CC-BY", now.AddDate(-6, 0, 0))
	if fresh.Weight <= stale.Weight {
		t.Fatalf("新官方文档应重于旧博客: fresh=%f stale=%f", fresh.Weight, stale.Weight)
	}
	// 时效下限 0.4：三十年老书不清零
	ancient := TimeWeight(now.AddDate(-30, 0, 0), now)
	if ancient != 0.4 {
		t.Fatalf("时效下限应 0.4: %f", ancient)
	}
}

// 相似度闸门：原创综合过闸，贴近复制被拦。
func TestShingleGate(t *testing.T) {
	original := "映射是一种对应关系：集合 A 中的每个元素，在集合 B 中都有唯一确定的像。可以想象成自动售货机的按钮与商品——每个按钮只对应一件商品，但两个按钮可能卖同一件。"
	copied := "映射是一种对应关系：集合 A 中的每个元素，在集合 B 中都有唯一确定的像。可以想象成自动售货机的按钮与商品——每个按钮只对应一件商品，但两个按钮可能卖同一件商品。"
	anchor := Source{URL: "https://book.example", Excerpt: original}
	g := GateMentorView(copied, []Source{anchor})
	if g.Passed {
		t.Fatalf("贴近复制应被拦: jaccard=%.2f", g.MaxJaccard)
	}
	// 原创综合（同概念、自己的语言与结构）
	rewrite := "把映射想成一台售货机：按一个键，出来的东西是确定的。这里有两个要点——键必须有输出（处处有定义），且一个键只出一样（单值）。两个键出同一样东西是允许的，这和一一对应不同。"
	g2 := GateMentorView(rewrite, []Source{anchor})
	if !g2.Passed {
		t.Fatalf("原创综合不应被拦: jaccard=%.2f", g2.MaxJaccard)
	}
	if !(g2.MaxJaccard < g.MaxJaccard) {
		t.Fatal("原创综合相似度应显著低于贴近复制")
	}
}

// 端到端：三层入账 + 状态派生 + 品味选优 + CC-BY-SA 导出。
func TestKBEndToEnd(t *testing.T) {
	kb := New()
	kb.SetTitle("kp-map", "映射（Map）")
	// ①层：两个白名单来源
	s1, err := kb.AddSource("kp-map", "Go 语言映射规范", "https://go.dev/ref/spec#Map", "映射类型由键元素类型与元素类型组成；键唯一。", "official", now.AddDate(-2, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := kb.AddSource("kp-map", "维基百科：映射", "https://en.wikipedia.org/wiki/Map_(mathematics)", "映射为两集合间的对应规则，定义域中每元素对应陪域中唯一元素。", "CC-BY-SA", now.AddDate(-1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	// ②层：助教综合（原创）
	if _, err := kb.AddMentorView("kp-map", "v1", "售货机类比：每个按钮有确定输出，且单值；两键同货允许——这与一一对应不同（那是多对一禁令）。", []string{s1.ID, s2.ID}); err != nil {
		t.Fatalf("原创综合应过闸: %v", err)
	}
	// ③层：批判批注（某教材的说法被更新）
	kb.AddCritique("kp-map", Critique{
		Kind: CritiqueOutdated, Claim: "旧教材称映射必须为数集之间",
		Correction: "现代定义推广到任意类型/集合（如类型论中的映射）", SourceID: s2.ID, Confidence: 0.9,
	})
	n := kb.Nodes["kp-map"]
	if n.Status() != StatusConsensus {
		t.Fatalf("outdated 批注应为共识态（非争议）: %s", n.Status())
	}
	if len(n.Sources) != 2 || len(n.Views) != 1 || len(n.Critiques) != 1 {
		t.Fatalf("三层入账数错误: %+v", n)
	}
	// 品味：v1 讲了 3 次过 2 次
	n.Views[0].Taught, n.Views[0].PassFirst = 3, 2
	if n.BestView().ID != "v1" {
		t.Fatal("唯一讲法应为最佳讲法")
	}
	// 前后置
	kb.Link("kp-set", "kp-map")
	kb.Link("kp-map", "kp-func")
	if len(kb.Nodes["kp-func"].Prereqs) != 1 {
		t.Fatal("前置关系未建立")
	}
	// 导出：署名清单——wikipedia（CC-BY-SA）入白名单；go.dev 官方文档按宪法为
	// 灰名单（学习锚定可以，再分发前逐条核授权）
	attr := kb.Attribution()
	if len(attr.Whitelist) != 1 || len(attr.Graylist) != 1 {
		t.Fatalf("署名清单错误: white=%d gray=%d", len(attr.Whitelist), len(attr.Graylist))
	}
	if attr.Whitelist[0].URL != "https://en.wikipedia.org/wiki/Map_(mathematics)" {
		t.Fatalf("白名单应含 wikipedia: %+v", attr.Whitelist)
	}
	if attr.License != "CC-BY-SA-4.0" {
		t.Fatal("导出授权应为 CC-BY-SA-4.0")
	}
}

// 持久化往返。
func TestSaveLoadRoundTrip(t *testing.T) {
	path := t.TempDir() + "/kb.json"
	kb := New()
	kb.SetTitle("kp-a", "节点A")
	if _, err := kb.AddSource("kp-a", "t", "https://github.com/x", "内容", "MIT", now); err != nil {
		t.Fatal(err)
	}
	if err := kb.Save(path); err != nil {
		t.Fatal(err)
	}
	kb2, err := LoadKB(path)
	if err != nil {
		t.Fatal(err)
	}
	if kb2.Nodes["kp-a"].Sources[0].URL != "https://github.com/x" {
		t.Fatal("往返丢失数据")
	}
	if kb2.Rules == nil || kb2.Now == nil {
		t.Fatal("加载后规则与时钟应重建")
	}
}
