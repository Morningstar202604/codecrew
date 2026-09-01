package i18n

// zhCN 是简体中文翻译字典。
// key 用英文描述，value 是中文翻译。
var zhCN = Dictionary{
	// 欢迎页
	"welcome.title":    "CodeCrew — 多角色协作 AI 编程助手",
	"welcome.subtitle": "像指挥一个开发团队一样指挥 AI",
	"welcome.quick":    "快速开始",
	"welcome.help":     "输入 /help 查看所有命令",

	// 通用
	"common.ok":      "✓",
	"common.fail":    "✗",
	"common.warning": "⚠",
	"common.loading": "加载中...",
	"common.none":    "无",
	"common.unknown": "未知",
	"common.confirm": "确认",
	"common.cancel":  "取消",
	"common.yes":     "是",
	"common.no":      "否",

	// 角色
	"role.current":   "当前角色",
	"role.available": "可用角色",
	"role.switched":  "已切换到角色 %s",
	"role.not_found": "角色 %s 不存在",
	"role.tools":     "当前角色工具",

	// 模型
	"model.current":   "当前模型",
	"model.switched":  "已切换到模型 %s",
	"model.invalid":   "模型格式应为 供应商/模型名",
	"model.not_found": "未知供应商 %s",

	// 配置
	"config.title":    "当前配置",
	"config.reloaded": "配置已重载",
	"config.source":   "配置来源",

	// 工具
	"tools.title":   "可用工具",
	"tools.allow":   "已放行",
	"tools.ask":     "需确认",
	"tools.deny":    "已禁用",
	"tools.allowed": "工具 %s 已放行",

	// 权限确认
	"perm.ask":    "是否允许执行？",
	"perm.always": "总是允许",
	"perm.once":   "仅本次",
	"perm.deny":   "拒绝",

	// 上下文
	"context.title":     "上下文使用情况",
	"context.used":      "已使用",
	"context.limit":     "限制",
	"context.compact":   "压缩上下文",
	"context.compacted": "上下文已压缩",
	"context.cleared":   "上下文已清空",

	// 成本
	"cost.title":     "本次会话成本",
	"cost.turns":     "轮模型调用",
	"cost.tokens":    "tokens",
	"cost.input":     "输入",
	"cost.output":    "输出",
	"cost.total":     "总计",
	"cost.elapsed":   "用时",
	"cost.provider":  "供应商",
	"cost.estimated": "估算花费",
	"cost.no_price":  "未配置单价，无法估算花费",
	"cost.note":      "供应商计费口径可能不同，此处仅作量级参考",

	// 会话
	"session.saved":   "会话已保存",
	"session.new":     "已创建新会话",
	"session.resumed": "已恢复会话 %s",
	"session.list":    "历史会话",
	"session.none":    "还没有历史会话",

	// 错误
	"error.generic":    "操作失败: %v",
	"error.no_config":  "未找到配置文件，请运行 codecrew --init 创建",
	"error.model_call": "模型调用失败: %v",

	// 退出
	"exit.goodbye": "再见！会话已保存，可用 /resume 续聊。",

	// 流水线
	"pipeline.start": "开始流水线: %s",
	"pipeline.stage": "阶段 %d/%d: %s",
	"pipeline.done":  "流水线完成",
	"pipeline.fail":  "流水线失败: %v",

	// 圆桌
	"roundtable.start": "开始圆桌讨论: %s（%d 轮）",
	"roundtable.turn":  "第 %d 轮",
	"roundtable.done":  "圆桌讨论完成",

	// 记忆
	"memory.title":   "%s 的长期记忆",
	"memory.empty":   "暂无记忆。用 /memory add <内容> 添加",
	"memory.added":   "记忆已添加",
	"memory.cleared": "记忆已清空",

	// MCP
	"mcp.connected":    "MCP 服务器 %q 连接成功，注册 %d 个工具",
	"mcp.connect_fail": "MCP 服务器 %q 连接失败: %v",
	"mcp.tools_fail":   "MCP 服务器 %q 获取工具列表失败: %v",
}
