package runtime

import (
	"context"
	"io"
	"os"
	"time"
)

type WorldSpec struct {
	ID          string
	ResourceCPU int
	MemoryMB    int64
	PIDs        int
	DiskMB      int64
	Timeout     time.Duration
}
type WorldHandle struct {
	ID         string
	Generation int64
	Ref        string
}
type ProcessSpec struct {
	Argv        []string
	Workdir     string
	Env         map[string]string
	Timeout     time.Duration
	OutputLimit int64
}
type ProcessHandle struct {
	ID           string
	PID          int
	ProcessGroup int
	Stdout       io.ReadCloser
	Stderr       io.ReadCloser
	Done         <-chan Result
}
type Result struct {
	ExitCode   int
	Signal     string
	Err        error
	StartedAt  time.Time
	FinishedAt time.Time
}
type Runtime interface {
	CreateWorld(context.Context, WorldSpec) (WorldHandle, error)
	DestroyWorld(context.Context, WorldHandle) error
	StartProcess(context.Context, WorldHandle, ProcessSpec) (ProcessHandle, error)
	KillProcess(context.Context, ProcessHandle, os.Signal) error
}
type osSignal interface{ String() string }
