// Package verify 实现代码验证与自愈循环。
//
// 验证引擎执行可配置的验证命令（编译、测试、lint 等），
// 失败时自动分析错误并触发模型修复，多轮循环直到通过或达到上限。
package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Config 验证与自愈配置。
type Config struct {
	// Enabled 是否启用验证功能，默认 true。
	Enabled bool `json:"enabled,omitempty"`
	// AutoVerify 是否在代码修改后自动验证，默认 true。
	AutoVerify bool `json:"auto_verify,omitempty"`
	// Commands 验证命令列表，按顺序执行。
	Commands []string `json:"commands,omitempty"`
	// MaxRepairRounds 最大修复轮数，默认 3。
	MaxRepairRounds int `json:"max_repair_rounds,omitempty"`
	// TimeoutSeconds 单个命令超时时间，默认 120 秒。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// WorkingDir 工作目录，默认为当前目录。
	WorkingDir string `json:"working_dir,omitempty"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		Enabled:         true,
		AutoVerify:      true,
		Commands:        []string{"go build ./...", "go test ./..."},
		MaxRepairRounds: 3,
		TimeoutSeconds:  120,
	}
}

// GetMaxRepairRounds 返回最大修复轮数。
func (c Config) GetMaxRepairRounds() int {
	if c.MaxRepairRounds <= 0 {
		return 3
	}
	return c.MaxRepairRounds
}

// GetTimeout 返回单个命令超时时间。
func (c Config) GetTimeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// CommandResult 单个验证命令的执行结果。
type CommandResult struct {
	Command  string        `json:"command"`
	Passed   bool          `json:"passed"`
	Output   string        `json:"output"`
	Duration time.Duration `json:"duration_ms"`
}

// Result 一次完整验证的结果。
type Result struct {
	Passed   bool            `json:"passed"`
	Commands []CommandResult `json:"commands"`
	Total    int             `json:"total"`
	PassedN  int             `json:"passed_count"`
	FailedN  int             `json:"failed_count"`
	Duration time.Duration   `json:"duration_ms"`
}

// FailedOutput 返回所有失败命令的合并输出，用于错误分析。
func (r Result) FailedOutput() string {
	var sb strings.Builder
	for _, c := range r.Commands {
		if !c.Passed {
			fmt.Fprintf(&sb, "=== %s ===\n%s\n", c.Command, c.Output)
		}
	}
	return sb.String()
}

// Summary 返回验证结果摘要。
func (r Result) Summary() string {
	if r.Passed {
		return fmt.Sprintf("✓ 全部 %d 项验证通过（耗时 %s）", r.Total, r.Duration.Round(time.Millisecond))
	}
	return fmt.Sprintf("✗ %d/%d 项验证失败（耗时 %s）", r.FailedN, r.Total, r.Duration.Round(time.Millisecond))
}

// Engine 验证引擎。
type Engine struct {
	cfg Config
}

// NewEngine 创建验证引擎。
func NewEngine(cfg Config) *Engine {
	return &Engine{cfg: cfg}
}

// Run 执行所有验证命令。
func (e *Engine) Run(ctx context.Context) Result {
	if ctx == nil {
		ctx = context.Background()
	}
	result := Result{Total: len(e.cfg.Commands)}
	start := time.Now()

	for _, cmdStr := range e.cfg.Commands {
		cmdResult := e.runCommand(ctx, cmdStr)
		result.Commands = append(result.Commands, cmdResult)
		if cmdResult.Passed {
			result.PassedN++
		} else {
			result.FailedN++
		}
	}

	result.Passed = result.FailedN == 0
	result.Duration = time.Since(start)
	return result
}

// runCommand 执行单个验证命令。
func (e *Engine) runCommand(ctx context.Context, cmdStr string) CommandResult {
	result := CommandResult{Command: cmdStr}
	start := time.Now()

	// 解析命令（支持管道和重定向的简单场景）
	cmdCtx, cancel := context.WithTimeout(ctx, e.cfg.GetTimeout())
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", cmdStr)
	if e.cfg.WorkingDir != "" {
		cmd.Dir = e.cfg.WorkingDir
	}
	// 继承环境变量
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result.Duration = time.Since(start)

	// 合并输出
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}
	result.Output = strings.TrimSpace(output)

	if err != nil {
		result.Passed = false
		if result.Output == "" {
			result.Output = err.Error()
		}
	} else {
		result.Passed = true
	}

	return result
}

// DetectCommands 自动检测项目类型并推荐验证命令。
func DetectCommands(workingDir string) []string {
	commands := []string{}

	// Go 项目
	if fileExists(filepath.Join(workingDir, "go.mod")) {
		commands = append(commands, "go build ./...", "go vet ./...", "go test ./...")
		return commands
	}

	// Node.js 项目
	if fileExists(filepath.Join(workingDir, "package.json")) {
		pkg, err := os.ReadFile(filepath.Join(workingDir, "package.json"))
		if err == nil {
			var info struct {
				Scripts map[string]string `json:"scripts"`
			}
			if json.Unmarshal(pkg, &info) == nil {
				if _, ok := info.Scripts["build"]; ok {
					commands = append(commands, "npm run build")
				}
				if _, ok := info.Scripts["test"]; ok {
					commands = append(commands, "npm test")
				}
				if _, ok := info.Scripts["lint"]; ok {
					commands = append(commands, "npm run lint")
				}
			}
		}
		if len(commands) == 0 {
			commands = append(commands, "npm test")
		}
		return commands
	}

	// Python 项目
	if fileExists(filepath.Join(workingDir, "pyproject.toml")) || fileExists(filepath.Join(workingDir, "setup.py")) {
		commands = append(commands, "python -m pytest")
		return commands
	}

	// Rust 项目
	if fileExists(filepath.Join(workingDir, "Cargo.toml")) {
		commands = append(commands, "cargo build", "cargo test")
		return commands
	}

	// 默认：尝试 make test
	if fileExists(filepath.Join(workingDir, "Makefile")) {
		commands = append(commands, "make test")
		return commands
	}

	return commands
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
