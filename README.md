# OCS Copilot

一个用于 **OCS 网课助手** 的本地大模型代理服务。  
将 `/v1/chat/completions` 请求转发到本地（或兼容 OpenAI 协议）的 LLM 接口，并对输出进行结构化约束与解析。

## 功能特性

- 提供 OpenAI 风格接口：`POST /v1/chat/completions`
- 自动注入系统提示词，约束模型输出为 JSON
- 支持答案结构化解析（`answer/confidence/explanation`）
- 支持解析失败重试（`MAX_RETRIES`）
- 提供健康检查接口：`GET /health`
- 模型不可用时返回降级响应，避免调用方崩溃

---

## 项目结构

```text
ocs-ai-proxy
├─ main.go                 # 服务入口
├─ go.mod                  # Go 模块定义
├─ Makefile                # 构建脚本
├─ .env.example            # 环境变量示例
└─ internal
   ├─ config
   │  └─ config.go         # 配置加载与校验
   ├─ handler
   │  └─ chat_handler.go   # HTTP 请求处理
   ├─ model
   │  └─ chat.go           # OpenAI 兼容数据结构
   └─ proxy
      ├─ llm_proxy.go      # LLM 代理核心逻辑（调用、重试、解析）
      └─ prompt.go         # 系统提示词与降级响应模板
```

---

## 运行环境

- Go `1.21+`
- 可访问的 LLM API（推荐 Ollama/OpenAI 兼容端点）

---

## 配置说明

通过环境变量配置（可参考 `.env.example`）：

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | 服务监听地址 |
| `LLM_ENDPOINT` | `http://localhost:11434/v1` | 本地大模型 API 基础地址 |
| `LLM_MODEL` | `qwen2.5:7b` | 使用的模型名 |
| `TIMEOUT_SEC` | `30` | 请求超时（秒） |
| `MAX_RETRIES` | `2` | 解析失败最大重试次数 |

示例 `.env`：

```text
# OCS AI Proxy 配置项

# HTTP 监听地址（默认 :8080）
LISTEN_ADDR=:8080

# 大模型 API 端点
LLM_ENDPOINT=http://localhost:11434/v1

# 使用的模型名称（需本地已拉取）
LLM_MODEL=qwen2.5:7b

# 请求超时秒数
TIMEOUT_SEC=30

# 解析失败最大重试次数
MAX_RETRIES=2
```

---

## 快速开始

### 1) 配置模型 / 获取模型API

自行搭建ollma并部署大模型；在对应AI模型开放平台申请Key，获取模型API。

### 2) 配置环境变量

复制 `.env.example` 为 `.env`，修改 `LLM_ENDPOINT` 为你的模型API地址。

### 3) 启动服务

#### 方式 A：直接运行

> 访问当前项目Releases页面，下载对应平台的二进制文件。

#### 方式 B：Makefile

> `run-env` / `start` 使用了 `source .env`，更适合类 Unix Shell（Linux/macOS/Git Bash）。  
> Windows PowerShell 下建议手动设置环境变量后再运行 `go run .` 或二进制。

---

## 接口说明

### 健康检查

**GET** `/health`

### Chat Completion 代理

**POST** `/v1/chat/completions`

> 注意：实际转发时会被服务端配置的 `LLM_MODEL` 覆盖。  
> 服务会自动在最前面插入 system prompt，要求模型只输出 JSON。

---

## 与 OCS 网课助手对接

服务启动后，控制台会提示：

- 监听地址（如 `:8080`）
- 本地模型端点
- 使用模型
- OCS 配置建议地址：`http://localhost:8080/v1`

将 OCS 网课助手中的 API Base URL 配置为上面的地址即可。

---

## 注意事项

1. `LLM_ENDPOINT` 应该是 **带 `/v1` 前缀** 的基础路径（例如 `http://localhost:11434/v1`）。
2. 当前仅支持 `POST /v1/chat/completions`，不支持流式返回（`stream` 强制为 `false`）。
3. 若模型输出不符合 JSON 结构，服务会按 `MAX_RETRIES` 重试。
4. 生产环境建议增加鉴权、限流、日志脱敏与监控告警。

---

## License

MIT License
