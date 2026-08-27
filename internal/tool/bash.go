package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// BashTool 执行 Shell 命令，跨平台选择解释器。
type BashTool struct{ base string }

func NewBashTool(base string) *BashTool { return &BashTool{base: base} }

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "执行 Shell 命令并返回 stdout/stderr。构建、跑测试、装依赖都用它"
}

func (t *BashTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"command": stringSchema("要执行的命令，使用当前操作系统的 Shell 语法"),
		"timeout": integerSchema("超时秒数，默认 60，最大 300"),
		"cwd":     stringSchema("工作目录，默认为项目工作目录"),
	}, []string{"command"})
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, err := getString(args, "command")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command 不能为空")
	}

	timeoutSec := getInt(args, "timeout", 60)
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	cwd := t.base
	if s, ok := args["cwd"].(string); ok && strings.TrimSpace(s) != "" {
		resolved, err := SafePath("", s) // cwd 允许指向工作目录之外，由权限闸门负责把关
		if err != nil {
			return "", err
		}
		cwd = resolved
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	name, shellArgs := shellFor(command)
	cmd := exec.CommandContext(runCtx, name, shellArgs...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// 上下文超时后即使孙进程仍持有管道，也要在 2 秒内收工，避免卡住对话
	cmd.WaitDelay = 2 * time.Second
	runErr := cmd.Run()

	output := strings.TrimSpace(stdout.String())
	if errText := strings.TrimSpace(stderr.String()); errText != "" {
		if output != "" {
			output += "\n"
		}
		output += "stderr:\n" + errText
	}

	if runCtx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("命令超时（%ds）\n%s", timeoutSec, output)
	}
	if runErr != nil {
		code := exitCode(runErr)
		if output == "" {
			return fmt.Sprintf("退出码 %d（无输出）: %v", code, runErr), nil
		}
		return fmt.Sprintf("退出码 %d\n%s", code, output), nil
	}
	if output == "" {
		return "命令执行成功（无输出）", nil
	}
	return output, nil
}

// shellFor 按操作系统挑选解释器，避免把 cmd.exe 写死。
func shellFor(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		if looksLikePowerShell(command) {
			return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", command}
		}
		return "cmd.exe", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

func looksLikePowerShell(command string) bool {
	lower := strings.ToLower(command)
	for _, hint := range []string{"get-", "set-", "new-item", "remove-item", "$env:", "| out-null", "invoke-"} {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func exitCode(err error) int {
	type exitStatus interface{ ExitCode() int }
	if es, ok := err.(exitStatus); ok {
		return es.ExitCode()
	}
	return -1
}
