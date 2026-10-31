package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voyage/internal/run"
)

// 交付履历：完工才进履历；导出里必须带目标、验收、产物、当时的不确定、对账与复盘。
func TestDeliverableExport(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)
	s.registerDeliverableHandlers(mux)

	// 一关，Expect 命中即过
	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "选品小工具",
		Tier:      run.TierTool,
		Stages: []run.Stage{
			{ID: "s1", Goal: "打印 5 行结果", Check: "终端能看到 5 行", Expect: []string{"5 行"}},
		},
		Results: map[string]string{},
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/submit",
		strings.NewReader(`{"output":"5 行结果","uncertain":"不确定排序对不对"}`)))
	if rec.Code != 200 {
		t.Fatalf("提交失败：%d %s", rec.Code, rec.Body.String())
	}
	if !s.RunState.Finished {
		t.Fatal("应当完工")
	}

	// 对账 + 复盘
	for _, body := range []string{
		`{"stageId":"s1","outcome":"was-problem"}`,
		`{"stageId":"s1","kind":"condition","note":"以后先看边界再写排序"}`,
	} {
		path := "/api/run/reconcile"
		if strings.Contains(body, "kind") {
			path = "/api/run/retro"
		}
		r2 := httptest.NewRecorder()
		mux.ServeHTTP(r2, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if r2.Code != 200 {
			t.Fatalf("%s 失败：%d", path, r2.Code)
		}
	}

	// 列表
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/deliverables", nil))
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list["count"] != float64(1) {
		t.Fatalf("履历应有 1 条：%v", list)
	}

	// 导出
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/deliverables/md", nil))
	md := rec.Body.String()
	for _, want := range []string{"选品小工具", "打印 5 行结果", "终端能看到 5 行", "5 行结果", "不确定排序对不对", "判断命中", "边界"} {
		if !strings.Contains(md, want) {
			t.Fatalf("导出缺 %q：\n%s", want, md)
		}
	}
}

// 没完工的不该进履历。
func TestDeliverableOnlyAfterFinish(t *testing.T) {
	s := NewServer(t.TempDir())
	mux := http.NewServeMux()
	s.registerRunHandlers(mux)
	s.registerDeliverableHandlers(mux)

	s.RunState = &run.Run{
		ProjectID: "t",
		Title:     "没做完的",
		Tier:      run.TierTool,
		Stages: []run.Stage{
			{ID: "s1", Goal: "第一步", Check: "有输出", Expect: []string{"对"}},
			{ID: "s2", Goal: "第二步", Check: "有输出", Expect: []string{"再对"}},
		},
		Results: map[string]string{},
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/run/submit", strings.NewReader(`{"output":"对"}`)))

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/deliverables", nil))
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list["count"] != float64(0) {
		t.Fatalf("没过完所有关不该进履历：%v", list)
	}
}
