package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

// Config 是 CodeCrew 的全部可配置项。
type Config struct {
	// Model 形如 "供应商/模型名"，例如 deepseek/deepseek-chat。
	Model     string              `json:"model,omitempty"`
	Providers map[string]Provider `json:"providers,omitempty"`
	// WorkingDir 是工具读写的用户项目根目录，默认为进程当前目录。
	WorkingDir       string            `json:"working_dir,omitempty"`
	Permissions      map[string]string `json:"permissions,omitempty"`
	MaxContextTokens int               `json:"max_context_tokens,omitempty"`
	MaxToolRounds    int               `json:"max_tool_rounds,omitempty"`
	Temperature      *float64          `json:"temperature,omitempty"`
	Source           string            `json:"-"` // 实际加载到的文件，仅用于展示
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
}

// applyEnv 支持 CREW_* 环境变量兜底，键存在才生效。
func applyEnv(cfg *Config) {
	baseURL := os.Getenv("CREW_BASE_URL")
	apiKey := os.Getenv("CREW_API_KEY")
	model := os.Getenv("CREW_MODEL")
	if baseURL != "" || apiKey != "" || model != "" {
		if cfg.Providers == nil {
			cfg.Providers = map[string]Provider{}
		}
		provider := cfg.Providers["env"]
		provider.BaseURL = firstNonEmpty(provider.BaseURL, baseURL, "https://api.deepseek.com")
		provider.APIKey = firstNonEmpty(provider.APIKey, apiKey)
		custom := model
		if custom != "" {
			if strings.Contains(custom, "/") {
				// 用户指定了完整的 供应商/模型名，直接使用
				if cfg.Model == "" {
					cfg.Model = custom
				}
			} else {
				// 只有模型名，当作 env 供应商的模型
				if !contains(provider.Models, custom) {
					provider.Models = append([]string{custom}, provider.Models...)
				}
				if cfg.Model == "" {
					cfg.Model = "env/" + custom
				}
			}
		}
		cfg.Providers["env"] = provider
	}
	if wd := os.Getenv("CREW_WORKING_DIR"); wd != "" && cfg.WorkingDir == "" {
		cfg.WorkingDir = wd
	}
	if v := os.Getenv("CREW_MAX_CONTEXT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxContextTokens = n
		}
	}
	if v := os.Getenv("CREW_DEFAULT_PERMISSION"); v != "" {
		cfg.Permissions = setPermission(cfg.Permissions, "*", v)
	}
}

func setPermission(m map[string]string, key, value string) map[string]string {
	if m == nil {
		m = map[string]string{}
	}
	m[key] = value
	return m
}

// Empty 表示没有任何可用供应商。
func (c *Config) Empty() bool { return len(c.Providers) == 0 }

// Resolve 把 "供应商/模型名" 解析为具体的 Provider 与模型 ID。
func (c *Config) Resolve(spec string) (Provider, string, error) {
	if spec == "" {
		return Provider{}, "", fmt.Errorf("未指定模型")
	}
	name, modelID, found := strings.Cut(spec, "/")
	if !found {
		if len(c.Providers) == 1 {
			for _, provider := range c.Providers {
				return provider, spec, nil
			}
		}
		return Provider{}, "", fmt.Errorf("模型格式应为 供应商/模型名，例如 deepseek/deepseek-chat；当前供应商: %v", c.ProviderNames())
	}
	provider, ok := c.Providers[name]
	if !ok {
		return Provider{}, "", fmt.Errorf("未知供应商 %q，已配置: %v", name, c.ProviderNames())
	}
	if provider.BaseURL == "" {
		return Provider{}, "", fmt.Errorf("供应商 %q 缺少 base_url", name)
	}
	if modelID == "" {
		return Provider{}, "", fmt.Errorf("供应商 %q 缺少模型名，例如 %s/deepseek-chat", name, name)
	}
	return provider, modelID, nil
}

// ModelSpecs 返回全部 "供应商/模型名" 组合；未声明 models 时用模型名占位。
func (c *Config) ModelSpecs() []string {
	var specs []string
	for _, name := range c.ProviderNames() {
		for _, m := range c.Providers[name].Models {
			specs = append(specs, name+"/"+m)
		}
	}
	return specs
}

// ProviderNames 返回排序后的供应商名。
func (c *Config) ProviderNames() []string {
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// PermissionFor 查询某工具的权限档位（allow/ask/deny），"*" 为通配兜底。
func (c *Config) PermissionFor(tool string) string {
	if v, ok := c.Permissions[tool]; ok {
		return normalizePermission(v)
	}
	if v, ok := c.Permissions["*"]; ok {
		return normalizePermission(v)
	}
	return ""
}

func normalizePermission(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "allow", "true", "y", "auto":
		return "allow"
	case "deny", "block", "off", "never":
		return "deny"
	default:
		return "ask"
	}
}

// MaskKey 对密钥脱敏，便于打印。
func MaskKey(key string) string {
	switch {
	case key == "":
		return "(未填写)"
	case len(key) <= 10:
		return key[:2] + "****"
	default:
		return key[:6] + "****" + key[len(key)-4:]
	}
}

// WorkDir 返回工具使用的工作目录（用户项目根），默认当前目录。
func (c *Config) WorkDir() string {
	if c.WorkingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "."
		}
		return wd
	}
	abs, err := filepath.Abs(c.WorkingDir)
	if err != nil {
		return c.WorkingDir
	}
	return abs
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// stripComments 去掉 JSON 中的 // 行注释（便于配置模板加说明），字符串内的 // 会被保留。
func stripComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	for _, line := range strings.Split(string(data), "\n") {
		out = append(out, []byte(stripLineComment(line))...)
		out = append(out, '\n')
	}
	return out
}

func stripLineComment(line string) string {
	inString := false
	escaped := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if escaped {
			escaped = false
			continue
		}
		switch {
		case inString && c == '\\':
			escaped = true
		case c == '"':
			inString = !inString
		case !inString && c == '/' && i+1 < len(line) && line[i+1] == '/':
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}
