package reasoning

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LLMClient 是反思引擎需要的 LLM 客户端接口。
// 由上层（repl）注入，保持 reasoning 包不依赖具体实现。
type LLMClient interface {
	// Complete 发送非流式请求，返回模型回复。
	Complete(ctx context.Context, messages []ChatMessage) (string, error)
}

// ChatMessage 是简化的聊天消息，用于反思引擎。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ReflexionEngine 反思引擎，负责触发反思、调用模型、存储结果。
type ReflexionEngine struct {
	client   LLMClient
	config   Config
	failures *FailureStore
}

// NewReflexionEngine 创建反思引擎。
func NewReflexionEngine(client LLMClient, config Config, failures *FailureStore) *ReflexionEngine {
	config.Validate()
	return &ReflexionEngine{
		client:   client,
		config:   config,
		failures: failures,
	}
}

// ReflectResult 是一次反思的结果。
type ReflectResult struct {
	Content  string
	Task     string
	Failed   bool
	Duration time.Duration
	Error    error
}

// Reflect 触发一次反思。
// task 是用户原始请求，summary 是执行过程摘要，failed 表示是否失败。
// role 是当前角色名，用于存储失败经验。
func (e *ReflexionEngine) Reflect(ctx context.Context, task, summary, role string, failed bool) *ReflectResult {
	start := time.Now()
	result := &ReflectResult{
		Task:   task,
		Failed: failed,
	}

	if e.client == nil {
		result.Error = fmt.Errorf("LLM 客户端未初始化")
		return result
	}

	prompt := ReflexionPrompt(task, summary, failed, e.config.ReflectionDepth)
	messages := []ChatMessage{
		{Role: "system", Content: "你是一个善于反思的 AI 助手。请客观分析任务执行过程，给出可操作的改进建议。"},
		{Role: "user", Content: prompt},
	}

	content, err := e.client.Complete(ctx, messages)
	if err != nil {
		result.Error = fmt.Errorf("反思调用失败: %w", err)
		return result
	}

	result.Content = strings.TrimSpace(content)
	result.Duration = time.Since(start)

	// 如果失败，存储失败经验
	if failed && e.failures != nil {
		f := Failure{
			Task:       task,
			Error:      summary,
			Role:       role,
			Timestamp:  time.Now(),
			Reflection: result.Content,
		}
		if err := e.failures.Add(role, f); err != nil {
			// 存储失败不影响反思结果
			_ = err
		}
	}

	return result
}

// ShouldReflect 判断是否应该触发反思。
func (e *ReflexionEngine) ShouldReflect(failed bool) bool {
	if e.config.Mode != ModeReflexion {
		return false
	}
	if failed {
		return true // 失败时总是反思
	}
	return e.config.AutoReflect
}

// InjectReflections 返回应该注入到 system prompt 的反思文本。
func (e *ReflexionEngine) InjectReflections(role string) string {
	if !e.config.InjectReflections || e.failures == nil {
		return ""
	}
	// 注入最近 5 条失败经验
	summary := e.failures.RecentSummary(role, 5)
	return InjectReflectionsPrompt(summary)
}

// Config 返回当前配置。
func (e *ReflexionEngine) Config() Config { return e.config }

// SetConfig 更新配置。
func (e *ReflexionEngine) SetConfig(c Config) {
	c.Validate()
	e.config = c
}
