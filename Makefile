BINARY  := ocs-ai-proxy
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= ghcr.io/mogumc/ocsc
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build run test vet lint fmt clean docker-build docker-run docker-push

all: build

# 本地构建
build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) .

# 本地运行：.env 由程序自动加载，不需要手动 source
run: build
	./bin/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

lint: vet

fmt:
	gofmt -l -w .

clean:
	rm -rf bin/
	go clean

# ---- Docker ----

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

docker-run:
	docker run --rm -p 8080:8080 --env-file .env $(IMAGE):latest

docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest
