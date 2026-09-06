# DeepSeek Harness adapter：`remote_execute` 工具

一个可加载的 Harness 插件，把工具执行路由到远程执行网关（Go）。网关在每个隔离的 Docker Execution World（无网络、资源限制、进程组清理）中执行命令，并保证幂等与有界输出。

这是一个真正的 Cordis 插件（`apply(ctx)` + `defineTool`），而不只是接口契约。原始的 seam 映射仍然成立：

- `ctx.fs` -> `RemoteFileSystemProvider`（见 `src/gateway-client.ts`）
- `ctx.subprocess` -> `RemoteSubprocessProvider`
- `ctx.shell` -> subprocess 的上层消费者
- `remote_execute` -> 注册到 `ctx.tools` 的面向模型工具

文件系统与进程调用必须共享同一个 Execution World：写入 `/workspace/foo` 的进程，必须能通过读取看到同一个文件。

## 安装

```bash
npm install
npm run build          # 产出 lib/
# 加载到 dsh（out-of-tree 插件路径，见官方插件文档）：
dsh plugin add /绝对路径/harness-adapter
```

## 配置

| 配置键 | 环境变量 | 默认值 | 含义 |
|---|---|---|---|
| `baseUrl` | `DSH_EXECUTOR_URL` | `http://127.0.0.1:8080` | 网关 base URL |
| `tenantId` | `DSH_EXECUTOR_TENANT` | `dsh-default` | 网关租户隔离 |
| `sessionId` | — | `dsh-session` | 用于派生 Execution World 的会话 |
| `maxRetries` | — | `2` | 可重试网关失败的重试次数 |
| `circuitThreshold` | — | `5` | 熔断前连续失败次数 |
| `circuitCooldownMs` | — | `30000` | 熔断冷却时间 |

## 可靠性契约

- **幂等**：每次逻辑执行生成一个 `crypto.randomUUID()`；重试复用同一个键，因此响应丢失后的重试不会重复执行已在网关启动的命令（网关在运行时资源分配之前去重）。
- **重试**：只重试可重试的失败（网络错误、HTTP 5xx、408、429、输出拉取/轮询超时），使用指数退避 + 抖动。4xx 与命令执行失败绝不重试。
- **熔断**：连续 `circuitThreshold` 次可重试失败后，客户端进入 `circuitCooldownMs` 冷却并快速失败，给出明确错误。
- **取消**：harness 的 `exec.signal` 会转发给进行中的 fetch 与轮询循环。

## 客户端 API 参考

```ts
import { GatewayClient } from './src/gateway-client.js';

const client = new GatewayClient('http://127.0.0.1:8080', 'dsh-default');
const result = await client.execute('s1', ['sh', '-lc', 'ls -la'], { timeoutSec: 30 });
// { executionId, processId, status, exitCode, stdout, stderr }
```
