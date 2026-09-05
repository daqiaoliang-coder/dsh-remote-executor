export interface ExecutionWorldRef {
  worldId: string;
  generation: number;
}

export interface RemoteGateway {
  createWorld(input: { tenantId: string; sessionId: string }): Promise<ExecutionWorldRef>;
  destroyWorld(world: ExecutionWorldRef): Promise<void>;
  startProcess(input: {
    world: ExecutionWorldRef;
    argv: string[];
    workdir?: string;
    env?: Record<string, string>;
    timeoutSec?: number;
    outputLimit?: number;
    idempotencyKey?: string;
  }): Promise<{ executionId: string; processId: string }>;
  readOutput(input: { processId: string; stream: "stdout" | "stderr"; offset: number; limit: number }): Promise<{ data: string; eof: boolean }>;
  readText(world: ExecutionWorldRef, path: string): Promise<string>;
  writeText(world: ExecutionWorldRef, path: string, content: string): Promise<void>;
}

// Mapping to DeepSeek Harness seams:
//   ctx.fs -> readText/writeText + semantic FS operations
//   ctx.subprocess -> startProcess/readOutput
//   ctx.shell -> keep as a higher-level consumer of subprocess
// The adapter must keep one world reference for both FS and subprocess calls.
export class RemoteSubprocessProvider {
  constructor(private readonly gateway: RemoteGateway, private readonly world: ExecutionWorldRef) {}
  spawn(argv: string[], opts: { cwd?: string; env?: Record<string, string>; timeoutSec?: number } = {}) {
    return this.gateway.startProcess({ world: this.world, argv, workdir: opts.cwd, env: opts.env, timeoutSec: opts.timeoutSec });
  }
}

export class RemoteFileSystemProvider {
  constructor(private readonly gateway: RemoteGateway, private readonly world: ExecutionWorldRef) {}
  readText(path: string) { return this.gateway.readText(this.world, path); }
  writeText(path: string, content: string) { return this.gateway.writeText(this.world, path, content); }
}
