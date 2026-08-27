package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome 把用户主目录指向临时目录，防止测试读到本机真实的 ~/.codecrew/config.json
func isolateHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	for _, k := range []string{"CREW_BASE_URL", "CREW_API_KEY", "CREW_MODEL", "CREW_WORKING_DIR", "CREW_DEFAULT_PERMISSION", "CREW_MAX_CONTEXT_TOKENS"} {
		t.Setenv(k, "")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMergesLayersWithPriority(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "codecrew.json"), `{"model":"a/one","providers":{"a":{"base_url":"https://a","api_key":"k-a"}}}`)
	write(t, filepath.Join(dir, "global.json"), `{"model":"b/two","providers":{"b":{"base_url":"https://b","api_key":"k-b"}}}`)

	cfg, err := Load(dir, filepath.Join(dir, "global.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "b/two" {
		t.Fatalf("显式路径应只加载一个文件，got %q", cfg.Model)
	}
	if len(cfg.Providers) != 1 || cfg.Providers["b"].APIKey != "k-b" {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	if cfg.MaxContextTokens != 24000 || cfg.MaxToolRounds != 12 {
		t.Fatalf("默认值未生效: %+v", cfg)
	}
}

func TestLoadMergesBaseDirThenProjectFile(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "codecrew.json"), `{"model":"p/m","providers":{"p":{"base_url":"https://p","api_key":"k-p"}}}`)

	cfg, err := Load("", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "p/m" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.Providers["p"].APIKey != "k-p" {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	if cfg.Source == "" {
		t.Fatal("应记录实际加载的文件")
	}
}

func TestLoadRejectsBrokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	write(t, path, `{"model": }`)
	if _, err := Load("", path); err == nil {
		t.Fatal("坏 JSON 应报错而不是静默忽略")
	} else if !strings.Contains(err.Error(), "解析失败") {
		t.Fatalf("报错信息应可定位: %v", err)
	}
}

func TestLoadMissingExplicitFileErrors(t *testing.T) {
	if _, err := Load("", filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("显式指定但不存在的配置文件应报错")
	}
}

func TestApplyEnvFallback(t *testing.T) {
	t.Setenv("CREW_BASE_URL", "http://127.0.0.1:9999/v1")
	t.Setenv("CREW_API_KEY", "sk-env")
	t.Setenv("CREW_MODEL", "my-model")
	t.Setenv("CREW_DEFAULT_PERMISSION", "allow")

	cfg, err := Load(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "env/my-model" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.Providers["env"].APIKey != "sk-env" {
		t.Fatalf("providers = %+v", cfg.Providers)
	}
	if got := cfg.PermissionFor("bash"); got != "allow" {
		t.Fatalf("通配权限 = %q", got)
	}
}

func TestResolve(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"deepseek": {BaseURL: "https://api.deepseek.com", APIKey: "k", Models: []string{"deepseek-chat"}},
		"nokey":    {BaseURL: "", APIKey: "k"},
	}}
	if _, _, err := cfg.Resolve(""); err == nil {
		t.Fatal("空 spec 应报错")
	}
	if _, _, err := cfg.Resolve("unknown/x"); err == nil {
		t.Fatal("未知供应商应报错")
	}
	if _, _, err := cfg.Resolve("deepseek/"); err == nil {
		t.Fatal("空模型名应报错")
	}
	p, model, err := cfg.Resolve("deepseek/deepseek-chat")
	if err != nil || p.BaseURL == "" || model != "deepseek-chat" {
		t.Fatalf("resolve 失败: %v %+v %q", err, p, model)
	}

	single := &Config{Providers: map[string]Provider{"only": {BaseURL: "https://only", APIKey: "k"}}}
	p, model, err = single.Resolve("no-slash-model")
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseURL != "https://only" || model != "no-slash-model" {
		t.Fatalf("单供应商应支持省略前缀: %+v %q", p, model)
	}
}

func TestModelSpecsAndProviderNamesAreSorted(t *testing.T) {
	cfg := &Config{Providers: map[string]Provider{
		"zeta":   {Models: []string{"z1", "z2"}},
		"alpha2": {Models: []string{"a1"}},
	}}
	specs := cfg.ModelSpecs()
	want := []string{"alpha2/a1", "zeta/z1", "zeta/z2"}
	if strings.Join(specs, ",") != strings.Join(want, ",") {
		t.Fatalf("specs = %v, want %v", specs, want)
	}
}

func TestMaskKey(t *testing.T) {
	cases := map[string]string{"": "(未填写)", "short": "sh****", "sk-1234567890abcd": "sk-123****abcd"}
	for in, want := range cases {
		if got := MaskKey(in); got != want {
			t.Fatalf("MaskKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripCommentsKeepsStrings(t *testing.T) {
	raw := "{\n  // 说明\n  \"url\": \"https://a\", // 尾注释\n  \"inside\": \"a//b\"\n}"
	var out map[string]any
	if err := json.Unmarshal(stripComments([]byte(raw)), &out); err != nil {
		t.Fatalf("去注释后应可解析: %v\n%s", err, stripComments([]byte(raw)))
	}
	if out["url"] != "https://a" || out["inside"] != "a//b" {
		t.Fatalf("解析结果异常: %+v", out)
	}
}

func TestWorkDirDefaultsAndOverride(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{WorkingDir: dir}
	if got := cfg.WorkDir(); got != filepath.Clean(dir) {
		t.Fatalf("WorkDir = %q, want %q", got, filepath.Clean(dir))
	}
	if got := (&Config{}).WorkDir(); got == "" {
		t.Fatal("缺省应回落到当前目录")
	}
}
