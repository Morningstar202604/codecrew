package repl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"codecrew/internal/disp"
	"codecrew/internal/llm"
	"codecrew/internal/role"
)

// defaultRoundtableRoles 是圆桌默认参与角色。
var defaultRoundtableRoles = []string{"architect", "developer", "reviewer"}

// roundtableSpeakerPrompt 是每个发言者的系统提示，告诉它这是圆桌讨论。
const roundtableSpeakerPrompt = `你正在参与一场多角色圆桌讨论。你的角色是 %s。
讨论规则：
- 仔细阅读之前所有角色的发言，针对其中你认同或反对的点给出回应
- 提出你自己角色视角下的独特观点，不要重复别人已经说过的话
- 用简洁的要点表达，每轮不超过 5 个要点
- 如果你改变了之前的看法，明确说明并解释原因
- 不要调用任何工具，只输出你的观点
用中文输出。`

// roundtableModeratorPrompt 是主持人总结用的提示。
const roundtableModeratorPrompt = `你是圆桌讨论的主持人。请根据以下完整讨论记录，输出：
1. **共识**：所有角色都同意的结论
2. **分歧**：角色之间存在的主要分歧点（注明谁支持、谁反对）
3. **建议方案**：综合各方观点后的推荐行动方案
用中文，结构清晰，不要遗漏任何角色的关键观点。`

// RunRoundtable 执行圆桌讨论。topic 是讨论话题，rounds 是轮数（默认 2，最多 5）。
func (r *REPL) RunRoundtable(topic string, rounds int) error {
	if r.client == nil {
		return fmt.Errorf("还没有可用的模型，先配置再运行圆桌讨论")
	}
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return fmt.Errorf("讨论话题不能为空")
	}
	if rounds <= 0 {
		rounds = 2
	}
	if rounds > 5 {
		rounds = 5
	}

	// 检查角色
	participants := make([]role.Role, 0, len(defaultRoundtableRoles))
	for _, name := range defaultRoundtableRoles {
		rr, ok := role.Get(r.roles, name)
		if !ok {
			return fmt.Errorf("圆桌需要角色 %q，但当前未加载", name)
		}
		participants = append(participants, rr)
	}

	fmt.Fprintf(r.out, "\n%s 圆桌讨论：%s\n", bright("💬"), disp.Truncate(topic, 80))
	fmt.Fprintf(r.out, "  参与角色：%s\n", strings.Join(defaultRoundtableRoles, " · "))
	fmt.Fprintf(r.out, "  讨论轮数：%d 轮（每轮每人发言一次）\n", rounds)
	fmt.Fprintln(r.out, "  "+dim("讨论结束后自动生成共识、分歧与建议方案"))

	// 辩论历史：所有发言按时间顺序排列
	var debate []llm.Message
	debate = append(debate, llm.TextMessage("user", "讨论话题："+topic))

	var allSpeeches []string
	start := time.Now()

	for round := 1; round <= rounds; round++ {
		fmt.Fprintf(r.out, "\n%s 第 %d/%d 轮\n", bright("──"), round, rounds)
		fmt.Fprintln(r.out, "  "+dim(strings.Repeat("─", 50)))

		for _, speaker := range participants {
			fmt.Fprintf(r.out, "\n  %s %s\n", bright("▸"), speaker.Name)

			// 构建该发言者的历史：系统提示 + 话题 + 之前所有发言
			systemPrompt := fmt.Sprintf(roundtableSpeakerPrompt, speaker.Name+"（"+speaker.Description+"）")
			history := []llm.Message{llm.TextMessage("system", systemPrompt)}
			history = append(history, debate...)
			history = append(history, llm.TextMessage("user", fmt.Sprintf("请以 %s 的身份发言。", speaker.Name)))

			text, err := r.completeRoundtableTurn(history)
			if err != nil {
				fmt.Fprintf(r.out, "  %s %s 发言失败：%v\n", bright("✗"), speaker.Name, err)
				continue
			}
			text = strings.TrimSpace(text)
			if text == "" {
				text = "（无发言）"
			}
			fmt.Fprintln(r.out)

			// 记录发言
			speech := fmt.Sprintf("【%s · 第%d轮】\n%s", speaker.Name, round, text)
			allSpeeches = append(allSpeeches, speech)
			debate = append(debate, llm.TextMessage("assistant", speaker.Name+": "+text))
		}
	}

	// 主持人总结
	fmt.Fprintf(r.out, "\n%s 主持人总结\n", bright("──"))
	fmt.Fprintln(r.out, "  "+dim(strings.Repeat("─", 50)))

	summary, err := r.roundtableSummary(topic, allSpeeches)
	if err != nil {
		fmt.Fprintf(r.out, "  %s 总结生成失败：%v\n", bright("✗"), err)
		// 即使总结失败，也把讨论记录写入历史
		summary = fmt.Sprintf("（总结生成失败：%v）\n\n讨论记录：\n%s", err, strings.Join(allSpeeches, "\n\n"))
	}
	fmt.Fprintln(r.out)

	elapsed := time.Since(start).Round(time.Second)
	fullResult := fmt.Sprintf("圆桌讨论结果（耗时 %s）\n\n话题：%s\n参与角色：%s\n轮数：%d\n\n%s",
		elapsed, topic, strings.Join(defaultRoundtableRoles, " · "), rounds, summary)

	// 写入主对话历史
	r.history = append(r.history, llm.TextMessage("user", "圆桌讨论："+topic))
	r.history = append(r.history, llm.TextMessage("assistant", fullResult))
	r.appendSession(llm.TextMessage("user", "圆桌讨论："+topic))
	r.appendSession(llm.TextMessage("assistant", fullResult))

	return nil
}

// completeRoundtableTurn 执行一轮非工具调用的补全（圆桌发言不需要工具）。
func (r *REPL) completeRoundtableTurn(history []llm.Message) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	text, err := r.client.Complete(ctx, history)
	if err != nil {
		return "", err
	}
	r.usage.turns++
	// 统计 prompt 和 completion tokens
	var promptText strings.Builder
	for _, m := range history {
		promptText.WriteString(m.Content)
	}
	r.usage.prompt += estimateTokens(promptText.String())
	r.usage.completion += estimateTokens(text)
	return text, nil
}

// roundtableSummary 用主持人提示生成讨论总结。
func (r *REPL) roundtableSummary(topic string, speeches []string) (string, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "讨论话题：%s\n\n", topic)
	for _, s := range speeches {
		sb.WriteString(s)
		sb.WriteString("\n\n")
	}
	history := []llm.Message{
		llm.TextMessage("system", roundtableModeratorPrompt),
		llm.TextMessage("user", sb.String()),
	}
	return r.completeRoundtableTurn(history)
}

// parseRoundtableArgs 解析 "/roundtable 话题 [轮数]" 格式。
// 只有当末尾是 1-5 的纯数字时才认为是轮数（最大轮数为 5），
// 避免把话题末尾的数字（如 "讨论 Go 2"）误解析为轮数。
func parseRoundtableArgs(arg string) (string, int) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", 0
	}
	fields := strings.Fields(arg)
	if len(fields) >= 2 {
		last := fields[len(fields)-1]
		if n, err := strconv.Atoi(last); err == nil && n >= 1 && n <= 5 {
			topic := strings.Join(fields[:len(fields)-1], " ")
			if topic != "" {
				return topic, n
			}
		}
	}
	return arg, 0
}
