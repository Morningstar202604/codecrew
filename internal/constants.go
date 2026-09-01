package codecrew

// 全局常量定义，消除魔法数字。
const (
	// 默认配置
	DefaultMaxContextTokens = 24000
	DefaultMaxToolRounds    = 12

	// 推理
	DefaultReflectionDepth  = 1
	MaxReflectionDepth      = 3
	DefaultFailureStoreSize = 50
	DefaultRecentFailures   = 5

	// 验证
	DefaultMaxRepairRounds = 3
	DefaultVerifyTimeout   = 120
	MaxVerifyTimeout       = 600

	// 规划
	DefaultMaxTasks        = 8
	DefaultMaxAdjustRounds = 2

	// 知识
	DefaultIndexInterval    = 24 // 小时
	DefaultMaxSearchResults = 10
	DefaultContextLines     = 3
	DefaultEpisodicCount    = 5
	MaxEpisodicMemories     = 100

	// 工具
	MaxBashTimeout        = 300
	MaxSearchLimit        = 2000
	MaxSearchScanFiles    = 200000
	MaxSearchContextLines = 10
	MaxSearchResults      = 1000
	MaxCodeSearchLimit    = 20

	// 评估
	MaxEvalOutputLength = 50

	// 显示
	MaxRoleNameLength      = 20
	MaxThoughtBufferLength = 200
	MinComplexTaskLength   = 20
)
