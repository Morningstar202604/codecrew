package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type BashTool struct{ base string }

func NewBashTool(base string) *BashTool { return &BashTool{base: base} }

func (t *BashTool) Name() string        { return "bash" }
func (t *BashTool) Description() string { return "执行 Shell 命令" }

func (t *BashTool) Schema() map[string]any {
	return objectSchema(map[string]any{
		"command": stringSchema("要执行的命令"),
		"timeout": map[string]any{"type": "integer", "description": "超时秒数，默认 30"},
		"cwd":     stringSchema("工作目录，相对路径或绝对路径"),
	}, []string{"command"})
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	command, err := getString(args, "command")
	if err != nil {
		return "", err
	}

	timeoutSec := 30
	if v, ok := args["timeout"]; ok {
		if f, ok := v.(float64); ok {
			timeoutSec = int(f)
		}
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}

	cwd := t.base
	if v, ok := args["cwd"]; ok {
		if s, ok := v.(string); ok && s != "" {
			absCwd, err := safePath(t.base, s)
			if err != nil {
				return "", err
			}
			cwd = absCwd
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cmd.exe", "/C", command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "stderr:\n" + stderr.String()
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("命令超时 (%ds)\n%s", timeoutSec, output), nil
	}
	if err != nil {
		return fmt.Sprintf("退出码: %d\n%s", cmd.ProcessState.ExitCode(), output), nil
	}
	return output, nil
}
