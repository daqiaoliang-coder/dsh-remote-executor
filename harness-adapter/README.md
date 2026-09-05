# DeepSeek Harness adapter contract

This directory intentionally does not fork Harness or guess its package-registration internals.

The contract maps the execution gateway to Harness capability seams:

- `ctx.fs` -> `RemoteFileSystemProvider`
- `ctx.subprocess` -> `RemoteSubprocessProvider`
- `ctx.shell` -> a higher-level provider that consumes subprocess

The same `ExecutionWorldRef` must be shared by filesystem and subprocess providers. This is the key correctness property: the process that writes `/workspace/foo` must be able to observe the same `/workspace/foo` through `ctx.fs`.
