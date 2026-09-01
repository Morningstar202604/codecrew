# CodeCrew 功能大全

CodeCrew 是一个终端里的 AI 开发团队，支持多角色协作、推理范式、验证自愈、规划执行、知识记忆、编排评估等完整的 Agent 技术栈。

---

## 📋 目录

- [快速开始](#快速开始)
- [核心功能](#核心功能)
- [推理范式](#推理范式)
- [验证与自愈](#验证与自愈)
- [规划与执行](#规划与执行)
- [记忆与知识](#记忆与知识)
- [编排与协作](#编排与协作)
- [评估框架](#评估框架)
- [团队协作](#团队协作)
- [Web 工作台](#web-工作台)
- [配置参考](#配置参考)

---

## 快速开始

```bash
# 安装
go install github.com/Morningstar202604/codecrew/cmd/codecrew@latest

# 配置
cp codecrew.example.json codecrew.json
# 编辑 codecrew.json，填入 base_url / api_key

# 启动终端交互
codecrew

# 启动 Web 服务
codecrew --serve --port 8080
# 浏览器访问 http://localhost:8080

# 非交互单轮
codecrew --print "帮我写一个快速排序"
```

---

## 核心功能

### 多角色系统

CodeCrew 内置 5 个角色，每个角色有独立的 system prompt 和记忆：

| 角色 | 职责 | 命令 |
|------|------|------|
| 👨‍💻 developer | 代码实现、功能开发 | `/role developer` |
| 👁️ reviewer | 代码审查、质量把控 | `/role reviewer` |
| 🏗️ architect | 系统设计、架构决策 | `/role architect` |
| 🧪 tester | 测试编写、质量验证 | `/role tester` |
| 📝 docs | 文档编写、注释完善 | `/role docs` |

### 工具系统

CodeCrew 提供丰富的工具供 AI 调用：

| 工具 | 功能 | 权限 |
|------|------|------|
| `read` | 读取文件内容 | 默认允许 |
| `write` | 写入文件（带 diff 预览） | 默认询问 |
| `edit` | 编辑文件（带 diff 预览） | 默认询问 |
| `bash` | 执行 shell 命令 | 默认询问 |
| `grep` | 搜索文件内容 | 默认允许 |
| `glob` | 匹配文件路径 | 默认允许 |
| `plan` | 任务计划管理 | 默认允许 |
| `search_code` | 代码库语义搜索 | 默认允许 |

### 权限系统

三档权限控制：
- `allow` — 自动允许，无需确认
- `ask` — 每次询问确认（默认）
- `deny` — 禁止使用

```json
{
  "permissions": {
    "read": "allow",
    "write": "ask",
    "bash": "deny",
    "*": "ask"
  }
}
```

### 会话管理

- 自动保存会话历史
- `/sessions` 查看历史会话
- `/resume <id>` 续聊指定会话
- `/new` 新建会话
- `/clear` 清空当前历史
- `/undo` 回退上一轮

### 上下文管理

- 自动 token 计数
- `/context` 查看上下文占用
- `/compact` 手动压缩历史
- 超过阈值自动压缩（保留摘要）

---

## 推理范式

CodeCrew 支持 4 种推理模式，可通过 `/reasoning <mode>` 切换：

### standard（标准模式）
默认模式，直接对话，无特殊推理格式。

### react（ReAct 模式）
显式化推理过程：Thought → Action → Observation 循环。
- 思考过程以灰色显示
- 适合需要逐步推理的复杂任务

```
/reasoning react
```

### reflexion（反思模式）
任务完成后自动反思，总结经验教训：
- 成功时：总结有效方法
- 失败时：深度分析根本原因
- 失败经验自动积累，后续任务自动注入

```
/reasoning reflexion
/failures          # 查看失败经验
/failures clear    # 清空失败经验
```

### cot（Chain-of-Thought 模式）
链式思考，强制按 4 步推理：
1. 理解问题
2. 分析方案
3. 制定计划
4. 执行验证

```
/reasoning cot
```

### 配置项

```json
{
  "reasoning": {
    "mode": "standard",
    "show_thoughts": true,
    "auto_reflect": true,
    "reflection_depth": 1,
    "inject_reflections": true
  }
}
```

---

## 验证与自愈

CodeCrew 内置验证引擎，代码修改后自动验证，失败时自动修复。

### 基本用法

```
/verify              # 运行验证
/verify repair       # 运行验证并自动修复
/verify config       # 查看验证配置
```

### 自动验证

代码修改后自动触发验证（可配置）：
- 验证失败 → 自动分析错误 → 模型修复 → 重新验证
- 最多修复 N 轮（默认 3 轮）
- 支持自动检测项目类型（Go/Node.js/Python/Rust/Makefile）

### 配置项

```json
{
  "verify": {
    "enabled": true,
    "auto_verify": true,
    "commands": ["go build ./...", "go test ./...", "go vet ./..."],
    "max_repair_rounds": 3,
    "timeout_seconds": 120
  }
}
```

---

## 规划与执行

复杂任务先分解为子任务，按依赖顺序逐步执行。

### 基本用法

```
/plan on                  # 开启计划模式
/plan 实现用户登录功能     # 分解任务并执行
/plan off                 # 关闭计划模式
/plan mode <auto|manual>  # 切换模式
/plan clear               # 清空当前计划
```

### 特性

- **DAG 任务分解**：任务间有依赖关系，按拓扑顺序执行
- **动态调整**：任务失败时自动调整计划（最多 2 轮）
- **进度跟踪**：实时显示任务进度
- **手动分配**：可手动添加任务和依赖

### 配置项

```json
{
  "planner": {
    "enabled": false,
    "auto_plan": false,
    "max_tasks": 8,
    "auto_adjust": true,
    "max_adjust_rounds": 2
  }
}
```

---

## 记忆与知识

CodeCrew 提供三层记忆系统：

### 1. 角色记忆

每个角色独立维护 Markdown 笔记，自动注入 system prompt：

```
/memory              # 查看当前角色记忆
/memory add <内容>   # 添加记忆
/memory clear        # 清空记忆
```

### 2. 情景记忆

记录任务执行历史，自动注入后续任务：
- 自动记录成功/失败的任务
- 按相关性检索历史经验
- 可配置注入数量

### 3. 代码库索引

扫描项目文件，构建倒排索引，支持语义搜索：

```
/index status        # 查看索引状态
/index build         # 重建索引
/index search <q>    # 搜索代码
```

**特性**：
- 多语言符号提取（Go/Python/JavaScript 等）
- BM25 语义检索
- 并发索引构建（8 worker）
- 自动检测文件变更
- 持久化到磁盘

### 配置项

```json
{
  "knowledge": {
    "enabled": true,
    "auto_index": true,
    "index_interval": 24,
    "max_results": 10,
    "context_lines": 3,
    "inject_episodic": true,
    "episodic_count": 5
  }
}
```

---

## 编排与协作

### Supervisor 模式

监督者协调多个工作者角色：

```
/supervisor on              # 开启 Supervisor 模式
/supervisor assign <角色> <任务>  # 分配任务
/supervisor off             # 关闭
/supervisor status          # 查看状态
```

**特性**：
- 任务分配给指定角色
- 进度跟踪
- 任务完成后自动汇总

### Human-in-the-Loop

关键操作人工审批：

```
/approve <id>    # 批准待审批操作
/deny <id>       # 拒绝待审批操作
```

**特性**：
- 待审批队列
- 自动批准配置（按操作类型）
- 审批历史记录

---

## 评估框架

CodeCrew 内置能力评估框架，可测试模型在各类任务上的表现。

### 基本用法

```
/eval run          # 运行默认测试用例
/eval list         # 查看历史评估报告
```

### 测试用例分类

- **code_generation** — 代码生成
- **debugging** — 调试修复
- **planning** — 任务规划
- **code_review** — 代码审查
- **conversation** — 技术对话
- **refactoring** — 代码重构

### 评估指标

- 通过率（Pass Rate）
- 关键词命中率
- 输出长度得分
- 平均得分
- 按分类/难度统计

---

## 团队协作

### 流水线编排

一键启动多阶段流水线：

```
/pipeline <任务>
```

**流程**：architect 拆解 → developer 实现 → reviewer 审查 → tester 测试

### 圆桌讨论

多角色辩论后输出共识：

```
/roundtable <话题> [轮数]
```

**角色**：architect / developer / reviewer 三方辩论

---

## Web 工作台

CodeCrew 提供完整的 Web 界面，支持桌面端和移动端。

### 启动

```bash
codecrew --serve --port 8080
# 访问 http://localhost:8080
```

### 功能面板

Web 端提供 10 个功能面板：

| 面板 | 功能 |
|------|------|
| 💬 对话 | 主对话界面，支持流式输出 |
| ⚙️ 配置 | 查看和修改配置 |
| 📈 统计 | token 使用、耗时、成本统计 |
| 🧠 推理 | 推理模式切换、失败经验管理 |
| ✅ 验证 | 运行验证、查看结果、自动修复 |
| 📚 索引 | 代码库索引管理、代码搜索 |
| 👔 编排 | Supervisor 任务分配、人工审批 |
| 📊 评估 | 运行评估、查看历史报告 |
| 📝 计划 | 任务计划管理 |
| 🧰 工具 | 工具权限管理 |

### 移动端适配

- 侧边栏抽屉式
- 功能面板抽屉式
- 安全区域适配（刘海屏）
- 触摸优化

---

## 配置参考

完整配置示例见 `codecrew.example.json`。

### 核心配置

```json
{
  "model": "deepseek/deepseek-chat",
  "providers": {
    "deepseek": {
      "base_url": "https://api.deepseek.com",
      "api_key": "sk-xxx",
      "models": ["deepseek-chat", "deepseek-coder"]
    }
  },
  "working_dir": ".",
  "max_context_tokens": 24000,
  "max_tool_rounds": 12,
  "permissions": {
    "read": "allow",
    "*": "ask"
  }
}
```

### 环境变量

- `CREW_BASE_URL` — API 基础 URL
- `CREW_API_KEY` — API 密钥
- `CREW_MODEL` — 默认模型
- `CREW_WORKING_DIR` — 工作目录
- `CREW_MAX_CONTEXT_TOKENS` — 最大上下文 token
- `CREW_DEFAULT_PERMISSION` — 默认权限

---

## 命令速查

### 基础命令
| 命令 | 功能 |
|------|------|
| `/help` | 查看帮助 |
| `/roles` | 查看角色列表 |
| `/role <name>` | 切换角色 |
| `/model` | 查看当前模型 |
| `/model <spec>` | 切换模型 |
| `/config` | 查看配置 |
| `/reload` | 重载配置 |
| `/exit` | 退出 |

### 会话命令
| 命令 | 功能 |
|------|------|
| `/sessions` | 历史会话列表 |
| `/resume <id>` | 续聊会话 |
| `/new` | 新建会话 |
| `/clear` | 清空历史 |
| `/undo` | 回退上一轮 |
| `/context` | 查看上下文 |
| `/compact` | 压缩历史 |
| `/cost` | 成本统计 |

### 高级命令
| 命令 | 功能 |
|------|------|
| `/reasoning <mode>` | 切换推理模式 |
| `/failures` | 查看失败经验 |
| `/verify [repair]` | 运行验证 |
| `/plan [on|off|<目标>]` | 计划模式 |
| `/index [status|build|search]` | 代码库索引 |
| `/supervisor [on|off|assign]` | Supervisor 模式 |
| `/approve <id>` | 批准操作 |
| `/deny <id>` | 拒绝操作 |
| `/eval [run|list]` | 评估框架 |
| `/pipeline <任务>` | 流水线编排 |
| `/roundtable <话题>` | 圆桌讨论 |
| `/memory [add|clear]` | 角色记忆 |
| `/history` | 命令历史 |
| `!n` / `!!` | 重复历史命令 |

---

## 技术栈

- **语言**：Go 1.21+（零第三方依赖）
- **前端**：原生 HTML/CSS/JavaScript（无框架）
- **协议**：OpenAI 兼容 API
- **存储**：本地文件（JSON）
- **架构**：15 个 internal 包，分层清晰

---

## License

MIT License
