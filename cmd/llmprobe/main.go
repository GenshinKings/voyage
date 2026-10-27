// llmprobe — 诊断 LLM 原始返回（质量门调试用）。
package main

import (
	"fmt"

	"voyage/internal/llm"
)

func main() {
	cfg, err := llm.LoadConfig("outpost2-data/llm.json")
	if err != nil {
		fmt.Println("config:", err)
		return
	}
	c := llm.NewClient(cfg)
	raw, err := c.ChatJSON(
		"你是课程设计师。把科目分解成 4-6 个有序知识点（title ≤16字、brief 一句话）。输出严格 JSON：{\"nodes\":[{\"title\":\"...\",\"brief\":\"...\"}]}",
		[]llm.Message{{Role: "user", Content: "科目：MCP 协议入门"}})
	if err != nil {
		fmt.Println("chat err:", err)
		return
	}
	fmt.Println("RAW >>>")
	fmt.Println(raw)
}
