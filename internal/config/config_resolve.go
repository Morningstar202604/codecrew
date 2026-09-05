package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Provider 描述一个 OpenAI 兼容的上游。

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
