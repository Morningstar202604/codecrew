# 参与贡献

## Working rules for this repository

* Dependency updates: search the whole repository for every occurrence of a dependency (build files, lockfiles, CI workflows, docs) before bumping. A partial bump — declaration updated but the lockfile or a pinned action left behind — is the most common cause of "works locally, CI fails". Keep lockfiles in the same commit as the declaration. Move version-coupled toolchain upgrades (e.g. Gradle/AGP/Kotlin/Hilt or the Python/uv pair) together in one commit.
* Refactoring: pull latest main first, work on a fresh branch, keep commits atomic with messages that state the why, and always run the full check suite before pushing (for this repo: `go test ./...` and `golangci-lint run ./...`). A branch left behind main cannot be merged under the repository's branch protection.
* Merge conflicts: resolve conflicts in the working tree against the latest main; never force-push shared branches; never resolve a conflict by blindly taking either side — re-read both sides and keep both changes when they are both valid.
* Versioning: releases follow X.Y.Z starting at 0.0.0. Last digit = fixes, middle digit = feature work, first digit stays 0 until a stable release is declared. Bump the version in code, CHANGELOG.md and the tag in the same change.

感谢关注 CodeCrew。项目当前版本 v1.0.6，欢迎提 Issue 和 PR。

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

提交即表示你的贡献按 MIT License 授权。请勿提交任何密钥（`codecrew.json` 已在 `.gitignore`）。
