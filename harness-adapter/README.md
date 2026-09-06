# DeepSeek Harness adapter: `remote_execute` tool

A loadable Harness plugin that routes tool execution to the remote execution
gateway (Go). The gateway runs each command inside an isolated Docker
Execution World (no network, resource limits, process-group cleanup) and
guarantees idempotency and bounded output.

This directory is a real Cordis plugin (`apply(ctx)` + `defineTool`), not just
a contract. The original seam mapping still holds:

- `ctx.fs` -> `RemoteFileSystemProvider` (see `src/gateway-client.ts`)
- `ctx.subprocess` -> `RemoteSubprocessProvider`
- `ctx.shell` -> a higher-level consumer of subprocess
- `remote_execute` -> the model-facing tool registered on `ctx.tools`

The same Execution World must be shared by filesystem and process calls: the
process that writes `/workspace/foo` must observe the same file through reads.

## Install

```bash
npm install
npm run build          # emits lib/
# load into dsh (out-of-tree plugin path, see official plugin docs):
dsh plugin add /abs/path/to/harness-adapter
```

## Configuration

| Config key | Env var | Default | Meaning |
|---|---|---|---|
| `baseUrl` | `DSH_EXECUTOR_URL` | `http://127.0.0.1:8080` | gateway base URL |
| `tenantId` | `DSH_EXECUTOR_TENANT` | `dsh-default` | gateway tenant isolation |
| `sessionId` | — | `dsh-session` | session used to derive the Execution World |
| `maxRetries` | — | `2` | retries for retryable gateway failures |
| `circuitThreshold` | — | `5` | consecutive failures before circuit opens |
| `circuitCooldownMs` | — | `30000` | circuit cooldown |

## Reliability contract

- **Idempotency**: one `crypto.randomUUID()` per logical execution; retries
  reuse the same key, so a retry after a lost response cannot re-run a command
  that already started at the gateway (the gateway dedupes before runtime
  allocation).
- **Retry**: only retryable failures (network errors, HTTP 5xx, 408, 429,
  drain/poll timeouts) are retried with exponential backoff + jitter. 4xx and
  command failures are never retried.
- **Circuit breaker**: after `circuitThreshold` consecutive retryable failures
  the client opens for `circuitCooldownMs` and fails fast with a clear error.
- **Cancellation**: the harness `exec.signal` is forwarded to in-flight fetches
  and polling loops.

## API reference (client)

```ts
import { GatewayClient } from './src/gateway-client.js';

const client = new GatewayClient('http://127.0.0.1:8080', 'dsh-default');
const result = await client.execute('s1', ['sh', '-lc', 'ls -la'], { timeoutSec: 30 });
// { executionId, processId, status, exitCode, stdout, stderr }
```
