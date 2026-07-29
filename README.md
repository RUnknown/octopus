# Octopus

Octopus is a self-hosted LLM API gateway and management console for individuals and small teams. It places multiple model providers and relay services behind a unified set of compatible APIs, with channel management, protocol transformation, load balancing, failover, cache statistics, and request diagnostics.

This repository is derived from [`tianxia3111/octopus`](https://github.com/tianxia3111/octopus), which is based on [`bestruirui/octopus`](https://github.com/bestruirui/octopus). This fork focuses on site synchronization, OpenAI Responses, WebSocket relay, observability, and route diagnostics.

中文文档：[README_zh.md](README_zh.md)

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

### Existing Docker image

The repository currently inherits the image maintained by the parent fork:

```bash
docker run -d \
  --name octopus \
  -p 8080:8080 \
  -v /path/to/data:/app/data \
  --restart unless-stopped \
  ghcr.io/tianxia3111/octopus:latest
```

Alternatively, update the data path in [`docker-compose.yml`](docker-compose.yml), then run:

```bash
docker compose up -d
```

The inherited image may not contain the latest unreleased commits from this fork. Build from source when you need the current repository state.

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
