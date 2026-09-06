/**
 * DeepSeek Harness plugin: route tool execution to the remote execution
 * gateway (Go). The gateway runs each command inside an isolated Docker
 * execution world (no network, resource limits) and guarantees idempotency,
 * bounded output and process-group cleanup.
 *
 * Install: `dsh plugin add @daqiaoliang-coder/dsh-remote-executor`
 * (see README for the exact profile wiring).
 */
import { defineTool } from '@deepseek-ai/dsh-tools';
import type { Context } from '@deepseek-ai/cordis';
import { CircuitOpenError, GatewayClient, NonRetryableError } from './gateway-client.js';

export const name = 'remote-executor';
export const inject = ['tools'];

export interface Config {
  /** Gateway base URL. Defaults to DSH_EXECUTOR_URL or http://127.0.0.1:8080 */
  baseUrl?: string;
  /** Tenant used for gateway isolation. Defaults to DSH_EXECUTOR_TENANT or "dsh-default". */
  tenantId?: string;
  /** Session id used to derive the execution world. Defaults to "dsh-session". */
  sessionId?: string;
  /** Max retries for retryable gateway failures (default 2). */
  maxRetries?: number;
  /** Consecutive failures before the circuit opens (default 5). */
  circuitThreshold?: number;
  /** Circuit cooldown in ms (default 30000). */
  circuitCooldownMs?: number;
}

interface ToolOutput {
  execution_id: string;
  status: string;
  exit_code: number;
  stdout: string;
  stderr: string;
}

function formatResult(v: ToolOutput): string {
  const lines: string[] = [`exit ${v.exit_code} (${v.status})`];
  if (v.stdout) {
    lines.push(v.stdout);
  }
  if (v.stderr) {
    lines.push(`[stderr]\n${v.stderr}`);
  }
  return lines.join('\n');
}

export function apply(ctx: Context, config: Config = {}) {
  const baseUrl = config.baseUrl ?? process.env.DSH_EXECUTOR_URL ?? 'http://127.0.0.1:8080';
  const tenantId = config.tenantId ?? process.env.DSH_EXECUTOR_TENANT ?? 'dsh-default';
  const sessionId = config.sessionId ?? 'dsh-session';
  const client = new GatewayClient(baseUrl, tenantId, {
    maxRetries: config.maxRetries,
    circuitThreshold: config.circuitThreshold,
    circuitCooldownMs: config.circuitCooldownMs,
  });

  const tool = defineTool({
    name: 'remote_execute',
    description:
      'Execute a shell command inside the remote execution gateway sandbox (isolated Docker world, no network, resource limits). Returns stdout, stderr and the exit code.',
    parameters: {
      command: {
        type: 'string',
        required: true,
        description: 'the shell command to run, e.g. "ls -la"',
      },
      timeout_sec: {
        type: 'integer',
        description: 'command timeout in seconds',
        default: 30,
      },
      workdir: {
        type: 'string',
        description: 'working directory, must be under /workspace',
        default: '/workspace',
      },
    },
    output: {
      schema: {
        type: 'object',
        additionalProperties: false,
        properties: {
          execution_id: { type: 'string', required: true, description: 'gateway execution id' },
          status: { type: 'string', required: true, description: 'COMPLETED or FAILED' },
          exit_code: { type: 'integer', required: true, description: 'process exit code' },
          stdout: { type: 'string', required: true, description: 'captured stdout' },
          stderr: { type: 'string', required: true, description: 'captured stderr' },
        },
      },
      render: (_args, value) => [{ type: 'text', text: formatResult(value) }],
    },
    timeoutMs: 120_000,
    async execute(args, exec) {
      try {
        const result = await client.execute(
          sessionId,
          ['sh', '-lc', args.command],
          {
            workdir: args.workdir ?? '/workspace',
            timeoutSec: args.timeout_sec ?? 30,
          },
          exec.signal,
        );
        return {
          execution_id: result.executionId,
          status: result.status,
          exit_code: result.exitCode,
          stdout: result.stdout,
          stderr: result.stderr,
        };
      } catch (err) {
        if (err instanceof CircuitOpenError || err instanceof NonRetryableError) {
          throw new Error(`remote_execute failed: ${err.message}`);
        }
        throw err;
      }
    },
  });

  ctx.tools.register(tool);
}
