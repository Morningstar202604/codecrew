package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"codecrew/internal/config"
	"codecrew/internal/repl"
	"codecrew/internal/server"
)

// version 由构建时 -ldflags "-X main.version=vX.Y.Z" 注入。
var version = "v0.2.0"

func main() {
	var (
		flagRole    = flag.String("role", "", "启动时使用的角色，例如 reviewer")
		flagModel   = flag.String("model", "", "启动时使用的模型，形如 供应商/模型名")
		flagCwd     = flag.String("cwd", "", "项目工作目录（工具读写与命令执行的根）")
		flagConfig  = flag.String("config", "", "指定配置文件路径（默认自动查找）")
		flagYes     = flag.Bool("yes", false, "跳过权限交互确认（等价 permissions 全 allow，谨慎使用）")
		flagSession = flag.String("session", "", "续聊指定会话 ID")
		flagPrint   = flag.String("print", "", "非交互模式：发送一条指令后退出")
		flagNoColor = flag.Bool("no-color", false, "禁用彩色输出")
		flagVer     = flag.Bool("version", false, "打印版本后退出")
		flagServe   = flag.Bool("serve", false, "启动 Web 服务（浏览器访问）")
		flagPort    = flag.String("port", "8080", "Web 服务端口（仅 --serve 时生效）")
		flagAddr    = flag.String("addr", "", "Web 服务监听地址，默认 0.0.0.0（仅 --serve 时生效）")
	)
	flag.Usage = usage
	flag.Parse()

	if *flagNoColor {
		_ = os.Setenv("NO_COLOR", "1")
	}
	if *flagVer {
		fmt.Printf("codecrew %s\n", version)
		return
	}

	base := exeDir()
	cfg, err := config.Load(base, *flagConfig)
	if err != nil {
		fatal(err)
	}
	if *flagCwd != "" {
		abs, err := filepath.Abs(*flagCwd)
		if err != nil {
			fatal(fmt.Errorf("工作目录无效: %w", err))
		}
		cfg.WorkingDir = abs
	}
	if *flagModel != "" {
		cfg.Model = *flagModel
	}
	if *flagYes {
		cfg.Permissions = mergePermissions(cfg.Permissions, map[string]string{"*": "allow"})
	}

	// Web 服务模式
	if *flagServe {
		addr := *flagAddr
		if addr == "" {
			addr = "0.0.0.0"
		}
		// 验证端口格式
		portNum, err := strconv.Atoi(*flagPort)
		if err != nil || portNum < 1 || portNum > 65535 {
			fatal(fmt.Errorf("无效端口 %q，必须是 1-65535 的整数", *flagPort))
		}
		srv := server.NewServer(cfg, base)
		if err := srv.ListenAndServe(addr + ":" + *flagPort); err != nil {
			fatal(err)
		}
		return
	}

	app, err := repl.New(cfg, repl.Options{
		BaseDir:   base,
		AutoYes:   *flagYes,
		SessionID: *flagSession,
		First:     *flagPrint,
		Print:     *flagPrint != "",
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
	})
	if err != nil {
		fatal(err)
	}
	if *flagRole != "" {
		if err := app.SetRole(*flagRole); err != nil {
			fatal(err)
		}
	}
	if err := app.Run(); err != nil {
		fatal(err)
	}
}

func mergePermissions(base, extra map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for k, v := range extra {
		base[k] = v
	}
	return base
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	// go run 会把二进制放到临时目录，此时退回当前工作目录
	if strings.Contains(filepath.Base(dir), "go-build") {
		if wd, err := os.Getwd(); err == nil {
			return wd
		}
	}
	return dir
}

func usage() {
	fmt.Fprint(os.Stderr, `CodeCrew — 终端里的 AI 开发团队 / Web 工作台

用法:
  codecrew [flags]                    终端交互模式
  codecrew --serve [--port 8080]     Web 服务模式（浏览器访问）
  codecrew [flags] --print "你的问题"  非交互单轮

标志:
  --role <name>      启动角色（developer / reviewer / architect / tester / docs）
  --model <spec>     模型，形如 deepseek/deepseek-chat
  --cwd <dir>        项目工作目录，工具读写与命令执行的根
  --config <path>    指定配置文件
  --session <id>     续聊历史会话
  --print <text>     非交互，单轮后退出
  --yes              跳过权限确认
  --no-color         关闭彩色输出
  --serve            启动 Web 服务，浏览器访问 http://localhost:8080
  --port <port>      Web 服务端口，默认 8080
  --addr <addr>      Web 服务监听地址，默认 0.0.0.0
  --version          打印版本
  -h, --help         显示本帮助

终端模式输入 /help 查看全部命令。
`)
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}
