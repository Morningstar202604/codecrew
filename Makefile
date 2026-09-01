# CodeCrew Makefile

.PHONY: build test vet fmt lint coverage clean run serve

# 构建
build:
	go build -o codecrew ./cmd/codecrew

# 测试
test:
	go test -race ./...

#  vet 检查
vet:
	go vet ./...

# 格式化
fmt:
	gofmt -w .

# lint 检查（需要安装 golangci-lint）
lint:
	golangci-lint run ./...

# 测试覆盖率
coverage:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

# 查看覆盖率摘要
coverage-summary:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# 清理
clean:
	rm -f codecrew coverage.out coverage.html

# 运行
run: build
	./codecrew

# 启动 Web 服务
serve: build
	./codecrew --serve --port 8080

# 全部检查
check: fmt vet test lint
	@echo "✅ 全部检查通过"
