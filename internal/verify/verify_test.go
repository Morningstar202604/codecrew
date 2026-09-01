package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.Enabled {
		t.Error("默认 Enabled 应为 true")
	}
	if !c.AutoVerify {
		t.Error("默认 AutoVerify 应为 true")
	}
	if c.GetMaxRepairRounds() != 3 {
		t.Errorf("默认 MaxRepairRounds = %d, 应为 3", c.GetMaxRepairRounds())
	}
	if c.GetTimeout() != 120*1e9 {
		t.Errorf("默认 Timeout = %v, 应为 120s", c.GetTimeout())
	}
}

func TestConfigGetters(t *testing.T) {
	c := Config{MaxRepairRounds: 5, TimeoutSeconds: 60}
	if c.GetMaxRepairRounds() != 5 {
		t.Errorf("MaxRepairRounds = %d, 应为 5", c.GetMaxRepairRounds())
	}
	if c.GetTimeout() != 60*1e9 {
		t.Errorf("Timeout = %v, 应为 60s", c.GetTimeout())
	}

	// 零值应返回默认
	c2 := Config{}
	if c2.GetMaxRepairRounds() != 3 {
		t.Error("零值 MaxRepairRounds 应为 3")
	}
}

func TestEngineRunSuccess(t *testing.T) {
	engine := NewEngine(Config{
		Commands: []string{"echo test"},
	})
	result := engine.Run(context.Background())
	if !result.Passed {
		t.Errorf("echo test 应通过，实际: %v", result.Summary())
	}
	if result.Total != 1 {
		t.Errorf("Total = %d, 应为 1", result.Total)
	}
	if result.PassedN != 1 {
		t.Errorf("PassedN = %d, 应为 1", result.PassedN)
	}
}

func TestEngineRunFailure(t *testing.T) {
	engine := NewEngine(Config{
		Commands: []string{"exit 1"},
	})
	result := engine.Run(context.Background())
	if result.Passed {
		t.Error("exit 1 应失败")
	}
	if result.FailedN != 1 {
		t.Errorf("FailedN = %d, 应为 1", result.FailedN)
	}
	if result.FailedOutput() == "" {
		t.Error("FailedOutput() 不应为空")
	}
}

func TestResultSummary(t *testing.T) {
	r := Result{Passed: true, Total: 3, PassedN: 3}
	if r.Summary() == "" {
		t.Error("Summary() 不应为空")
	}

	r2 := Result{Passed: false, Total: 3, PassedN: 1, FailedN: 2}
	if r2.Summary() == "" {
		t.Error("Summary() 不应为空")
	}
}

func TestDetectCommands(t *testing.T) {
	// Go 项目
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0o644)
	cmds := DetectCommands(dir)
	if len(cmds) == 0 {
		t.Error("Go 项目应检测到命令")
	}

	// Node.js 项目
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "package.json"), []byte("{}"), 0o644)
	cmds2 := DetectCommands(dir2)
	if len(cmds2) == 0 {
		t.Error("Node.js 项目应检测到命令")
	}

	// 空目录
	dir3 := t.TempDir()
	cmds3 := DetectCommands(dir3)
	// 空目录可能返回空或默认命令，不强制
	_ = cmds3
}
