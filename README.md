# Octopus

Octopus 是一个面向个人和小团队的自托管 LLM API 网关与管理控制台。它将多个模型供应商和中转服务统一到一组兼容 API 后面，并提供渠道管理、协议转换、负载均衡、故障转移、缓存统计和请求诊断。

> 社区推荐：[Linux.do](https://linux.do/)

本仓库从 [`tianxia3111/octopus`](https://github.com/tianxia3111/octopus) 派生，项目源自 [`bestruirui/octopus`](https://github.com/bestruirui/octopus)。当前分支重点增强了站点同步、OpenAI Responses、WebSocket Relay、可观测性和路由诊断。

主文档使用中文；[README_zh.md](README_zh.md) 保留为兼容入口。

## 近期阶段性成果（2026-07 ～ 2026-08）

这一阶段的工作重点不是单纯增加页面，而是让 Octopus 在 Codex、长连接中转、多渠道故障切换和日常运维场景中更稳定、更容易定位问题。

| 方向 | 已完成内容 |
| --- | --- |
| Codex / OpenAI Responses | 增加 `/v1/codex/responses` 与 `/backend-api/codex/responses` 别名；转发 Session、Thread、Turn-State 身份头并隔离连接池；修正 `length`、`content_filter`、工具调用未完成等终止语义；支持 HTTP replay、WebSocket 续接及异常流恢复 |
| 智能路由 | 增加 Auto 分组策略，根据渠道和模型的真实成功率、延迟及样本量动态排序；支持最小样本、统计时间窗、窗口容量和延迟权重配置；保留熔断、粘性会话、同渠道重试和跨渠道故障转移 |
| 渠道诊断 | 渠道详情可直接测试 OpenAI 图片生成；新增 Sub2API `/v1/usage` 余额查询，兼容额度、钱包和旧版余额字段；测试请求使用服务端保存的渠道 Key，不计入业务统计 |
| 国内网络适配 | 模型价格更新支持自定义 `models.dev` 镜像或反代地址；远程价格不可达时继续使用内置价格表，不影响网关启动和转发 |
| 协议兼容 | 过滤 Gemini 不支持的 JSON Schema 关键字；跨协议转换前去重重复工具结果；OpenAI Responses 在缺失 usage、终止事件不完整等情况下补齐正确状态 |
| 数据与日志安全 | 单条 Relay 日志正文默认限制为 2 MiB，并限制 attempts 决策记录，避免超大请求、图片响应或异常重试撑大数据库；日志仍保留路由尝试、状态码和协议诊断信息 |
| 移动端与运维 | 修复日志卡片、渠道/分组表单和长错误信息在手机端的横向溢出；中文化项目说明和更新日志；更新方式统一给出拉取新构建产物的明确提示 |

上述改动均配套了 Go 单元/回归测试、路由注册测试和前端类型检查。提交前建议至少运行：

```bash
go test ./...
go vet ./...
cd web
pnpm exec tsc --noEmit
pnpm lint
pnpm build
```

## Highlights

### Unified model APIs

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | OpenAI Chat Completions |
| `POST /v1/responses` | Streaming and non-streaming OpenAI Responses |
| `GET /v1/responses` | OpenAI Responses over WebSocket |
| `POST /v1/responses/compact` | Responses context compaction relay |
| `POST /v1/messages` | Anthropic Messages |
| `POST /v1/embeddings` | OpenAI Embeddings |
| `POST /v1/images/*` | Image generation, edits, and variations |

Outbound adapters support OpenAI Chat, OpenAI Responses, Anthropic, Gemini, Volcengine, and OpenAI Embeddings. Cross-protocol requests use a shared internal representation, while same-protocol paths preserve raw fields and streaming semantics where possible.

### Routing and reliability

- Groups act as client-facing model names and map requests to one or more channel models.
- Round-robin, random, failover, and weighted routing strategies.
- Same-channel retries, cross-channel failover, first-token timeouts, and exponential backoff.
- Circuit breakers scoped by `channel:key:model`, including half-open probes.
- API-key and model-scoped sticky sessions.
- HTTP replay, upstream WebSocket reuse, and session recovery for OpenAI Responses.

### Site and channel management

- Manage relay sites, accounts, user groups, tokens, and models.
- Synchronize account data, models, and balances, with scheduled check-ins.
- Project site accounts into managed channels to reduce duplicate configuration.
- Manual channels, proxy profiles, custom headers, parameter overrides, and automatic model grouping.

### Observability

- Request, input/output token, cache-read, cache-hit-rate, cost, and latency metrics.
- Relay logs with channel attempts, HTTP status, retryability, cooldown, and protocol details.
- In-flight log entries that update in place when a request finishes.
- Route diagnostics for candidate order, key availability, circuit state, and protocol compatibility.
- A live active-request view showing the current routing stage.
- Simplified Chinese, Traditional Chinese, and English UI.

## Request flow

```text
Client request
  → API key authentication
  → Inbound protocol parsing
  → Group matching
  → Load balancing / sticky session
  → Channel and key selection
  → Circuit breaker / retry / failover
  → Outbound transformation or same-protocol passthrough
  → Upstream model API
  → Response transformation, metrics, and logs
```

The Next.js frontend is statically exported and embedded into the Go binary, so a deployment only needs one service process.

## Quick start

### 使用 Docker 镜像

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

### Run from source

Requirements: Go 1.25, Node.js 22+, and pnpm.

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

Open <http://localhost:8080>. The first start creates `data/config.json` and the default SQLite database automatically.

The default username and password are both `admin`. Change them immediately after the first login.

## Configuration

Default configuration:

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

- The default config file is `data/config.json`.
- Use `octopus start --config /path/to/config.json` to select another file.
- Environment variables use the `OCTOPUS_` prefix, for example `OCTOPUS_SERVER_PORT=9090`.
- SQLite, MySQL, and PostgreSQL are supported. For non-SQLite databases, set `database.path` to the appropriate DSN.

## Development and verification

```bash
# Backend
go test ./...
go vet ./...

# Frontend
cd web
pnpm install --frozen-lockfile
pnpm exec tsc --noEmit
pnpm lint
pnpm build
```

Key directories:

- `internal/server`: HTTP service, routes, middleware, and handlers
- `internal/op`: business operations, persistence, and in-memory caches
- `internal/relay`: forwarding, retries, circuit breakers, streaming, and WebSocket support
- `internal/transformer`: inbound and outbound protocol adapters
- `internal/sitesync`: site synchronization, check-ins, and managed-channel projection
- `web/src/components/modules`: management console modules
- `web/src/api/endpoints`: frontend API and React Query wrappers

## Operational notes

- Managed channels are generated by Site synchronization and should be maintained from the Site UI.
- Production deployments should use HTTPS and configure generous reverse-proxy timeouts for SSE and WebSocket traffic.
- API keys, site tokens, and database files are sensitive; protect the `data/` directory.
- Back up the database before upgrades. Large SQLite `relay_logs` tables use a dedicated safe migration path.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before submitting changes. Keep each pull request focused on one topic and complete the relevant tests and human review first.
