# CodeCrew 项目总规划

> **当前版本**: v0.1.0
> **定位**: 终端原生、多角色协作、权限可控的 AI 编程助手
> **核心理念**: 像指挥一个开发团队一样指挥 AI —— 有分工、有流程、有质检

---

## 🎯 产品愿景

**一句话**：把 AI 从「聊天机器人」变成「可编程的开发团队」。

- **角色化**：开发、审查、架构、测试、文档各司其职，工具白名单即权限边界
- **闭环可验证**：模型调用工具 → 结果回填 → 再推理，直到任务真正完成，而不是一句话答完就断
- **可控**：写文件与执行命令默认需要确认，破坏性命令强制二次确认
- **本地优先**：支持完全离线的 Ollama；会话记录留在本机

---

## 🏗️ 架构总览

```
codecrew (单二进制)
├── cmd/codecrew          CLI 入口：参数解析、配置装载、启动 REPL
├── internal/repl         交互层：命令系统、Agent 循环、权限确认、上下文压缩、成本统计
├── internal/config       分层配置（项目 > 全局 > 环境变量）、供应商解析、密钥脱敏
├── internal/role         角色加载：内嵌默认为底 + 磁盘覆盖，frontmatter 解析
├── internal/llm          OpenAI 兼容客户端：SSE 流式、tool_calls 按槽位归并、token 统计
├── internal/tool         注册表 + read/write/edit/bash/glob/grep/plan + 三层权限闸门
├── internal/session      会话 JSONL 落盘 / 续聊
└── internal/disp         终端显示宽度（中英混排对齐）
```

依赖方向自上而下，禁止反向依赖；`tool` 仅复用 `llm` 的数据结构做摘要。

---

## ✅ 已完成

### v0.0.1 — MVP 骨架

REPL、角色、配置、流式输出、四个工具、欢迎页与中文别名。

### v0.1.0 — 真正可用的闭环（本次）

| 方向         | 内容                                                                                                                   |
| ------------ | ---------------------------------------------------------------------------------------------------------------------- |
| **修断线**   | 对话历史完整回填（user / assistant / role:"tool"），单轮内循环调用直到模型不再请求工具；`/reload`、`/model` 真正重建客户端 |
| **修协议**   | 工具 schema 包装成标准 `{"type":"function", ...}`；流式 `tool_calls` 按 index 归并，并行调用不再串味；兼容省略 index 的端点 |
| **权限**     | allow/ask/deny 三档 + 角色白名单 + 交互确认；破坏性命令强制确认；`--yes`、`/allow`、`permissions` 配置                    |
| **工具**     | 新增 `glob` / `grep` / `plan`；`bash` 跨平台（cmd/PowerShell 与 /bin/sh）；`read` 支持分片与剩余行数提示                 |
| **上下文**   | token 预算、`/context` 可视化、超限自动摘要压缩（模型不可用时机械兜底）、`/compact` `/clear` `/undo`                      |
| **会话**     | JSONL 落盘、`--session` 续聊、`/sessions` `/resume` `/new` `/save`                                                       |
| **角色**     | 补 `architect` / `tester` / `docs`；角色内嵌二进制（`go install` 可用），磁盘同名文件可覆盖                                |
| **安全**     | `SafePath` 改用相对路径判定，堵住同前缀兄弟目录逃逸；文件类工具以工作目录为根；密钥全链路脱敏                              |
| **质量**     | 单元 + 集成测试（假 OpenAI 服务跑完整闭环），`gofmt` / `go vet` / `go test -race` 全绿，三平台 CI + 交叉编译发布流水      |
| **体验**     | 中英混排边框对齐、ANSI 彩色可关闭（`NO_COLOR` / `--no-color`）、错误提示均给出下一步动作                                   |

---

## 🗺️ 后续路线图

### v0.2.0 — 团队化协作

| 任务             | 优先级 | 说明                                                                     |
| ---------------- | ------ | ------------------------------------------------------------------------ |
| **流水线编排**   | P0     | `/pipeline 实现 X` → architect 拆解 → developer 写 → reviewer 审 → tester 测 |
| **圆桌模式**     | P1     | 多角色就同一设计辩论 N 轮再动手，输出决议与分歧点                        |
| **角色长期记忆** | P1     | 每角色维护笔记（架构师记决策、测试员记坑点），可 `/memory` 查看         |
| **diff 预览**    | P1     | write/edit 前展示统一 diff，逐块确认                                     |

### v1.0.0 — 生产级打磨

| 任务             | 说明                                                     |
| ---------------- | -------------------------------------------------------- |
| **成本可视化**   | 按供应商单价估算花费，`/cost --money`                    |
| **插件系统**     | 用户可用 Go 插件或外部进程扩展工具                       |
| **多语言 i18n**  | 中英界面切换，命令别名可扩展                             |
| **基准测试**     | SWE-bench / HumanEval 自动跑分，量化角色与模型组合效果   |
| **MCP 接入**     | 作为 MCP 客户端复用生态工具，替代自研插件机制            |

---

## 🛠️ 技术栈与约束

| 维度     | 选择                          | 理由                                                     |
| -------- | ----------------------------- | -------------------------------------------------------- |
| 语言     | Go 1.22+，仅标准库            | 单二进制、跨平台、无供应链负担                           |
| LLM 协议 | OpenAI Chat Completions       | 生态最广，DeepSeek/Qwen/Kimi/GLM/Ollama 全兼容           |
| 配置     | JSON（容忍 `//` 行注释）      | 标准库解析，模板可读性靠注释补足                         |
| 角色     | Markdown + YAML frontmatter   | 人类可写、diff 友好；内嵌为默认，磁盘可覆盖              |
| 存储     | 文件系统（会话 JSONL）        | 零依赖、可 grep、可迁移                                  |
| 打包     | `go build ./cmd/codecrew`     | 角色随二进制走，`go install` 后无需附带数据目录          |

---

## 📦 仓库结构

```
codecrew/
├── .github/workflows/        CI（gofmt/vet/test/race + 三平台矩阵）与 Release 交叉编译
├── cmd/codecrew/main.go      入口
├── internal/                 见上方架构
│   └── role/defaults/*.md    内置角色（唯一事实来源，go:embed）
├── roles/                    可选：项目自定义/覆盖角色（不入库，本机生效）
├── codecrew.example.json     配置模板
├── go.mod / LICENSE / README.md / PLAN.md / CHANGELOG.md
```

---

## 🚀 用户侧快速开始

见 [README.md](README.md#-快速开始)。三条路径：`codecrew.json` / 环境变量 / `--config`，都支持 `--cwd` 指定项目根。

---

## 📝 变更日志

规范见 [CHANGELOG.md](CHANGELOG.md)。每个里程碑发布前必须同步本文件与 README 的能力表。

---

## 🤝 贡献指南

1. Fork → 分支 → PR，描述里写清「问题—方案—验证方式」
2. 遵循 `internal/*` 分层，禁止跨层反向依赖
3. 新工具实现 `tool.Tool` 接口，在 `NewDefaultRegistry` 注册并声明默认权限档，补测试
4. 新内置角色放 `internal/role/defaults/*.md`；文档型角色放 README
5. `gofmt -l .` 无输出、`go vet ./...`、`go test ./... -race` 全绿后方可提 PR

---

> **维护者提示**：本文件是项目总纲。任何「已完成」声明都必须能被 `go test ./...` 或文档中的命令复现，避免文档再次跑在实现前面。
