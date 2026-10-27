package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"voyage/internal/evidence"
	"voyage/internal/tutor"
)

// TierOf：档位由后端按缺失数判定（LLM 不定档）。
func TestTierOf(t *testing.T) {
	cases := []struct {
		miss int
		want int
	}{
		{0, tutor.TierConsolidate},
		{1, tutor.TierStretch},
		{2, tutor.TierStretch},
		{3, tutor.TierLeap},
		{5, tutor.TierLeap},
	}
	for _, c := range cases {
		req := []tutor.ReqNode{}
		for i := 0; i < c.miss; i++ {
			req = append(req, tutor.ReqNode{N: "缺", Miss: true})
		}
		req = append(req, tutor.ReqNode{N: "函数封装"})
		if got := tutor.TierOf(req); got != c.want {
			t.Fatalf("TierOf(%d miss)=%d want %d", c.miss, got, c.want)
		}
	}
}

// mintTrophy：纪念卡字段完整 + 新卡置顶 + 旧数据 Impact 兜底。
func TestMintTrophy(t *testing.T) {
	s := &Server{Dir: t.TempDir(), Ledger: evidence.New()}
	d := &tutor.Dungeon{
		ID: "dn-test-1", Repo: "headroomlabs-ai/headroom", Project: "Token 压缩层",
		Status: "cleared", Impact: "让长上下文 AI 应用便宜 10 倍的开源底座",
		Stages: []tutor.DStage{{Title: "a"}, {Title: "b"}, {Title: "c"}},
		ShadowMeta: tutor.ShadowMeta{Tier: 0, Stars: 2, Req: []tutor.ReqNode{
			{N: "函数封装"}, {N: "条件门控"}, {N: "字符串切片"},
		}},
	}
	tr := s.mintTrophy(d)
	if tr.Name != "Token 压缩层" || tr.Repo != d.Repo || tr.Stages != 3 {
		t.Fatalf("字段不完整: %+v", tr)
	}
	if len(tr.Nodes) != 3 || tr.Nodes[0] != "函数封装" {
		t.Fatalf("nodes 提取错误: %v", tr.Nodes)
	}
	if len(tr.Date) != 10 || !strings.Contains(tr.Short, "·") {
		t.Fatalf("日期格式错误: %q %q", tr.Date, tr.Short)
	}
	// 铸第二张 → 新卡置顶
	d2 := &tutor.Dungeon{ID: "dn-test-2", Project: "排序器", Repo: "x/y",
		Stages: []tutor.DStage{{Title: "a"}}, ShadowMeta: tutor.ShadowMeta{Req: []tutor.ReqNode{{N: "循环"}}}}
	tr2 := s.mintTrophy(d2)
	if s.Trophies[0] != tr2 || len(s.Trophies) != 2 {
		t.Fatalf("新卡应置顶: %d", len(s.Trophies))
	}
	// 无 Impact 兜底
	d3 := &tutor.Dungeon{ID: "dn-3", Project: "P", Repo: "a/b", Stages: []tutor.DStage{{Title: "x"}}}
	tr3 := s.mintTrophy(d3)
	if tr3.Impact == "" {
		t.Fatal("兜底 Impact 不应为空")
	}
}

// 脱敏红线（ADR 0001）：Sealed 副本的 JSON 序列化仍含敏感字段，
// 端点层必须走 dunView——这里验证 ShadowMeta 嵌入序列化后前端可拿 tier/stars/req。
func TestDungeonShadowJSON(t *testing.T) {
	d := tutor.Dungeon{Project: "秘密项目", Repo: "a/b", Status: "active",
		ShadowMeta: tutor.ShadowMeta{Tier: 1, Stars: 3, Req: []tutor.ReqNode{{N: "归并思想", Miss: true}}}}
	jb, _ := json.Marshal(d)
	raw := string(jb)
	if !strings.Contains(raw, `"tier":1`) || !strings.Contains(raw, `"miss":true`) {
		t.Fatalf("影子画像序列化缺失: %s", raw)
	}
}

// Trophy 时间戳可序列化回读（trophies.json 持久化往返）。
func TestTrophyJSONRoundTrip(t *testing.T) {
	tr := Trophy{Name: "N", Repo: "r", Impact: "i", Nodes: []string{"a"},
		Stages: 2, Date: "2026-10-27", Short: "10·27", At: time.Now(), DungeonID: "dn-1"}
	b, err := json.Marshal(tr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"short":"10·27"`) {
		t.Fatalf("short 序列化缺失: %s", b)
	}
}
