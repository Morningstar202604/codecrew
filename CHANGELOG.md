# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 与语义化版本。

## v0.1.0 — 2026-08-27

首个真正可日常使用的版本：修通 Agent 闭环，补齐权限、上下文、会话与测试。

### 修复（破坏性缺陷）

- **对话历史从未回填**：用户输入、模型回复、工具结果都没有写入 `history`，每轮请求都是同一份冻结上下文；工具结果也从未以 `role:"tool"` + `tool_call_id` 回传给模型，所谓「Agent 循环」实际不成立。现在单轮内会持续 调用工具 → 回填结果 → 再次推理，直到模型给出最终答复，并受 `max_tool_rounds` 约束。
- **`/reload` 是空操作**：`config.Load` 的返回值被丢弃，全局配置与模型客户端都没重建，README 宣传的「改完配置 /reload 生效」无法工作。
- **`/model` 不生效**：只改了配置字符串，客户端仅在启动时构造一次，切模型对实际请求毫无影响；未配置启动后永远停在「请先配置模型」。
- **工具 schema 格式错误**：直接把 JSON Schema 当工具声明下发，缺少 `{"type":"function","function":{...}}` 包装，多数上游不会按 function calling 处理。
- **流式并行工具调用串味**：`tool_calls` 增量按「ID 非空即新调用」归并，多路并行时后一个调用的 arguments 会拼到前一个上；现按 index 分槽，并兼容省略 `index` 的兼容端点。
- **路径校验可绕过**：`safePath` 用 `strings.HasPrefix` 判定，同级的 `proj-evil` 会被当成 `proj` 之内；且相对路径按进程当前目录解析、根目录取的是二进制所在目录，装到 PATH 后模型读写不到用户项目。现以工作目录为根、用 `filepath.Rel` 判定越界。
- **`go test ./...` 无法通过**：`fmt.Errorf(res.Error)` 触发 vet 的非常量格式串报错（Go 1.24+ 起阻断测试构建）。
- **会话持久化字段丢失**：JSONL 行用内嵌结构体序列化，`llm.Message` 被当成嵌套对象，读回时消息字段全空。
- **`bash` 写死 `cmd.exe`**：非 Windows 平台不可用，且与「跨平台单二进制」的承诺冲突。
- **中英混排边框错位**：按 rune 计宽补空格，CJK 全角字符算 1 列，欢迎框与角色列表右侧不齐。
- **密钥脱敏函数从未被调用**：README/PLAN 声称 `/config` 展示脱敏密钥，实际完全没输出密钥信息。

### 新增

- **权限三档闸门**：角色白名单 → 配置 `permissions`（allow/ask/deny，支持 `*` 兜底）→ 交互确认；`rm -rf`、`git push --force`、`curl | sh`、`iex` 等破坏性命令强制二次确认；`--yes`、`/allow <tool>` 可分级放行；被拒绝的工具调用会以错误形式回传给模型，便于自我纠正。
- **工具**：`glob`（`**/*.go`、`{md,txt}` 多选）、`grep`（正则 + 上下文行 + 文件过滤 + 命中上限）、`plan`（任务拆解与进度跟踪，`/plan` 可查看/手动追加）；`read` 支持 `offset/limit` 分片并提示剩余行数。
- **上下文管理**：token 估算、`/context` 占用条、超过 `max_context_tokens` 自动摘要压缩（模型不可用时机械兜底，不静默丢信息）、`/compact`、`/clear`、`/undo`。
- **会话持久化**：`~/.codecrew/sessions/*.jsonl`，`--session <id>` 启动续聊，`/sessions`、`/resume`、`/new`、`/save`。
- **角色**：新增 `architect`（只读 + 规划）、`tester`（生成并跑测试）、`docs`（文档）；角色改为 `go:embed` 内置，`go install` 后无需附带目录；`roles/*.md` 同名覆盖、新增即新角色，切换角色时替换而非累加 system 提示词。
- **成本统计** `/cost`：模型返回的 usage 优先，缺失时本地估算，含轮数、压缩次数与耗时。
- **CLI 参数**：`--role` `--model` `--cwd` `--config` `--session` `--print` `--yes` `--no-color` `--version`。
- **可观测性**：ANSI 彩色输出（支持 `NO_COLOR`），工具调用与结果摘要单行展示，错误信息一律给出下一步建议。
- **工程化**：`gofmt` / `go vet` / `go test -race` 全绿；三平台 CI 矩阵与交叉编译 Release 流水线；仓库补齐 `.gitattributes`、扩展 `.gitignore`、新增 `CHANGELOG.md`。

### 变更

- 目录结构对齐 PLAN 规范：入口移到 `cmd/codecrew/`，交互层拆到 `internal/repl/`，新增 `internal/session/`、`internal/disp/`；工具实现合并为 `files.go` / `search.go` / `plan.go`。
- 配置模板重写：显式列出 `permissions`、`max_context_tokens`、`max_tool_rounds`、`working_dir`，新增 Ollama 本地模型示例。
- README 角色表与能力表全部与代码核对，删除未实现的 architect/tester 工具（`test`、`glob` 之外的幻影工具）描述；`/roles` 不再提示不存在的中文角色名。
- 单轮工具调用上限默认 12（可配置），替代此前「一次流式响应 + 一次性执行工具」的行为。

### 仓库维护

- 许可证由 MIT 改为 Apache License 2.0（`LICENSE` 全文替换，README 引用同步）。
- 移除误提交的 8.9MB 二进制 `crew.exe~`（此前被 git 跟踪，`.gitignore` 的 `*.exe` 不匹配 `~` 后缀），并在 `.gitignore` 补上 `*.exe~` 与更完整的构建产物规则。
- 新增仓库外围配置：`.github/ISSUE_TEMPLATE/`（缺陷/功能）、`.github/pull_request_template.md`、`.github/dependabot.yml`（GitHub Actions + gomod）、`CONTRIBUTING.md`；GitHub 侧开启 `main` 分支保护（需 1 人审查、要求分支最新、禁止强推）。

## v0.0.1 — 2026-08-27

- 初始 MVP 骨架：REPL、角色 frontmatter、分层配置、流式输出、read/write/edit/bash。
- 已知问题（本版本描述见上方「修复」条目）：Agent 循环未闭合、`/reload` 与 `/model` 不生效、无测试。
