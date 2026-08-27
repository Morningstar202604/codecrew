# CodeCrew — 你的 AI 开发团队

> 像指挥一个开发团队一样指挥 AI —— 有分工、有流程、有质检。  
> 终端原生、多角色协作、可扩展的 AI 编程助手。

---

## ✨ 核心特性

| 特性           | 说明                                                                                                       |
| -------------- | ---------------------------------------------------------------------------------------------------------- |
| **角色化分工** | 内置 `developer`（编码）、`reviewer`（审查）、规划中 `architect`/`tester`/`docs`，各司其职、工具白名单隔离 |
| **多模型通吃** | 一套配置兼容 DeepSeek / 通义千问 / Kimi / 智谱 GLM / OpenAI / Ollama 等所有 OpenAI 兼容 API                |
| **工具闭环**   | read / write / edit / bash 四件套，LLM 自动调用、结果回填、再次推理，形成完整 Agent 循环                   |
| **配置即代码** | `codecrew.json` 项目级 + `~/.codecrew/config.json` 全局级，热重载、密钥脱敏、模板开箱即用                  |
| **终端原生**   | 单二进制、零依赖、机械硬盘友好、支持全离线模型（Ollama）                                                   |
| **中文友好**   | 全中文界面、中文命令别名（/角色、/模型、/配置）、中文报错提示                                              |

---

## 🚀 快速开始

### 1. 安装

```bash
# Go 用户
go install github.com/yourname/codecrew@latest

# 或下载单二进制（Release 页面）
```

### 2. 三步配置

```bash
# 复制配置模板
cp codecrew.example.json codecrew.json

# 编辑 codecrew.json，填入你的 API Key（以 DeepSeek 为例）
{
  "model": "deepseek/deepseek-chat",
  "providers": {
    "deepseek": {
      "base_url": "https://api.deepseek.com",
      "api_key": "sk-xxxxxxxxxxxxxxxx",  # ← 填这里
      "models": ["deepseek-chat", "deepseek-reasoner"]
    }
  }
}

# 回到终端刷新
codecrew /reload
```

### 3. 开始用

```bash
codecrew
> 帮我写个 Go HTTP 服务器，带健康检查和优雅关闭
```

---

## 🎮 常用命令

| 命令            | 别名          | 说明                                         |
| --------------- | ------------- | -------------------------------------------- |
| `/help`         | `帮助`        | 显示所有命令                                 |
| `/roles`        | `角色`        | 列出所有角色                                 |
| `/role <name>`  | `角色 <name>` | 切换角色（如 `/role reviewer`）              |
| `/model`        | `模型`        | 查看可用模型列表                             |
| `/model <spec>` | `模型 <spec>` | 切换模型（如 `/model qwen/qwen-coder-plus`） |
| `/config`       | `配置`        | 查看当前配置、供应商状态、密钥脱敏           |
| `/reload`       | `重载`        | 热重载配置文件（改完 codecrew.json 后用）    |
| `/exit`         | `退出`        | 退出程序                                     |

> 所有命令均支持中文别名，如 `/角色 reviewer`、`/模型 deepseek/deepseek-chat`。

---

## 🎭 内置角色

| 角色        | 定位           | 授权工具                  | 适用场景                       |
| ----------- | -------------- | ------------------------- | ------------------------------ |
| `developer` | 全栈开发工程师 | read/write/edit/bash      | 日常编码、重构、新增功能       |
| `reviewer`  | 代码审查员     | read/glob/grep            | 代码审查、安全扫描、规范检查   |
| `architect` | 系统架构师     | read/glob/grep/plan       | 架构设计、技术选型、任务拆解   |
| `tester`    | 测试工程师     | read/write/edit/bash/test | 单测生成、集成测试、回归跑分   |
| `docs`      | 技术文档工程师 | read/write/glob           | API 文档、README、架构文档生成 |

> 角色定义在 `roles/*.md`，遵循 `frontmatter + Markdown` 格式，用户可自行增删改。

---

## ⚙️ 配置详解

### 配置文件优先级

```
项目级 codecrew.json  >  全局 ~/.codecrew/config.json  >  环境变量
```

### 完整示例

```json
{
  "model": "deepseek/deepseek-chat",
  "providers": {
    "deepseek": {
      "base_url": "https://api.deepseek.com",
      "api_key": "sk-xxx",
      "models": ["deepseek-chat", "deepseek-reasoner"]
    },
    "qwen": {
      "base_url": "https://dashscope.aliyuncs.com/compatible-mode/v1",
      "api_key": "sk-xxx",
      "models": ["qwen-coder-plus", "qwen-max"]
    },
    "ollama": {
      "base_url": "http://localhost:11434/v1",
      "api_key": "ollama",
      "models": ["qwen2.5-coder:7b", "codellama:13b"]
    }
  }
}
```

### 环境变量兜底

```bash
export CREW_BASE_URL="https://api.deepseek.com"
export CREW_API_KEY="sk-xxx"
export CREW_MODEL="deepseek-chat"
```

---

## 🛠️ 自定义角色

在 `roles/` 目录新建 `.md` 文件：

```markdown
---
name: my-role
description: 我的专属角色
tools: [read, write, bash]
---

你是一名专注于 XXX 的专家。

- 职责 1
- 职责 2
```

然后 `/reload` 即可在 `/roles` 看到并 `/role my-role` 切换。

---

## 📦 从源码构建

```bash
git clone https://github.com/yourname/codecrew
cd codecrew
go build -o codecrew .
./codecrew
```

要求：Go 1.22+，零额外依赖。

---

## 🗺️ 路线图

| 版本       | 重点                                                              | 状态      |
| ---------- | ----------------------------------------------------------------- | --------- |
| **v0.0.1** | MVP：REPL、角色、配置、LLM流式、工具闭环                          | ✅ 完成   |
| **v0.1.0** | 权限交互、规划工具、上下文管理、会话持久化、architect/tester 角色 | 🚧 进行中 |
| **v0.2.0** | 流水线编排（/pipeline）、圆桌模式、角色记忆、角色市场             | 📋 规划中 |
| **v1.0.0** | 权限三档、Token可视化、插件系统、多语言、基准测试                 | 📋 规划中 |

详见 [PLAN.md](PLAN.md)。

---

## 🤝 贡献指南

欢迎 PR！请遵循：

1. **分层架构**：`internal/config` → `internal/llm` → `internal/role` → `internal/tool`，禁止跨层反向依赖
2. **新工具**：实现 `tool.Tool` 接口，在 `internal/tool/registry.go` 注册
3. **新角色**：放入 `roles/*.md`，遵循 `frontmatter` 规范
4. **测试**：`go test ./...` 通过后再提交 PR

---

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE)。

---

## 🙏 致谢

- OpenAI 兼容 API 标准
- DeepSeek / Qwen / Kimi / GLM / Ollama 等模型提供商
- Go 社区优秀开源库

---

**CodeCrew** — 让 AI 成为你最得力的开发队友。 🤝
