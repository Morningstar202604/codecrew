package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codecrew/internal/config"
	"codecrew/internal/llm"
	"codecrew/internal/role"
	"codecrew/internal/tool"
)

var base = exeDir()

func main() {
	var err error
	cfg, err = config.Load(base)
	fatal(err)

	roles, err := role.Load(filepath.Join(base, "roles"))
	fatal(err)
	if len(roles) == 0 {
		fatal(fmt.Errorf("角色文件夹为空，请在 roles/ 目录下添加 .md 文件"))
	}

	current = roles[0]
	registry = tool.NewDefaultRegistry(base)
	applyRoleTools(current, registry)

	client = buildClient(cfg)

	scanner := bufio.NewScanner(os.Stdin)
	printWelcome(roles, current)

	history = []llm.Message{{Role: "system", Content: current.Prompt}}

	for {
		modelName := currentModelName()
		if modelName == "" {
			modelName = "未配置"
		}
		fmt.Printf("\n%s → %s\n> ", current.Name, modelName)
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch {
		case input == "/exit" || input == "/quit" || input == "退出":
			fmt.Println("\n再见！")
			return

		case input == "/help" || input == "帮助":
			printHelp()

		case input == "/roles" || input == "角色":
			printRoles()

		case input == "/config" || input == "配置":
			printConfig()

		case input == "/model" || input == "模型":
			printModels()

		case input == "/reload" || input == "重载":
			handleReload()

		case strings.HasPrefix(input, "/role ") || strings.HasPrefix(input, "角色 "):
			name := strings.TrimPrefix(strings.TrimPrefix(input, "/role "), "角色 ")
			switchRole(name)

		case strings.HasPrefix(input, "/model ") || strings.HasPrefix(input, "模型 "):
			modelSpec := strings.TrimPrefix(strings.TrimPrefix(input, "/model "), "模型 ")
			switchModel(modelSpec)

		default:
			sendMessage()
		}
	}
}

var (
	cfg      *config.Config
	current  role.Role
	registry *tool.Registry
	client   *llm.Client
	history  []llm.Message
)

func printWelcome(roles []role.Role, cur role.Role) {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════════╗")
	fmt.Println("  ║          CodeCrew — 你的 AI 开发团队          ║")
	fmt.Println("  ╚══════════════════════════════════════════════╝")
	fmt.Println()

	if cfg == nil || cfg.Empty() {
		fmt.Println("  首次使用？三步搞定：")
		fmt.Println()
		fmt.Println("  第 1 步  复制配置模板")
		fmt.Println("           cp codecrew.example.json codecrew.json")
		fmt.Println()
		fmt.Println("  第 2 步  打开 codecrew.json，填入你的 API 密钥")
		fmt.Println("           支持: DeepSeek / 通义千问 / Kimi / 智谱 / OpenAI")
		fmt.Println()
		fmt.Println("  第 3 步  回到这里输入 /reload")
		fmt.Println()
	} else {
		providerName, modelID, _ := strings.Cut(cfg.Model, "/")
		fmt.Printf("  当前模型: %s（%s）\n", modelID, providerName)
		fmt.Println()
	}

	fmt.Println("  可用角色:")
	for _, r := range roles {
		marker := "  "
		if r.Name == cur.Name {
			marker = "→ "
		}
		fmt.Printf("    %s%-12s %s\n", marker, r.Name, r.Description)
	}
	fmt.Println()
	fmt.Println("  输入 /help 查看所有命令")
	fmt.Println("  输入 /exit 退出")
	fmt.Println()
}

func printHelp() {
	fmt.Println()
	fmt.Println("  命令列表:")
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Println("  /help       显示此帮助")
	fmt.Println("  /roles      查看所有角色")
	fmt.Println("  /role XX    切换到指定角色")
	fmt.Println("  /model      查看可用模型")
	fmt.Println("  /model XX   切换模型（格式: 供应商/模型名）")
	fmt.Println("  /config     查看配置")
	fmt.Println("  /reload     重新加载配置文件")
	fmt.Println("  /exit       退出")
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Println("  直接输入文字即可对话，支持中文")
	fmt.Println()
}

func printRoles() {
	fmt.Println()
	roles, err := role.Load(filepath.Join(base, "roles"))
	if err != nil {
		fmt.Println("  ✗ 读取角色失败:", err)
		return
	}
	fmt.Println()
	fmt.Println("  可用角色:")
	for _, r := range roles {
		fmt.Printf("    %-14s %s\n", r.Name, r.Description)
	}
	fmt.Println()
	fmt.Println("  切换方法: /role 开发者  或  /role reviewer")
	fmt.Println()
}

func printConfig() {
	fmt.Println()
	if cfg == nil || cfg.Empty() {
		fmt.Println("  当前状态: 未配置")
		fmt.Println()
		fmt.Println("  操作步骤:")
		fmt.Println("    1. 复制配置模板: cp codecrew.example.json codecrew.json")
		fmt.Println("    2. 打开 codecrew.json，填入你的 API 密钥")
		fmt.Println("    3. 回到这里输入 /reload")
		fmt.Println()
		return
	}

	fmt.Println("  已配置的供应商:")
	for name, p := range cfg.Providers {
		status := "✓"
		if p.APIKey == "" {
			status = "✗ 缺少密钥"
		}
		fmt.Printf("    %s %-12s %s\n", status, name, p.BaseURL)
		if len(p.Models) > 0 {
			fmt.Printf("      可用模型: %s\n", strings.Join(p.Models, ", "))
		}
	}

	fmt.Println()
	if cfg.Model != "" {
		fmt.Printf("  当前模型: %s\n", cfg.Model)
	} else {
		fmt.Println("  当前模型: 未设置")
	}
	fmt.Println()
	fmt.Println("  配置文件位置:")
	for _, path := range config.Paths(base) {
		fmt.Printf("    %s\n", path)
	}
	fmt.Println()
}

func printModels() {
	fmt.Println()
	if cfg == nil || cfg.Empty() {
		fmt.Println("  还没有配置任何供应商")
		fmt.Println("  请先配置 codecrew.json（输入 /config 查看步骤）")
		fmt.Println()
		return
	}

	fmt.Println("  可用模型:")
	for name, p := range cfg.Providers {
		for _, m := range p.Models {
			spec := name + "/" + m
			cursor := "  "
			if spec == cfg.Model {
				cursor = "→ "
			}
			fmt.Printf("    %s%s\n", cursor, spec)
		}
	}
	fmt.Println()
	fmt.Println("  切换方法: /model deepseek/deepseek-chat")
	fmt.Println()
}

func switchRole(name string) {
	roles, _ := role.Load(filepath.Join(base, "roles"))
	for _, r := range roles {
		if r.Name == name {
			current = r
			applyRoleTools(r, registry)
			history = append(history, llm.Message{
				Role:    "system",
				Content: "角色切换为 " + r.Name + "，从现在起严格遵守新角色的职责与行为准则：" + r.Prompt,
			})
			fmt.Printf("  ✓ 已切换到: %s\n", r.Name)
			return
		}
	}
	fmt.Printf("  ✗ 未找到角色: %s（输入 /roles 查看可用角色）\n", name)
}

func switchModel(modelSpec string) {
	provider, modelID, err := cfg.Resolve(modelSpec)
	if err != nil {
		fmt.Printf("  ✗ 切换失败: %s\n", err)
		return
	}
	cfg.Model = modelSpec
	fmt.Printf("  ✓ 已切换模型: %s @ %s\n", modelID, provider.BaseURL)
}

func handleReload() {
	_, err := config.Load(base)
	if err != nil {
		fmt.Println("  ✗ 重载失败:", err)
		return
	}
	applyRoleTools(current, registry)
	fmt.Println("  ✓ 配置已刷新")
}

func currentModelName() string {
	if cfg == nil || cfg.Empty() || cfg.Model == "" {
		return ""
	}
	_, modelID, _ := strings.Cut(cfg.Model, "/")
	return modelID
}

func applyRoleTools(r role.Role, reg *tool.Registry) {
	allowed := map[string]bool{"read": true}
	for _, t := range r.Tools {
		allowed[t] = true
	}
	for _, name := range reg.AllToolNames() {
		reg.SetAllowed(name, allowed[name])
	}
}

func buildClient(cfg *config.Config) *llm.Client {
	if cfg == nil || cfg.Empty() || cfg.Model == "" {
		return nil
	}
	provider, modelID, err := cfg.Resolve(cfg.Model)
	if err != nil {
		return nil
	}
	return llm.New(provider.BaseURL, provider.APIKey, modelID)
}

func sendMessage() {
	if client == nil {
		fmt.Println("  ⚠ 请先配置模型（/config 查看步骤）")
		return
	}
	fmt.Println()
	ctx := context.Background()

	var toolCalls []llm.ToolCall

	err := client.Stream(ctx, history, registry.AllSchemas(),
		func(delta string) { fmt.Print(delta) },
		func(calls []llm.ToolCall) { toolCalls = calls },
	)
	fmt.Println()
	if err != nil {
		fmt.Println("  ✗ 出错了:", err)
		return
	}

	if len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				fmt.Printf("  ✗ 参数解析失败: %v\n", err)
				continue
			}
			result, err := registry.Execute(context.Background(), tc.Function.Name, args)
			if err != nil {
				fmt.Printf("  ✗ 工具 %s 执行失败: %v\n", tc.Function.Name, err)
				continue
			}
			fmt.Printf("  🔧 %s → %s\n", tc.Function.Name, result)
		}
	}
}

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}
