package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Provider 描述一个 OpenAI 兼容的上游。
type Provider struct {
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key"`
	Models  []string `json:"models,omitempty"`
	// InputPrice / OutputPrice 单位：美元 / 1K tokens，用于成本估算。
	// 不填则该供应商不参与成本计算（显示为"未配置单价"）。
	InputPrice  float64 `json:"input_price,omitempty"`
	OutputPrice float64 `json:"output_price,omitempty"`
}

// MCPServer 描述一个 MCP（Model Context Protocol）服务器。
type MCPServer struct {
	// Command 是服务器可执行文件路径（如 "npx"、"python3"）。
	Command string `json:"command"`
	// Args 是启动参数（如 ["-y", "@modelcontextprotocol/server-filesystem", "/path"]）。
	Args []string `json:"args,omitempty"`
	// Disabled 为 true 时不启动该服务器。
	Disabled bool `json:"disabled,omitempty"`
}

// Config 是 CodeCrew 的全部可配置项。
type Config struct {
	// Model 形如 "供应商/模型名"，例如 deepseek/deepseek-chat。
	Model     string              `json:"model,omitempty"`
	Providers map[string]Provider `json:"providers,omitempty"`
	// MCPServers 是 MCP 服务器配置，启动时自动连接并注册工具。
	MCPServers map[string]MCPServer `json:"mcp_servers,omitempty"`
	// WorkingDir 是工具读写的用户项目根目录，默认为进程当前目录。
	WorkingDir       string            `json:"working_dir,omitempty"`
	Permissions      map[string]string `json:"permissions,omitempty"`
	MaxContextTokens int               `json:"max_context_tokens,omitempty"`
	MaxToolRounds    int               `json:"max_tool_rounds,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	// Reasoning 推理范式配置（standard / react / reflexion）。
	Reasoning ReasoningConfig `json:"reasoning,omitempty"`
	// Verify 代码验证与自愈配置。
	Verify    VerifyConfig    `json:"verify,omitempty"`
	Planner   PlannerConfig   `json:"planner,omitempty"`
	Knowledge KnowledgeConfig `json:"knowledge,omitempty"`
	Source    string          `json:"-"` // 实际加载到的文件，仅用于展示
	// Language 是界面语言，支持 zh-CN / en-US，默认 zh-CN。
	Language string `json:"language,omitempty"`
}

func (c *Config) defaults() {
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	if c.MaxContextTokens <= 0 {
		c.MaxContextTokens = 24000
	}
	if c.MaxToolRounds <= 0 {
		c.MaxToolRounds = 12
	}
}

// Paths 返回配置候选路径，按优先级从高到低；explicit 非空时只用它。

func Paths(baseDir, explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	paths := []string{}
	if baseDir != "" {
		paths = append(paths, filepath.Join(baseDir, "codecrew.json"))
	}
	paths = append(paths, "codecrew.json")
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "codecrew.json")
		if p != "codecrew.json" && !contains(paths, p) {
			paths = append(paths, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".codecrew", "config.json"))
	}
	return dedupe(paths)
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func dedupe(list []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// Load 按优先级合并配置文件，最后叠加环境变量兜底。
func Load(baseDir, explicit string) (*Config, error) {
	cfg := &Config{}
	loaded := false
	for _, path := range Paths(baseDir, explicit) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		partial := &Config{}
		if err := json.Unmarshal(stripComments(data), partial); err != nil {
			return nil, fmt.Errorf("配置文件 %s 解析失败: %w", path, err)
		}
		loaded = true
		merge(cfg, partial)
		cfg.Source = path
	}
	if explicit != "" && !loaded {
		return nil, fmt.Errorf("配置文件不存在: %s", explicit)
	}
	applyEnv(cfg)
	cfg.defaults()
	return cfg, nil
}

func merge(dst, src *Config) {
	for name, provider := range src.Providers {
		if dst.Providers == nil {
			dst.Providers = map[string]Provider{}
		}
		if provider.BaseURL == "" && provider.APIKey == "" && len(provider.Models) == 0 {
			continue
		}
		dst.Providers[name] = provider
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.WorkingDir != "" {
		dst.WorkingDir = src.WorkingDir
	}
	for k, v := range src.Permissions {
		if dst.Permissions == nil {
			dst.Permissions = map[string]string{}
		}
		dst.Permissions[k] = v
	}
	if src.MaxContextTokens > 0 {
		dst.MaxContextTokens = src.MaxContextTokens
	}
	if src.MaxToolRounds > 0 {
		dst.MaxToolRounds = src.MaxToolRounds
	}
	if src.Temperature != nil {
		dst.Temperature = src.Temperature
	}
	// 合并 Reasoning 配置
	if src.Reasoning.Mode != "" {
		dst.Reasoning.Mode = src.Reasoning.Mode
	}
	if src.Reasoning.ShowThoughts != nil {
		dst.Reasoning.ShowThoughts = src.Reasoning.ShowThoughts
	}
	if src.Reasoning.AutoReflect != nil {
		dst.Reasoning.AutoReflect = src.Reasoning.AutoReflect
	}
	if src.Reasoning.ReflectionDepth > 0 {
		dst.Reasoning.ReflectionDepth = src.Reasoning.ReflectionDepth
	}
	if src.Reasoning.InjectReflections != nil {
		dst.Reasoning.InjectReflections = src.Reasoning.InjectReflections
	}
	// 合并 Verify 配置
	if src.Verify.Enabled != nil {
		dst.Verify.Enabled = src.Verify.Enabled
	}
	if src.Verify.AutoVerify != nil {
		dst.Verify.AutoVerify = src.Verify.AutoVerify
	}
	if len(src.Verify.Commands) > 0 {
		dst.Verify.Commands = src.Verify.Commands
	}
	if src.Verify.MaxRepairRounds > 0 {
		dst.Verify.MaxRepairRounds = src.Verify.MaxRepairRounds
	}
	if src.Verify.TimeoutSeconds > 0 {
		dst.Verify.TimeoutSeconds = src.Verify.TimeoutSeconds
	}
	// 合并 Planner 配置
	if src.Planner.Enabled != nil {
		dst.Planner.Enabled = src.Planner.Enabled
	}
	if src.Planner.AutoPlan != nil {
		dst.Planner.AutoPlan = src.Planner.AutoPlan
	}
	if src.Planner.MaxTasks > 0 {
		dst.Planner.MaxTasks = src.Planner.MaxTasks
	}
	if src.Planner.AutoAdjust != nil {
		dst.Planner.AutoAdjust = src.Planner.AutoAdjust
	}
	if src.Planner.MaxAdjustRounds > 0 {
		dst.Planner.MaxAdjustRounds = src.Planner.MaxAdjustRounds
	}
	// 合并 Knowledge 配置
	if src.Knowledge.Enabled != nil {
		dst.Knowledge.Enabled = src.Knowledge.Enabled
	}
	if src.Knowledge.AutoIndex != nil {
		dst.Knowledge.AutoIndex = src.Knowledge.AutoIndex
	}
	if src.Knowledge.IndexInterval > 0 {
		dst.Knowledge.IndexInterval = src.Knowledge.IndexInterval
	}
	if src.Knowledge.MaxResults > 0 {
		dst.Knowledge.MaxResults = src.Knowledge.MaxResults
	}
	if src.Knowledge.ContextLines > 0 {
		dst.Knowledge.ContextLines = src.Knowledge.ContextLines
	}
	if src.Knowledge.InjectEpisodic != nil {
		dst.Knowledge.InjectEpisodic = src.Knowledge.InjectEpisodic
	}
	if src.Knowledge.EpisodicCount > 0 {
		dst.Knowledge.EpisodicCount = src.Knowledge.EpisodicCount
	}
}

// applyEnv 支持 CREW_* 环境变量兜底，键存在才生效。
