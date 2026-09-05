package domain

import "time"

type WorldStatus string

const (
	WorldCreating    WorldStatus = "CREATING"
	WorldReady       WorldStatus = "READY"
	WorldTerminating WorldStatus = "TERMINATING"
	WorldTerminated  WorldStatus = "TERMINATED"
)

type ExecutionStatus string

const (
	ExecPending   ExecutionStatus = "PENDING"
	ExecRunning   ExecutionStatus = "RUNNING"
	ExecCompleted ExecutionStatus = "COMPLETED"
	ExecFailed    ExecutionStatus = "FAILED"
)

type ProcessStatus string

const (
	ProcessCreated  ProcessStatus = "CREATED"
	ProcessStarting ProcessStatus = "STARTING"
	ProcessRunning  ProcessStatus = "RUNNING"
	ProcessExited   ProcessStatus = "EXITED"
	ProcessKilled   ProcessStatus = "KILLED"
	ProcessOOM      ProcessStatus = "OOM"
)

type ResourceSpec struct {
	CPU        int   `json:"cpu"`
	MemoryMB   int64 `json:"memory_mb"`
	PIDs       int   `json:"pids"`
	DiskMB     int64 `json:"disk_mb"`
	TimeoutSec int   `json:"timeout_sec"`
}
type World struct {
	ID          string
	Generation  int64
	TenantID    string
	SessionID   string
	RuntimeType string
	RuntimeRef  string
	Resource    ResourceSpec
	Status      WorldStatus
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type CommandSpec struct {
	Argv        []string          `json:"argv"`
	Workdir     string            `json:"workdir"`
	Env         map[string]string `json:"env"`
	TimeoutSec  int               `json:"timeout_sec"`
	OutputLimit int64             `json:"output_limit"`
}
type Execution struct {
	ID                string
	WorldID           string
	TenantID          string
	SessionID         string
	IdempotencyKey    string
	Status            ExecutionStatus
	Attempt           int
	Command           CommandSpec
	StartedAt         *time.Time
	FinishedAt        *time.Time
	ExitCode          *int
	TerminationReason string
	LeaseVersion      int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
type Process struct {
	ID           string
	ExecutionID  string
	WorldID      string
	WorkerID     string
	PID          int
	ProcessGroup int
	Status       ProcessStatus
	StartedAt    time.Time
	FinishedAt   *time.Time
	ExitCode     *int
	Signal       string
}
type Lease struct {
	WorldID    string
	WorkerID   string
	Version    int64
	AcquiredAt time.Time
	ExpiresAt  time.Time
}
