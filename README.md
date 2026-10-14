# OCS Copilot

一个用于 **OCS 网课助手** 的本地大模型答题服务。

把题目交给 OpenAI 兼容的大模型（本地 Ollama、DeepSeek、通义、豆包等）作答，
再按 OCS 要求的格式返回答案，实现自动答题。

## 功能是什么

| 能力 | 说明 |
|---|---|
| **OCS 题库协议** | 对外只提供一个 `POST /search`，接收题目、返回答案，粘贴一份配置即可接入 OCS |
| **多题型适配** | 单选、多选、判断、填空分别处理；多选题答案用 `#` 分隔 |
| **答案缓存** | 答过的题会记住，同一道题再问直接返回，省额度也更快。可关闭、可临时跳过、可让旧答案按概率重新作答 |
| **长回答不会被打断** | 推理模型思考得久也不怕：只要模型还在往外输出，就不会被判成超时失败 |
| **答案干净** | 只返回最终答案，模型写在答案里的思考过程会被去掉；模型没给出答案时报错，而不是塞一个错误答案 |
| **失败自动重试** | 调用失败或拿不到可用答案时自动重试 |
| **图片作答** | 可选开关。开启后可接收题干截图，交给多模态模型作答 |
| **请求头鉴权** | 可选开关。设置令牌后，未携带正确令牌的请求一律拒绝，防止私人部署被刷 |
| **多后端支持** | 任何 OpenAI 兼容接口都能接，不绑定厂商 |
| **自动读配置** | 启动时自动加载 `.env`，不用手动 `source`；环境变量优先级更高 |
| **友好日志** | 彩色分级日志，打印题目、答案与解析；输出到文件或容器日志时自动转为纯文本 |

## 部署前准备

1. **一个模型接口**：本地跑 Ollama，或在云端平台申请 API Key。
   若是云端服务，记得在平台侧授权要用的模型。
2. **一份配置**：

   ```bash
   cp .env.example .env
   ```

   至少填好 `BASE_URL`、`MODEL`，云端服务还要填 `API_KEY`。

   常见配置示例：

   ```bash
   # 本机 Ollama
   BASE_URL=http://localhost:11434/v1
   MODEL=qwen2.5:7b

   # 云端 OpenAI 兼容服务
   BASE_URL=https://api.deepseek.com/v1
   MODEL=deepseek-chat
   API_KEY=你的密钥
   ```

## 如何部署

### 方式一：Docker Compose（推荐）

```bash
cp .env.example .env      # 填好配置
docker compose up -d
```

服务默认监听 `8080`，日志用 `docker compose logs -f` 查看。
答案缓存会落在具名卷 `ocs-data` 里，容器重建后依然保留。

### 方式二：Docker 命令

```bash
# 用仓库里的 Dockerfile 构建
docker build -t ocs-copilot .

# 或直接拉取已发布的镜像
docker pull ghcr.io/mogumc/ocsc:latest
docker tag ghcr.io/mogumc/ocsc:latest ocs-copilot

docker run -d \
  --name ocs-copilot \
  --restart unless-stopped \
  -p 8080:8080 \
  --env-file .env \
  ocs-copilot
```

> **容器要访问宿主机上的 Ollama**：容器里的 `localhost` 指向容器自己，
> 需要把 `BASE_URL` 改成 `http://host.docker.internal:11434/v1`，
> 并在 `docker run` 时加上 `--add-host host.docker.internal:host-gateway`。
> `docker-compose.yml` 已经配好这项映射，Linux 同样适用。

### 方式三：源码 / 二进制

需要 Go `1.23+`：

```bash
make run        # 构建到 bin/ 并启动
# 或者
go run .
```

也可以编译出单文件二进制后丢到任何机器上直接运行（无第三方依赖）：

```bash
make build      # 产物在 bin/ocs-ai-proxy
```

### 方式四：用 Makefile 的 Docker 目标

```bash
make docker-build    # 构建镜像
make docker-run      # 跑容器（自动读取 .env）
make docker-push     # 推送到 GHCR
```

### 镜像与发布

镜像基于 `distroless static`，以非 root 用户运行，体积小、攻击面低。
仓库内置了 GitHub Actions：推送 `main`、打 `v*` tag 或发起 PR 时自动构建镜像，
非 PR 事件会发布到 `ghcr.io/<owner>/<repo>`。打 tag 会额外生成语义化版本标签。

## 配置项

通过环境变量或 `.env` 配置。**真实环境变量优先级高于 `.env`**，
所以在 Docker / 编排里用 `-e` 传参可以压过配置文件。

| 变量名 | 默认值 | 说明 |
|---|---|---|
| `LISTEN_ADDR` | `:8080` | HTTP 监听地址 |
| `BASE_URL` | `http://localhost:11434/v1` | 上游模型地址，需带 `/v1` |
| `MODEL` | `qwen2.5:7b` | 模型名称 |
| `API_KEY` | 空 | 上游模型密钥，本地 Ollama 可留空 |
| `TIMEOUT_SEC` | `30` | 多久没有响应就判超时（秒）。模型持续输出时不计入 |
| `MAX_RETRIES` | `2` | 拿不到可用答案时的重试次数 |
| `STREAM` | `true` | 让长回答不被超时打断。上游模型或网关不支持流式时设为 `false` |
| `ENABLE_VISION` | `false` | 是否接受下游传入的图片作答 |
| `CACHE_ENABLED` | `true` | 是否记住答过的题，重复提问直接返回 |
| `CACHE_DB` | `answer_cache.db` | 缓存文件位置 |
| `CACHE_RETRY_PROBABILITY` | `0.1` | 命中缓存时，有多大比例重新问一次模型来刷新答案（0~1） |
| `ACCESS_TOKEN` | 空 | 下游访问令牌，留空表示不鉴权 |
| `AUTH_HEADER` | `Authorization` | 携带访问令牌的请求头名 |
| `ENV_FILE` | `.env` | 配置文件路径 |
| `NO_COLOR` | 空 | 设置后关闭彩色日志 |

也兼容历史变量名：`LLM_ENDPOINT` → `BASE_URL`，`LLM_MODEL` → `MODEL`，
`OPENAI_BASE_URL` / `OPENAI_MODEL` / `OPENAI_API_KEY` 同样可用，
`AUTH_TOKEN` / `SERVER_TOKEN` 可作为 `ACCESS_TOKEN` 的别名，
`ENABLE_STREAM` 可作为 `STREAM` 的别名。

## 部署后检查

确认服务活着：

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

发一道题试试：

```bash
curl -X POST http://localhost:8080/search \
  -H 'Content-Type: application/json' \
  -d '{"title":"1+1 等于几","options":"A. 1\nB. 2\nC. 3","type":"single"}'
```

正常返回：

```json
{
  "code": 1,
  "question": "1+1 等于几",
  "answer": "B",
  "analysis": "1+1=2，选 B"
}
```

如果开启了鉴权，记得带上请求头：

```bash
curl -X POST http://localhost:8080/search \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer 你的 ACCESS_TOKEN' \
  -d '{"title":"1+1 等于几","options":"A. 1\nB. 2\nC. 3","type":"single"}'
```

## 接口

### `GET /` 与 `GET /health`

服务状态与健康检查。容器健康检查用的就是 `/health`。
这两个接口**不受鉴权保护**，方便探活。

### `POST /search`

OCS 题库协议接口，也是唯一会消耗模型额度的接口。

请求字段：

| 字段 | 必填 | 说明 |
|---|---|---|
| `title` | 是 | 题目文本 |
| `options` | 否 | 选项文本，每行一个 |
| `type` | 否 | `single` / `multiple` / `judgement` / `completion` |
| `images` | 否 | 题干图片，仅在 `ENABLE_VISION=true` 时生效 |
| `skip_cache` | 否 | 设为 `true` 时跳过缓存，强制重新问一次模型 |

响应：

- `code=1` 表示作答成功，取 `question` 与 `answer`
- `code=0` 表示本次失败，读 `msg` 即可知道原因，OCS 会继续尝试后续题库

```json
{ "code": 0, "msg": "模型调用失败: 上游返回状态码 401: ..." }
```

> **失败时不会返回「未知」之类的占位答案**，避免 OCS 把错误答案当成正确答案填进去。

## 图片作答

设置 `ENABLE_VISION=true` 后，`/search` 请求可以带上 `images` 字段，
图片会一并交给支持视觉的模型作答。每个元素支持三种写法：

| 写法 | 示例 |
|---|---|
| data URI | `data:image/jpeg;base64,/9j/4AAQ...` |
| 图片直链 | `https://example.com/question.png` |
| 裸 base64 | `iVBORw0KGgo...`（未声明类型，按 PNG 处理） |

需要注意：

- **上游模型必须支持视觉（多模态）**，用纯文本模型开这个开关没有意义
- 单次请求最多 8 张图片，多余的会被丢弃并打日志
- 开关关闭时，请求里的 `images` 会被忽略并告警，不影响原来的纯文本答题
- OCS 题库配置的 `data` 里没有图片占位符，所以图片需要由自建脚本 / 客户端调用

## 请求头鉴权

**默认不鉴权，适合本机自用。一旦部署到局域网或公网，请务必设置 `ACCESS_TOKEN`**，
否则任何能访问到这个端口的人都会消耗你的模型额度。

```bash
ACCESS_TOKEN=换成一串足够长的随机字符串
AUTH_HEADER=Authorization          # 默认值，可按需改成 X-Auth-Token
```

服务端同时接受两种写法：

```text
Authorization: Bearer <ACCESS_TOKEN>
Authorization: <ACCESS_TOKEN>
```

校验失败返回 HTTP 401，响应体仍是 OCS 协议格式：

```json
{ "code": 0, "msg": "未授权：请求头 Authorization 缺少有效的访问令牌" }
```

要点：

- **只有 `/search` 受保护**，`/` 与 `/health` 保持开放以便探活
- 令牌建议用 `openssl rand -hex 32` 生成
- 日志只记录请求来源与路径，**不会打印令牌内容**
- 面向公网时建议再叠加一层反向代理与限流

## 答案缓存

默认开启。同一道题（题干、选项、题型都一致）问过之后，再问会直接返回上次的答案，
不消耗模型额度，响应从「几秒」变成「几毫秒」。

- 缓存文件默认是当前工作目录下的 `answer_cache.db`，用 `CACHE_DB` 换位置
- **Docker 部署要给这个文件挂一个卷**，否则容器重建后缓存就丢了
- 想彻底关掉：`CACHE_ENABLED=false`
- 想临时绕过缓存、强制重新问一次模型：请求体里带 `"skip_cache": true`
- 担心错误的答案被一直记着：`CACHE_RETRY_PROBABILITY` 默认 `0.1`，
  表示命中缓存时有 10% 的概率重新问一次模型，拿到新答案后覆盖旧的
- 想清空缓存：删掉那个 `.db` 文件即可（同名 `-wal` / `-shm` 一起删）

## 接入 OCS 网课助手

打开 OCS 网课助手 →【全局设置】→【题库配置】→【新建】，
把下面内容整段粘贴进去；仓库里的 `ocs_config.json` 就是这个配置。

```json
[
    {
        "name": "OCS Copilot",
        "homepage": "http://localhost:8080",
        "url": "http://localhost:8080/search",
        "method": "post",
        "type": "GM_xmlhttpRequest",
        "contentType": "json",
        "data": {
            "title": "${title}",
            "options": "${options}",
            "type": "${type}"
        },
        "headers": {
            "Authorization": "Bearer 你的 ACCESS_TOKEN"
        },
        "handler": "return (res) => { if (res.code !== 1) return [res.msg, undefined]; let answer = String(res.answer || '').trim(); if (answer === '正确') answer = '对'; if (answer === '错误') answer = '错'; return [res.question, answer]; }"
    }
]
```

两点提醒：

- **服务端部署在别的机器上时，把 `localhost` 换成服务器 IP / 域名**
- **开启鉴权后，务必把 `headers.Authorization` 里的占位符替换成真实的 `ACCESS_TOKEN`**
- 如果 OCS 请求不到数据，到油猴脚本头部元信息的 `@connect` 里加上对应域名
  （或把 `@connect` 模式设为宽松）

## 常见问题

**模型报 401 / 403**
`API_KEY` 没填或填错；本地 Ollama 不需要 Key，把该项留空即可。

**容器里连不上 Ollama**
容器内的 `localhost` 是容器自己，要改成 `http://host.docker.internal:11434/v1`，
并确认 `docker run` 带了 `--add-host host.docker.internal:host-gateway`。

**回答超时**
模型长时间一个字节都不返回（上游卡住或网络断了），会在 `TIMEOUT_SEC` 后中断并重试。
模型只是思考得久、但一直在往外输出的话不会被判超时。想更宽容就把 `TIMEOUT_SEC` 调大，比如 `120`。

**答案里混进了模型的思考过程**
本地 / 自建推理模型（Qwen3、R1 蒸馏版等）有时会把思考过程写进答案。
本服务只返回最终答案，思考过程会被去掉；如果模型光思考、没给答案，
会返回 `code:0` 让你知道这次没拿到结果，而不是塞一个错误答案进去。

**提示「模型只返回了思考过程」**
模型的输出长度上限太小，思考还没写完就被截断了。
需要在模型或网关那边放宽输出长度上限；本服务不会限制输出长度。

**换了模型，答案却没变**
命中了缓存。请求体里带上 `"skip_cache": true`，或者删掉缓存文件。

**容器重建后缓存没了**
缓存文件在容器里，没有挂卷就会随容器一起消失，按上面的「答案缓存」配一个卷即可。

**一直返回 `code:0`**
看 `msg` 里的具体原因，常见的是端点写错（`BASE_URL` 必须带 `/v1`）、
模型名不存在，或下游没开鉴权却被前面一层代理要求令牌。

**答案被填成「未知」之类**
新版本不会这样了：作答失败一律返回 `code=0` + `msg`，
OCS 会跳过本次并继续尝试其他题库。

## 免责声明

本项目仅供学习交流使用，请勿用于违反学校规定或法律法规的用途。开发者不对使用本项目产生的任何后果负责。

## License

MIT License
