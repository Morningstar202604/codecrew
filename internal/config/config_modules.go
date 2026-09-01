package config

import ()

// Provider 描述一个 OpenAI 兼容的上游。

type ReasoningConfig struct {
	// Mode 推理模式：standard / react / reflexion，默认 standard。
	Mode string `json:"mode,omitempty"`
	// ShowThoughts 是否显示思考过程，默认 true。
	ShowThoughts *bool `json:"show_thoughts,omitempty"`
	// AutoReflect 是否自动反思（仅 reflexion 模式），默认 true。
	AutoReflect *bool `json:"auto_reflect,omitempty"`
	// ReflectionDepth 反思深度 1-3，默认 1。
	ReflectionDepth int `json:"reflection_depth,omitempty"`
	// InjectReflections 是否注入历史反思，默认 true。
	InjectReflections *bool `json:"inject_reflections,omitempty"`
}

// Enabled 返回是否启用了非标准推理模式。
func (c ReasoningConfig) Enabled() bool {
	return c.Mode != "" && c.Mode != "standard"
}

// GetShowThoughts 返回是否显示思考过程。
func (c ReasoningConfig) GetShowThoughts() bool {
	if c.ShowThoughts == nil {
		return true
	}
	return *c.ShowThoughts
}

// GetAutoReflect 返回是否自动反思。
func (c ReasoningConfig) GetAutoReflect() bool {
	if c.AutoReflect == nil {
		return true
	}
	return *c.AutoReflect
}

// GetInjectReflections 返回是否注入历史反思。
func (c ReasoningConfig) GetInjectReflections() bool {
	if c.InjectReflections == nil {
		return true
	}
	return *c.InjectReflections
}

// VerifyConfig 代码验证与自愈配置。
type VerifyConfig struct {
	// Enabled 是否启用验证功能，默认 true。
	Enabled *bool `json:"enabled,omitempty"`
	// AutoVerify 是否在代码修改后自动验证，默认 true。
	AutoVerify *bool `json:"auto_verify,omitempty"`
	// Commands 验证命令列表，按顺序执行。为空时自动检测。
	Commands []string `json:"commands,omitempty"`
	// MaxRepairRounds 最大修复轮数，默认 3。
	MaxRepairRounds int `json:"max_repair_rounds,omitempty"`
	// TimeoutSeconds 单个命令超时时间，默认 120 秒。
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// GetEnabled 返回是否启用验证。
func (c VerifyConfig) GetEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetAutoVerify 返回是否自动验证。
func (c VerifyConfig) GetAutoVerify() bool {
	if c.AutoVerify == nil {
		return true
	}
	return *c.AutoVerify
}

// PlannerConfig 规划与执行分离配置。
type PlannerConfig struct {
	// Enabled 是否启用计划模式，默认 false（需要手动 /plan on 开启）。
	Enabled *bool `json:"enabled,omitempty"`
	// AutoPlan 是否自动触发规划（检测到复杂任务时），默认 false。
	AutoPlan *bool `json:"auto_plan,omitempty"`
	// MaxTasks 最大子任务数量，默认 8。
	MaxTasks int `json:"max_tasks,omitempty"`
	// AutoAdjust 是否自动调整计划（任务失败时），默认 true。
	AutoAdjust *bool `json:"auto_adjust,omitempty"`
	// MaxAdjustRounds 最大计划调整轮数，默认 2。
	MaxAdjustRounds int `json:"max_adjust_rounds,omitempty"`
}

// GetEnabled 返回是否启用计划模式。
func (c PlannerConfig) GetEnabled() bool {
	if c.Enabled == nil {
		return false
	}
	return *c.Enabled
}

// GetAutoPlan 返回是否自动触发规划。
func (c PlannerConfig) GetAutoPlan() bool {
	if c.AutoPlan == nil {
		return false
	}
	return *c.AutoPlan
}

// GetMaxTasks 返回最大子任务数量。
func (c PlannerConfig) GetMaxTasks() int {
	if c.MaxTasks <= 0 {
		return 8
	}
	return c.MaxTasks
}

// GetAutoAdjust 返回是否自动调整计划。
func (c PlannerConfig) GetAutoAdjust() bool {
	if c.AutoAdjust == nil {
		return true
	}
	return *c.AutoAdjust
}

// KnowledgeConfig 知识系统配置（代码库索引、语义检索、情景记忆）。
type KnowledgeConfig struct {
	// Enabled 是否启用知识系统，默认 true。
	Enabled *bool `json:"enabled,omitempty"`
	// AutoIndex 是否自动索引项目，默认 true。
	AutoIndex *bool `json:"auto_index,omitempty"`
	// IndexInterval 索引更新间隔（小时），默认 24。
	IndexInterval int `json:"index_interval,omitempty"`
	// MaxResults 搜索最大返回结果数，默认 10。
	MaxResults int `json:"max_results,omitempty"`
	// ContextLines 搜索结果上下文行数，默认 3。
	ContextLines int `json:"context_lines,omitempty"`
	// InjectEpisodic 是否注入情景记忆到 system prompt，默认 true。
	InjectEpisodic *bool `json:"inject_episodic,omitempty"`
	// EpisodicCount 注入的情景记忆数量，默认 5。
	EpisodicCount int `json:"episodic_count,omitempty"`
}

// GetEnabled 返回是否启用知识系统。
func (c KnowledgeConfig) GetEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// GetAutoIndex 返回是否自动索引。
func (c KnowledgeConfig) GetAutoIndex() bool {
	if c.AutoIndex == nil {
		return true
	}
	return *c.AutoIndex
}

// GetInjectEpisodic 返回是否注入情景记忆。
func (c KnowledgeConfig) GetInjectEpisodic() bool {
	if c.InjectEpisodic == nil {
		return true
	}
	return *c.InjectEpisodic
}

// GetEpisodicCount 返回注入的情景记忆数量。
func (c KnowledgeConfig) GetEpisodicCount() int {
	if c.EpisodicCount <= 0 {
		return 5
	}
	return c.EpisodicCount
}

// Config 是 CodeCrew 的全部可配置项。
