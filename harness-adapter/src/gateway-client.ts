/**
 * HTTP client for the remote execution gateway (Go).
 *
 * Reliability contract:
 * - idempotency: every logical execution carries one idempotency key; retries
 *   reuse the same key so the gateway never re-runs a command that already
 *   started (the gateway dedupes before runtime allocation).
 * - retry: only retryable failures (network errors, HTTP 5xx, gateway timeouts)
 *   are retried with exponential backoff + jitter; 4xx and command failures are
 *   never retried.
 * - circuit breaker: after `circuitThreshold` consecutive retryable failures
 *   the client opens for `circuitCooldownMs` and fails fast with a clear error.
 * - cancellation: an AbortSignal aborts in-flight requests and polling loops.
 */

export interface ExecOptions {
  workdir?: string;
  env?: Record<string, string>;
  timeoutSec?: number;
  outputLimit?: number;
}

export interface ExecResult {
  executionId: string;
  processId: string;
  status: 'COMPLETED' | 'FAILED';
  exitCode: number;
  stdout: string;
  stderr: string;
  terminationReason?: string;
}

/** Errors that must never be retried. */
export class NonRetryableError extends Error {
  readonly retryable = false as const;
}

/** Transient failures (network, 5xx, timeout) that may be retried. */
export class RetryableError extends Error {
  readonly retryable = true as const;
}

/** The circuit is open: fail fast instead of hammering the gateway. */
export class CircuitOpenError extends Error {
  readonly retryable = false as const;
}

const sleep = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new NonRetryableError('cancelled'));
      return;
    }
    const onAbort = () => {
      clearTimeout(t);
      reject(new NonRetryableError('cancelled'));
    };
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    signal?.addEventListener('abort', onAbort, { once: true });
  });

export interface GatewayClientOptions {
  maxRetries?: number;
  circuitThreshold?: number;
  circuitCooldownMs?: number;
  pollIntervalMs?: number;
}

export class GatewayClient {
  private readonly baseUrl: string;
  private readonly tenantId: string;
  private readonly maxRetries: number;
  private readonly circuitThreshold: number;
  private readonly circuitCooldownMs: number;
  private readonly pollIntervalMs: number;
  private readonly worlds = new Map<string, { worldId: string; generation: number }>();
  private consecutiveFailures = 0;
  private circuitOpenUntil = 0;

  constructor(baseUrl: string, tenantId: string, opts: GatewayClientOptions = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.tenantId = tenantId;
    this.maxRetries = opts.maxRetries ?? 2;
    this.circuitThreshold = opts.circuitThreshold ?? 5;
    this.circuitCooldownMs = opts.circuitCooldownMs ?? 30_000;
    this.pollIntervalMs = opts.pollIntervalMs ?? 200;
  }

  /** Run one command in a per-session execution world. */
  async execute(
    sessionId: string,
    argv: string[],
    opts: ExecOptions = {},
    signal?: AbortSignal,
  ): Promise<ExecResult> {
    if (this.isCircuitOpen()) {
      throw new CircuitOpenError(
        `gateway circuit open (after ${this.consecutiveFailures} consecutive failures); retry later`,
      );
    }
    const worldId = await this.ensureWorld(sessionId, signal);
    const idempotencyKey = crypto.randomUUID();
    try {
      const { executionId, processId } = await this.request<{ executionId: string; processId: string }>(
        'POST',
        `/v1/worlds/${worldId}/executions`,
        {
          argv,
          workdir: opts.workdir ?? '/workspace',
          env: opts.env ?? {},
          timeout_sec: opts.timeoutSec ?? 30,
          output_limit: opts.outputLimit ?? 1 << 20,
        },
        { 'Idempotency-Key': idempotencyKey },
        signal,
      );
      this.recordSuccess();
      return await this.waitForCompletion(executionId, processId, opts.timeoutSec ?? 30, signal);
    } catch (err) {
      if (err instanceof RetryableError) {
        this.recordFailure();
      }
      throw err;
    }
  }

  /** Ensure an execution world exists for the session (created once, cached). */
  private async ensureWorld(sessionId: string, signal?: AbortSignal): Promise<string> {
    const cached = this.worlds.get(sessionId);
    if (cached) {
      return cached.worldId;
    }
    const body = {
      session_id: sessionId,
      resource: { cpu: 1, memory_mb: 256, pids: 64, disk_mb: 512, timeout_sec: 3600 },
    };
    const created = await this.request<{ id: string; generation: number }>(
      'POST',
      '/v1/worlds',
      body,
      {},
      signal,
    );
    this.worlds.set(sessionId, { worldId: created.id, generation: created.generation });
    return created.id;
  }

  private async waitForCompletion(
    executionId: string,
    processId: string,
    timeoutSec: number,
    signal?: AbortSignal,
  ): Promise<ExecResult> {
    const deadline = Date.now() + (timeoutSec + 30) * 1000;
    const drain = async (stream: 'stdout' | 'stderr') => {
      let offset = 0;
      const chunks: string[] = [];
      for (;;) {
        const out = await this.request<{ data: string; eof: boolean }>(
          'GET',
          `/v1/processes/${processId}/output?stream=${stream}&offset=${offset}&limit=1048576`,
          undefined,
          {},
          signal,
        );
        chunks.push(out.data);
        offset += out.data.length;
        if (out.eof) {
          return chunks.join('');
        }
        if (Date.now() > deadline) {
          throw new RetryableError(`output drain timed out for ${stream}`);
        }
        await sleep(this.pollIntervalMs, signal);
      }
    };
    for (;;) {
      const exec = await this.request<{
        status: string;
        exit_code?: number;
        termination_reason?: string;
      }>(
        'GET',
        `/v1/executions/${executionId}`,
        undefined,
        {},
        signal,
      );
      if (exec.status === 'COMPLETED' || exec.status === 'FAILED') {
        const [stdout, stderr] = await Promise.all([drain('stdout'), drain('stderr')]);
        return {
          executionId,
          processId,
          status: exec.status,
          exitCode: exec.exit_code ?? (exec.status === 'COMPLETED' ? 0 : 1),
          stdout,
          stderr,
          terminationReason: exec.termination_reason,
        };
      }
      if (Date.now() > deadline) {
        throw new RetryableError(`execution ${executionId} did not finish within ${timeoutSec + 30}s`);
      }
      await sleep(this.pollIntervalMs, signal);
    }
  }

  /**
   * One HTTP round trip with retry on retryable failures. The same idempotency
   * key rides every attempt, so a retry after a lost response cannot re-run a
   * command that already started at the gateway.
   */
  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    extraHeaders: Record<string, string> = {},
    signal?: AbortSignal,
  ): Promise<T> {
    let attempt = 0;
    for (;;) {
      try {
        const res = await fetch(this.baseUrl + path, {
          method,
          headers: {
            'Content-Type': 'application/json',
            'X-Tenant-ID': this.tenantId,
            ...extraHeaders,
          },
          body: body === undefined ? undefined : JSON.stringify(body),
          signal,
        });
        const text = await res.text();
        if (!res.ok) {
          if (res.status >= 500 || res.status === 408 || res.status === 429) {
            throw new RetryableError(`gateway ${res.status}: ${text}`);
          }
          throw new NonRetryableError(`gateway ${res.status}: ${text}`);
        }
        return (text ? JSON.parse(text) : undefined) as T;
      } catch (err) {
        if (err instanceof NonRetryableError) {
          throw err;
        }
        if (err instanceof RetryableError) {
          if (attempt < this.maxRetries) {
            await sleep(this.backoffMs(attempt), signal);
            attempt += 1;
            continue;
          }
          throw err;
        }
        // Network-level failure (fetch TypeError): retryable.
        if (attempt < this.maxRetries) {
          await sleep(this.backoffMs(attempt), signal);
          attempt += 1;
          continue;
        }
        throw new RetryableError(`gateway unreachable: ${String(err)}`);
      }
    }
  }

  private backoffMs(attempt: number): number {
    const base = 300 * 2 ** attempt;
    return base + Math.floor(Math.random() * base * 0.3);
  }

  private recordSuccess(): void {
    this.consecutiveFailures = 0;
  }

  private recordFailure(): void {
    this.consecutiveFailures += 1;
    if (this.consecutiveFailures >= this.circuitThreshold) {
      this.circuitOpenUntil = Date.now() + this.circuitCooldownMs;
    }
  }

  private isCircuitOpen(): boolean {
    return Date.now() < this.circuitOpenUntil;
  }
}
