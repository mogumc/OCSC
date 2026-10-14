# syntax=docker/dockerfile:1

# ---------- 构建阶段 ----------
FROM golang:1.23-alpine AS builder

WORKDIR /src

# 纯标准库项目，无第三方依赖；单独拷贝 go.mod 以便复用构建缓存
COPY go.mod ./
RUN go mod download

COPY . .

ARG VERSION=dev
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/ocs-ai-proxy .

# 预建缓存目录：具名卷首次挂载时会继承这里的属主，非 root 进程才写得进去
RUN mkdir -p /out/data && touch /out/data/.keep

# ---------- 运行阶段 ----------
# distroless static：无 shell、无包管理器，自带 CA 证书与 tzdata，
# 且 CGO 关掉后是静态链接，直接跑得起来。
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/ocs-ai-proxy /usr/local/bin/ocs-ai-proxy
COPY --from=builder --chown=65532:65532 /out/data /data

ENV LISTEN_ADDR=:8080
# 缓存落在挂载卷里，容器重建后依然保留
ENV CACHE_DB=/data/answer_cache.db
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/ocs-ai-proxy"]
