.PHONY: build run clean test lint

# 默认目标
build:
	go build -o bin/ocs-ai-proxy .

run:
	$(MAKE) build
	./bin/ocs-ai-proxy

# 从 .env 加载配置后运行
run-env:
	set -a && source .env 2>/dev/null || true && $(MAKE) run

clean:
	rm -rf bin/
	go clean

test:
	go test ./...

lint:
	go vet ./...

# 一键启动（含构建 + 加载环境变量）
start:
	set -a && source .env 2>/dev/null || true && \
		go build -o bin/ocs-ai-proxy . && \
		./bin/ocs-ai-proxy
