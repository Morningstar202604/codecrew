package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Provider struct {
	BaseURL string   `json:"base_url"`
	APIKey  string   `json:"api_key"`
	Models  []string `json:"models,omitempty"`
}

type Config struct {
	Providers map[string]Provider `json:"providers,omitempty"`
	Model     string              `json:"model,omitempty"`
}

func Paths(baseDir string) []string {
	global := ""
	if home, err := os.UserHomeDir(); err == nil {
		global = filepath.Join(home, ".codecrew", "config.json")
	}
	paths := []string{"codecrew.json"}
	if baseDir != "" {
		paths = append([]string{filepath.Join(baseDir, "codecrew.json")}, paths...)
	}
	if global != "" {
		paths = append(paths, global)
	}
	return paths
}

func Load(baseDir string) (*Config, error) {
	cfg := &Config{}
	for _, path := range Paths(baseDir) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		partial := &Config{}
		if err := json.Unmarshal(data, partial); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for name, provider := range partial.Providers {
			if cfg.Providers == nil {
				cfg.Providers = map[string]Provider{}
			}
			cfg.Providers[name] = provider
		}
		if partial.Model != "" {
			cfg.Model = partial.Model
		}
	}
	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	baseURL := os.Getenv("CREW_BASE_URL")
	apiKey := os.Getenv("CREW_API_KEY")
	model := os.Getenv("CREW_MODEL")
	if baseURL == "" && apiKey == "" && model == "" {
		return
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]Provider{}
	}
	provider := cfg.Providers["env"]
	provider.BaseURL = firstNonEmpty(provider.BaseURL, baseURL, "https://api.deepseek.com")
	provider.APIKey = apiKey
	provider.Models = append(provider.Models, "deepseek-chat")
	cfg.Providers["env"] = provider
	if cfg.Model == "" {
		cfg.Model = "env/" + firstNonEmpty(model, "deepseek-chat")
	}
}

func (c *Config) Empty() bool {
	return len(c.Providers) == 0
}

func (c *Config) Resolve(spec string) (Provider, string, error) {
	if spec == "" {
		return Provider{}, "", fmt.Errorf("未指定模型")
	}
	name, modelID, found := strings.Cut(spec, "/")
	if !found {
		if len(c.Providers) == 1 {
			for _, provider := range c.Providers {
				return provider, specWithFallback(provider.Models, spec), nil
			}
		}
		return Provider{}, "", fmt.Errorf("模型格式应为 供应商/模型名，例如 deepseek/deepseek-chat")
	}
	provider, ok := c.Providers[name]
	if !ok {
		return Provider{}, "", fmt.Errorf("未知供应商 %q，已在配置中的: %v", name, c.ProviderNames())
	}
	if provider.BaseURL == "" {
		return Provider{}, "", fmt.Errorf("供应商 %q 缺少 base_url", name)
	}
	return provider, modelID, nil
}

func specWithFallback(models []string, spec string) string {
	if len(models) > 0 {
		return models[0]
	}
	return spec
}

func (c *Config) ProviderNames() []string {
	names := []string{}
	for name := range c.Providers {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
