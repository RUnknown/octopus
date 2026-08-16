# Octopus

Octopus 是一个面向个人和小团队的自托管 LLM API 网关与管理控制台。它把多个模型供应商和中转站统一到一组兼容 API 后面，并提供渠道管理、协议转换、负载均衡、故障转移、缓存统计和请求诊断。

> 社区推荐：[Linux.do](https://linux.do/)

本仓库从 [`tianxia3111/octopus`](https://github.com/tianxia3111/octopus) 派生，项目源自 [`bestruirui/octopus`](https://github.com/bestruirui/octopus)。当前分支在上游能力之上重点增强了站点同步、OpenAI Responses、WebSocket Relay、可观测性和路由诊断。

English: [README.md](README.md)

## 核心能力

### 统一模型 API

| 入口 | 用途 |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions |
| `POST /v1/responses` | OpenAI Responses，支持流式与非流式 |
| `GET /v1/responses` | OpenAI Responses WebSocket |
| `POST /v1/responses/compact` | Responses 上下文压缩转发 |
| `POST /v1/messages` | Anthropic Messages |
| `POST /v1/embeddings` | OpenAI Embeddings |
| `POST /v1/images/*` | 图片生成、编辑和变体 |

出站支持 OpenAI Chat、OpenAI Responses、Anthropic、Gemini、Volcengine 和 OpenAI Embeddings。跨协议请求通过统一内部模型转换；同协议请求优先保留原始字段和流式语义。

### 路由与可靠性

- Group 作为客户端使用的模型名，将请求映射到一个或多个渠道模型。
- 支持轮询、随机、故障转移和加权分配。
- 支持同渠道重试、跨渠道降级、首 Token 超时和指数退避。
- 按 `channel:key:model` 维度进行熔断，并支持 Half-Open 探测。
- 支持 API Key + 模型维度的会话粘性。
- OpenAI Responses 支持 HTTP replay、上游 WebSocket 复用和会话恢复。

### 站点与渠道管理

- 管理中转站、账号、用户组、Token 和模型。
- 自动同步账号数据、模型和余额，并支持定时签到。
- 将站点账号投影为托管渠道，减少重复配置。
- 支持手动渠道、代理配置、自定义 Header、参数覆盖和模型自动分组。

### 可观测性

- 首页展示请求、输入/输出 Token、缓存读取、缓存命中率、费用和耗时。
- Relay 日志记录渠道尝试、HTTP 状态、重试性、熔断冷却和协议路径。
- 请求进入后立即显示为“响应中”，完成后原位更新。
- 分组页提供路由候选、Key 可用性、熔断状态和协议兼容性诊断。
- 日志页可查看当前活动请求及其路由阶段。
- 管理界面支持简体中文、繁体中文和英文。

## 请求链路

```text
客户端请求
  → API Key 鉴权
  → 入站协议解析
  → Group 匹配
  → 负载均衡 / 会话粘性
  → Channel 与 Key 选择
  → 熔断 / 重试 / 故障转移
  → 出站协议转换或同协议透传
  → 上游模型 API
  → 响应转换、统计与日志
```

前端使用 Next.js 静态导出，构建结果嵌入 Go 二进制；部署时只需要运行一个服务进程。

## 快速开始

### 使用现有 Docker 镜像

镜像由本仓库的 GitHub Actions 发布：

```bash
docker run -d \
  --name octopus \
  -p 8080:8080 \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/mingtian886/octopus:latest
```

也可以修改 [`docker-compose.yml`](docker-compose.yml) 中的数据目录后运行：

```bash
docker compose up -d
```

每个版本都会同时发布 `latest`、版本标签和 Alpine 变体；需要固定版本时使用 `ghcr.io/mingtian886/octopus:vX.Y.Z`。

### 从源码运行

要求：Go 1.25、Node.js 22+、pnpm。

```bash
git clone https://github.com/mingtian886/octopus.git
cd octopus

cd web
pnpm install --frozen-lockfile
pnpm build
cd ..

rm -rf static/out
mv web/out static/out
go run . start
```

访问 <http://localhost:8080>。首次启动会自动创建 `data/config.json` 和 SQLite 数据库。

默认用户名和密码均为 `admin`。首次登录后请立即修改密码。

## 配置

默认配置：

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "database": {
    "type": "sqlite",
    "path": "data/data.db"
  }
}
```

- 配置文件默认位于 `data/config.json`。
- 可通过 `octopus start --config /path/to/config.json` 指定配置。
- 环境变量使用 `OCTOPUS_` 前缀，例如 `OCTOPUS_SERVER_PORT=9090`。
- 数据库支持 SQLite、MySQL 和 PostgreSQL；非 SQLite 时 `database.path` 填写对应 DSN。

## 开发与验证

```bash
# 后端
go test ./...
go vet ./...

# 前端
cd web
pnpm install --frozen-lockfile
pnpm exec tsc --noEmit
pnpm lint
pnpm build
```

常用目录：

- `internal/server`：HTTP 服务、路由、中间件和 Handler
- `internal/op`：业务操作、数据库访问和内存缓存
- `internal/relay`：模型转发、重试、熔断、流式与 WebSocket
- `internal/transformer`：入站/出站协议转换
- `internal/sitesync`：站点同步、签到和托管渠道投影
- `web/src/components/modules`：管理控制台页面
- `web/src/api/endpoints`：前端 API 与 React Query 封装

## 使用注意

- 托管渠道由 Site 同步生成，应从 Site 页面维护，不要按普通手动渠道修改。
- 生产部署建议使用 HTTPS，并为 SSE/WebSocket 配置足够长的反向代理超时。
- API Key、站点 Token 和数据库文件属于敏感数据，请妥善保护 `data/` 目录。
- 更新前建议备份数据库；大型 SQLite `relay_logs` 表使用了专门的安全迁移路径。

## 贡献

提交修改前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。每个 PR 应只包含一个明确主题，并在提交前完成相关测试和人工审查。
