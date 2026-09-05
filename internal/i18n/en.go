package i18n

// enUS 是美式英语翻译字典。
var enUS = Dictionary{
	// Welcome
	"welcome.title":    "CodeCrew — Multi-Role AI Programming Assistant",
	"welcome.subtitle": "Direct AI like a development team",
	"welcome.quick":    "Quick Start",
	"welcome.help":     "Type /help to see all commands",

	// Common
	"common.ok":      "✓",
	"common.fail":    "✗",
	"common.warning": "⚠",
	"common.loading": "Loading...",
	"common.none":    "None",
	"common.unknown": "Unknown",
	"common.confirm": "Confirm",
	"common.cancel":  "Cancel",
	"common.yes":     "Yes",
	"common.no":      "No",

	// Roles
	"role.current":   "Current role",
	"role.available": "Available roles",
	"role.switched":  "Switched to role %s",
	"role.not_found": "Role %s not found",
	"role.tools":     "Current role tools",

	// Models
	"model.current":   "Current model",
	"model.switched":  "Switched to model %s",
	"model.invalid":   "Model format should be provider/model-name",
	"model.not_found": "Unknown provider %s",

	// Config
	"config.title":    "Current Configuration",
	"config.reloaded": "Configuration reloaded",
	"config.source":   "Config source",

	// Tools
	"tools.title":   "Available Tools",
	"tools.allow":   "Allowed",
	"tools.ask":     "Ask first",
	"tools.deny":    "Denied",
	"tools.allowed": "Tool %s allowed",

	// Permission
	"perm.ask":    "Allow execution?",
	"perm.always": "Always allow",
	"perm.once":   "This time only",
	"perm.deny":   "Deny",

	// Context
	"context.title":     "Context Usage",
	"context.used":      "Used",
	"context.limit":     "Limit",
	"context.compact":   "Compact context",
	"context.compacted": "Context compacted",
	"context.cleared":   "Context cleared",

	// Cost
	"cost.title":     "Session Cost",
	"cost.turns":     "model turns",
	"cost.tokens":    "tokens",
	"cost.input":     "input",
	"cost.output":    "output",
	"cost.total":     "total",
	"cost.elapsed":   "elapsed",
	"cost.provider":  "provider",
	"cost.estimated": "estimated cost",
	"cost.no_price":  "No pricing configured, cannot estimate cost",
	"cost.note":      "Provider billing may differ, this is for reference only",

	// Sessions
	"session.saved":   "Session saved",
	"session.new":     "New session created",
	"session.resumed": "Resumed session %s",
	"session.list":    "Session History",
	"session.none":    "No sessions yet",

	// Errors
	"error.generic":    "Operation failed: %v",
	"error.no_config":  "No config file found, run codecrew --init to create",
	"error.model_call": "Model call failed: %v",

	// Exit
	"exit.goodbye": "Goodbye! Session saved, use /resume to continue.",

	// Pipeline
	"pipeline.start": "Starting pipeline: %s",
	"pipeline.stage": "Stage %d/%d: %s",
	"pipeline.done":  "Pipeline complete",
	"pipeline.fail":  "Pipeline failed: %v",

	// Roundtable
	"roundtable.start": "Starting roundtable: %s (%d rounds)",
	"roundtable.turn":  "Round %d",
	"roundtable.done":  "Roundtable complete",

	// Memory
	"memory.title":   "%s's Long-term Memory",
	"memory.empty":   "No memory yet. Use /memory add <content> to add",
	"memory.added":   "Memory added",
	"memory.cleared": "Memory cleared",

	// MCP
	"mcp.connected":    "MCP server %q connected, %d tools registered",
	"mcp.connect_fail": "MCP server %q connection failed: %v",
	"mcp.tools_fail":   "MCP server %q failed to list tools: %v",
}
