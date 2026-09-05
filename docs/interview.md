# Interview Q&A

### Why not create a container for every command?
Because an Agent session is stateful. File edits, generated artifacts, installed dependencies and running processes need one shared execution world. Per-command containers break that consistency.

### Why is Execution separate from Process?
An execution is a logical request; a process is an attempt. The separation is necessary for retry, worker failover and audit.

### Why MySQL + Redis?
Lifecycle state needs durable transactional storage, while leases and short-lived coordination need low-latency TTL semantics. Redis is not the source of truth.

### Why argv instead of command string?
The subprocess boundary should not implicitly invoke a shell. Shell syntax belongs to `ctx.shell`/Bash. This reduces quoting ambiguity and command-injection surface at the API boundary.

### How do you avoid orphan processes?
The gateway creates a process group and uses TERM -> grace period -> KILL. A production runtime should also use a container init process and enforce PID limits.

### What happens if the worker dies?
The lease expires. A recovery worker detects the stale ownership, fences the old owner, and in V1 destroys the old execution world rather than pretending it can safely resume arbitrary side effects.

### What would you add next?
Hierarchical quotas, worker pool scheduling, RocketMQ lifecycle events, durable output storage, stronger fencing in SQL, world warm pools, worker draining and a second runtime such as E2B/Kubernetes.
