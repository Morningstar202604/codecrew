# 参与贡献

感谢关注 CodeCrew。项目当前处于开发阶段（v0.1.0），欢迎提 Issue 和 PR。

## 起步

要求 Go 1.22+，仅用标准库：

```bash
go build ./...
go test ./...
./codecrew --cwd <你的项目目录>
```

无 API Key 也能跑通核心：`internal/repl` 的测试内置了一个假的 OpenAI 兼容服务，会走完整「模型请求工具 → 执行 → 回填 → 再推理」的闭环。

## 结构约定

- 分层依赖方向：`config → llm → role → tool`，交互层 `repl` 组装它们；禁止反向依赖。
- 新工具：实现 `tool.Tool`，在 `tool.NewDefaultRegistry` 注册并声明默认权限档，补单测。
- 新角色：内置放 `internal/role/defaults/*.md`；用户自定义放项目里的 `roles/*.md`。

## 提交前

`gofmt -l .` 无输出、`go vet ./...` 与 `go test ./... -count=1` 全绿，新逻辑带测试。

## 许可

提交即表示你的贡献按 Apache License 2.0 授权。请勿提交任何密钥（`codecrew.json` 已在 `.gitignore`）。
