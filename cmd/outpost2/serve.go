package main

// serve.go — 启动本地 Web 服务（全托管学习机的日用形态）。

import (
	"flag"
	"fmt"
	"os/exec"
	"time"

	"outpost2/internal/server"
)

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7412", "监听地址")
	noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
	fs.Parse(args)
	_, err := server.Serve(*addr)
	if err != nil {
		die(err)
	}
	url := "http://" + *addr
	fmt.Println("前哨 2.0 已启动 →", url)
	fmt.Println("数据目录：./outpost2-data（宪法与内核见仓库 README）")
	if !*noOpen {
		go func() {
			time.Sleep(400 * time.Millisecond)
			_ = exec.Command("cmd", "/c", "start", url).Start()
		}()
	}
	select {} // 常驻：handler 在 internal/server 的 goroutine 里跑
}
