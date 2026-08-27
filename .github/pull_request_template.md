## 这个 PR 做了什么
一两句话说明动机与范围。

## 类型
- [ ] 功能 / 新工具
- [ ] 缺陷修复
- [ ] 重构
- [ ] 文档

## 验证方式
贴出实际跑过的命令与结果。至少：

```
gofmt -l .          # 应无输出
go vet ./...
go test ./... -count=1
```

## 影响与风险
涉及的模块（config/llm/role/tool/repl/session）、行为变化、是否需要更新 README/PLAN。

## 备注
- 项目处于开发中（v0.1.0），优先保证能跑通与不破坏现有测试；
- 不要提交本地密钥（`codecrew.json` 已在 `.gitignore`）。
