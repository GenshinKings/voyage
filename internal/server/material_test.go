package server

import (
	"strings"
	"testing"
)

// 孵化池兜底：本地没有代码时，出题材料改用孵化池里没读过的项目。
func TestPoolMaterialFallsBack(t *testing.T) {
	s := &Server{Dir: t.TempDir()} // 沙箱目录不存在 → learnerMaterial 返回空

	s.Readings = []Reading{
		{ID: "1", Title: "micro-app", URL: "https://github.com/jd-opensource/micro-app", Brief: "微前端框架"},
		{ID: "2", Title: "已经读完的", Done: true, Brief: "不该出现"},
		{ID: "3", Title: "taro", Brief: "多端框架"},
		{ID: "4", Title: "oxygent", Brief: "多智能体"},
		{ID: "5", Title: "第四个不该进", Brief: "超出 limit"},
	}

	got := s.exerciseMaterial()
	if got == "" {
		t.Fatal("孵化池有项目时不该返回空串")
	}
	for _, want := range []string{"micro-app", "taro", "oxygent"} {
		if !strings.Contains(got, want) {
			t.Fatalf("材料缺少 %q：\n%s", want, got)
		}
	}
	for _, bad := range []string{"已经读完的", "第四个不该进"} {
		if strings.Contains(got, bad) {
			t.Fatalf("不该包含 %q：\n%s", bad, got)
		}
	}
}

// 两边都空 = 空串（保持旧行为：退回通用例子）。
func TestPoolMaterialEmpty(t *testing.T) {
	s := &Server{Dir: t.TempDir()}
	if got := s.exerciseMaterial(); got != "" {
		t.Fatalf("没有任何材料时应返回空串，实际 %q", got)
	}
}
