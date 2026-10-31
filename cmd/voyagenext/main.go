// voyagenext —— 下一版界面（三屏线框）的独立可执行文件。
//
// 为什么不复用原来的 exe：旧界面还在用，两边要能同时开、对比手感。
// 数据目录是同一个，所以进度、收件箱、记录都是同一份。
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"voyage/internal/server"
)

func main() {
	fs := flag.NewFlagSet("voyagenext", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:7413", "监听地址")
	noOpen := fs.Bool("no-open", false, "不自动打开浏览器")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "参数有误：", err)
		os.Exit(2)
	}

	if _, err := server.ServeUI(*addr, "next"); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败：", err)
		os.Exit(1)
	}
	url := "http://" + *addr
	fmt.Println("Voyage Next（三屏线框）已启动 →", url)
	fmt.Println("数据与 7412 共用同一份 outpost2-data。")
	if !*noOpen {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := exec.Command("cmd", "/c", "start", url).Start(); err != nil {
				fmt.Fprintln(os.Stderr, "打不开浏览器，请手动访问：", url)
			}
		}()
	}
	select {}
}
