# Agent Execution Gateway v1

Remote Execution & Sandbox Governance for Agent Runtime.

This project is a standalone Go execution plane designed to complement an Agent Runtime. It models an **Execution World** as the consistency boundary shared by filesystem and processes, and adds lifecycle, idempotency, bounded output, resource limits, leases and crash recovery.

## Architecture

```text
DeepSeek Harness / Agent Runtime
        |
 ctx.fs / ctx.subprocess / ctx.shell
        |
 Remote Providers
        | HTTP/JSON (v1)
        v
 Go Execution Gateway
   | admission / idempotency / lifecycle
   +---- MySQL durable state
   +---- Redis lease coordination (optional)
        |
   Docker Runtime
        |
 Execution World
   +-- /workspace filesystem
   +-- process group
   +-- shell/subprocess
```

The important abstraction is **Execution World**, not a per-request container. FS and process calls for one world see the same mutable workspace and process namespace.

## V1 scope

- ExecutionWorld lifecycle
- Docker runtime
- argv-based process execution
- process-group cleanup: TERM -> grace -> KILL
- command timeout
- bounded stdout/stderr buffers with offset reads
- idempotency by tenant + idempotency key
- semantic filesystem read/write/edit under `/workspace`
- Redis lease manager
- expiry reconciliation
- MySQL schema + repository
- HTTP API
- Harness adapter contracts
- **loadable Harness plugin** (`remote_execute` tool with retry + circuit breaker)
- **structured JSON logs** (execution_started / execution_finished / world lifecycle)
- **`/metrics` endpoint** (Prometheus text format, zero-dependency)
- unit/integration-style tests

The runnable V1 binary uses the in-memory repository to keep the demo dependency-free; `internal/repository/mysql.go` and `migrations/001_init.sql` provide the durable MySQL implementation for the next integration step.

V1 deliberately does not claim production-grade isolation. It is a technical project for demonstrating execution-plane architecture.

## Run

Requirements: Go 1.23+, Docker.

```bash
docker compose up -d mysql redis
go test ./...
go run ./cmd/gateway
```

The gateway listens on `:8080`.

Create a world:

```bash
curl -X POST http://localhost:8080/v1/worlds \
  -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: demo' \
  -d '{"session_id":"s1","resource":{"cpu":1,"memory_mb":256,"pids":64,"disk_mb":512,"timeout_sec":60}}'
```

Start a process:

```bash
curl -X POST http://localhost:8080/v1/worlds/WORLD_ID/executions \
  -H 'Content-Type: application/json' \
  -H 'X-Tenant-ID: demo' \
  -H 'Idempotency-Key: exec-001' \
  -d '{"argv":["sh","-lc","echo hello; sleep 1; echo done"],"workdir":"/workspace","timeout_sec":10,"output_limit":1048576}'
```

Read status/output:

```bash
curl http://localhost:8080/v1/executions/EXEC_ID
curl 'http://localhost:8080/v1/processes/PROCESS_ID/output?stream=stdout&offset=0&limit=65536'
```

Write/read a file:

```bash
curl -X PUT http://localhost:8080/v1/worlds/WORLD_ID/fs/write \
  -H 'Content-Type: application/json' \
  -d '{"path":"/workspace/a.txt","content":"hello\n"}'

curl 'http://localhost:8080/v1/worlds/WORLD_ID/fs/read?path=/workspace/a.txt'
```

Metrics:

```bash
curl http://localhost:8080/metrics
```

Every execution is logged as structured JSON on stdout (`execution_started` /
`execution_finished` with duration, exit code and termination reason).

## DeepSeek Harness plugin

`harness-adapter/` ships a loadable Harness plugin that routes the agent's tool
execution to this gateway:

```bash
cd harness-adapter
npm install
npm run build
dsh plugin add <path-or-package>
```

It registers one tool, `remote_execute` (command / timeout_sec / workdir), which:

- lazily creates an Execution World per session (Docker, no network, resource limits)
- executes `sh -lc <command>` through the gateway with an idempotency key
- **retries** retryable gateway failures (network / 5xx / timeout) with exponential
  backoff + jitter, reusing the same idempotency key (never re-runs a started command)
- opens a **circuit breaker** after consecutive failures and fails fast with a clear error
- forwards the harness cancellation signal to in-flight requests and polling

Configuration via plugin config or environment variables:
`DSH_EXECUTOR_URL` (default `http://127.0.0.1:8080`), `DSH_EXECUTOR_TENANT`
(default `dsh-default`), and `sessionId` (default `dsh-session`). See
`harness-adapter/README.md`.

## Relationship to DeepSeek Harness

The adapter follows the capability seams conceptually:

- `ctx.fs` -> `FileSystemGateway`
- `ctx.subprocess` -> execution endpoints
- `ctx.shell` remains a higher-level consumer of subprocess

The remote provider must use one shared `ExecutionWorld`; do not create separate containers for FS and subprocess. This project is intentionally a gateway/runtime implementation rather than a fork of Harness.

## Interview value

The project demonstrates distributed-system topics that are useful for Agent Infra:

1. Execution World as a consistency boundary.
2. Execution != Process: one logical execution can have multiple attempts.
3. Idempotency before runtime allocation.
4. Lease version as a fencing token.
5. Durable state in MySQL; Redis only for coordination.
6. Process-group lifecycle and orphan prevention.
7. Output backpressure/bounded memory.
8. Crash recovery separated from side-effect retry.
9. FS and process sharing one execution world.

See `docs/design.md` and `docs/interview.md`.
