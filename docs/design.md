# Design notes

## 1. World is the consistency boundary

A world is a long-lived execution environment. Its mutable filesystem and process tree belong to the same world. Per-call containers would create split-brain state:

```text
wrong: FS -> C1, Process -> C2
right: World W1 -> FS + Process + Shell
```

## 2. Execution != Process

`Execution` is the durable logical operation. `Process` is one concrete attempt. This makes retries/recovery explicit:

```text
E1
 |- attempt 1 -> P1
 `- attempt 2 -> P2
```

## 3. Correctness layers

- MySQL: source of truth for world/execution/process lifecycle.
- Redis: lease/coordination only.
- Runtime: actual process/container state.
- Gateway: admission, idempotency and lifecycle transitions.

## 4. Idempotency

The idempotency key is checked before runtime allocation. Repeating a request with the same tenant + key returns the existing execution rather than creating a second process.

## 5. Fencing

A lease version is a fencing token. A stale worker must not mutate durable state after losing ownership. V1 keeps the model and Redis implementation; a production implementation should enforce the version in every state mutation.

## 6. Crash recovery

A worker/process crash is not automatically equivalent to a safe business retry. V1 marks lost execution failed and destroys the old world. Retrying arbitrary external side effects is deliberately outside the gateway.

## 7. Output

Output is consumed asynchronously into bounded buffers. Reads use offset + limit, avoiding unbounded RPC payloads and memory growth. A production version should spill large output to object storage and expose durable cursors.

## 8. Security boundary

This project is not a security sandbox claim. Docker isolation, network policy, credential handling, kernel hardening and tenant isolation require a dedicated production threat model. The gateway focuses on execution semantics and governance.
