package execution

import (
	"context"
	"errors"
	"fmt"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/domain"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/metrics"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/repository"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/runtime"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Service struct {
	Repo         repository.Repository
	Runtime      runtime.Runtime
	Metrics      *metrics.Metrics
	mu           sync.Mutex
	buffers      map[string]*runtime.Buffer
	procHandles  map[string]runtime.ProcessHandle
	worldHandles map[string]runtime.WorldHandle
}

func NewService(r repository.Repository, rt runtime.Runtime) *Service {
	return &Service{Repo: r, Runtime: rt, Metrics: metrics.New(), buffers: map[string]*runtime.Buffer{}, procHandles: map[string]runtime.ProcessHandle{}, worldHandles: map[string]runtime.WorldHandle{}}
}
func id(prefix string) string { return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()) }
func (s *Service) CreateWorld(ctx context.Context, tenant, session string, res domain.ResourceSpec) (*domain.World, error) {
	now := time.Now()
	w := &domain.World{ID: id("w"), Generation: 1, TenantID: tenant, SessionID: session, RuntimeType: "docker", Resource: res, Status: domain.WorldCreating, ExpiresAt: now.Add(time.Duration(res.TimeoutSec) * time.Second), CreatedAt: now, UpdatedAt: now}
	if res.TimeoutSec <= 0 {
		w.ExpiresAt = now.Add(10 * time.Minute)
	}
	if e := s.Repo.CreateWorld(w); e != nil {
		return nil, e
	}
	h, e := s.Runtime.CreateWorld(ctx, runtime.WorldSpec{ID: w.ID, ResourceCPU: res.CPU, MemoryMB: res.MemoryMB, PIDs: res.PIDs, DiskMB: res.DiskMB, Timeout: time.Until(w.ExpiresAt)})
	if e != nil {
		w.Status = domain.WorldTerminated
		w.UpdatedAt = time.Now()
		_ = s.Repo.UpdateWorld(w)
		return nil, e
	}
	w.RuntimeRef = h.Ref
	w.Status = domain.WorldReady
	w.UpdatedAt = time.Now()
	_ = s.Repo.UpdateWorld(w)
	s.mu.Lock()
	s.worldHandles[w.ID] = h
	s.mu.Unlock()
	slog.Info("world_created", "world_id", w.ID, "tenant", tenant, "session", session, "runtime", w.RuntimeType)
	return w, nil
}
func (s *Service) DestroyWorld(ctx context.Context, id string, generation int64) error {
	w, e := s.Repo.GetWorld(id)
	if e != nil {
		return e
	}
	if generation != 0 && generation != w.Generation {
		return errors.New("generation mismatch")
	}
	w.Status = domain.WorldTerminating
	w.UpdatedAt = time.Now()
	_ = s.Repo.UpdateWorld(w)
	s.mu.Lock()
	h := s.worldHandles[id]
	s.mu.Unlock()
	if h.ID == "" {
		h = runtime.WorldHandle{ID: w.ID, Generation: w.Generation, Ref: w.RuntimeRef}
	}
	e = s.Runtime.DestroyWorld(ctx, h)
	w.Status = domain.WorldTerminated
	w.UpdatedAt = time.Now()
	_ = s.Repo.UpdateWorld(w)
	slog.Info("world_terminated", "world_id", w.ID, "tenant", w.TenantID)
	return e
}
func (s *Service) Start(ctx context.Context, tenant, worldID, idem string, cmd domain.CommandSpec) (*domain.Execution, *domain.Process, error) {
	w, e := s.Repo.GetWorld(worldID)
	if e != nil {
		return nil, nil, e
	}
	if w.TenantID != tenant {
		return nil, nil, errors.New("tenant mismatch")
	}
	if w.Status != domain.WorldReady {
		return nil, nil, errors.New("world not ready")
	}
	if idem != "" {
		if old, e := s.Repo.FindExecutionByIdempotency(tenant, idem); e == nil {
			return old, nil, nil
		}
	}
	if len(cmd.Argv) == 0 {
		return nil, nil, errors.New("argv is required")
	}
	cmd.Workdir = normalizeWorkdir(cmd.Workdir)
	if !underWorkspace(cmd.Workdir) {
		return nil, nil, errors.New("workdir must be under /workspace")
	}
	if cmd.OutputLimit <= 0 {
		cmd.OutputLimit = 1 << 20
	}
	if cmd.TimeoutSec <= 0 {
		cmd.TimeoutSec = w.Resource.TimeoutSec
	}
	now := time.Now()
	ex := &domain.Execution{ID: id("e"), WorldID: w.ID, TenantID: tenant, SessionID: w.SessionID, IdempotencyKey: idem, Status: domain.ExecPending, Attempt: 1, Command: cmd, CreatedAt: now, UpdatedAt: now}
	if e = s.Repo.CreateExecution(ex); e != nil {
		return nil, nil, e
	}
	s.mu.Lock()
	h := s.worldHandles[w.ID]
	s.mu.Unlock()
	if h.ID == "" {
		h = runtime.WorldHandle{ID: w.ID, Generation: w.Generation, Ref: w.RuntimeRef}
	}
	// The process must outlive the HTTP request that started it: the request
	// context is cancelled once the handler returns, which would kill the
	// docker exec child. Execution lifecycle is governed by the process
	// timeout (TERM -> grace -> KILL) inside the runtime, not by the caller.
	ph, e := s.Runtime.StartProcess(context.Background(), h, runtime.ProcessSpec{Argv: cmd.Argv, Workdir: cmd.Workdir, Env: cmd.Env, Timeout: time.Duration(cmd.TimeoutSec) * time.Second, OutputLimit: cmd.OutputLimit})
	if e != nil {
		ex.Status = domain.ExecFailed
		ex.TerminationReason = "START_FAILED"
		ex.UpdatedAt = time.Now()
		_ = s.Repo.UpdateExecution(ex)
		return ex, nil, e
	}
	p := &domain.Process{ID: ph.ID, ExecutionID: ex.ID, WorldID: w.ID, WorkerID: "gateway-local", PID: ph.PID, ProcessGroup: ph.ProcessGroup, Status: domain.ProcessRunning, StartedAt: now}
	ex.Status = domain.ExecRunning
	ex.StartedAt = &now
	ex.UpdatedAt = now
	_ = s.Repo.UpdateExecution(ex)
	_ = s.Repo.CreateProcess(p)
	s.Metrics.ExecutionStarted(ex.ID)
	slog.Info("execution_started", "execution_id", ex.ID, "tenant", tenant, "world_id", w.ID, "process_id", p.ID, "argv", cmd.Argv, "timeout_sec", cmd.TimeoutSec, "output_limit", cmd.OutputLimit, "idempotency_key", idem)
	out := runtime.NewBuffer(cmd.OutputLimit)
	errb := runtime.NewBuffer(cmd.OutputLimit)
	s.mu.Lock()
	s.procHandles[p.ID] = ph
	s.buffers[p.ID+":stdout"] = out
	s.buffers[p.ID+":stderr"] = errb
	s.mu.Unlock()
	go out.Consume(ph.Stdout)
	go errb.Consume(ph.Stderr)
	go s.finish(ex, p, ph)
	return ex, p, nil
}
func (s *Service) finish(ex *domain.Execution, p *domain.Process, h runtime.ProcessHandle) {
	r := <-h.Done
	now := r.FinishedAt
	dur := now.Sub(r.StartedAt)
	failed := false
	p.FinishedAt = &now
	p.ExitCode = &r.ExitCode
	if r.ExitCode == 0 {
		p.Status = domain.ProcessExited
		ex.Status = domain.ExecCompleted
	} else {
		p.Status = domain.ProcessExited
		ex.Status = domain.ExecFailed
		ex.TerminationReason = "PROCESS_EXIT"
		failed = true
	}
	ex.FinishedAt = &now
	ex.ExitCode = &r.ExitCode
	ex.UpdatedAt = now
	_ = s.Repo.UpdateProcess(p)
	_ = s.Repo.UpdateExecution(ex)
	s.Metrics.ExecutionFinished(ex.ID, failed, dur)
	slog.Info("execution_finished", "execution_id", ex.ID, "tenant", ex.TenantID, "world_id", ex.WorldID, "status", ex.Status, "exit_code", r.ExitCode, "termination_reason", ex.TerminationReason, "duration_ms", dur.Milliseconds())
}
func (s *Service) GetExecution(id string) (*domain.Execution, error) { return s.Repo.GetExecution(id) }
func (s *Service) GetProcess(id string) (*domain.Process, error)     { return s.Repo.GetProcess(id) }
func (s *Service) ReadOutput(pid, stream string, offset, limit int64) ([]byte, bool, error) {
	if stream != "stdout" && stream != "stderr" {
		return nil, false, errors.New("stream must be stdout or stderr")
	}
	s.mu.Lock()
	b := s.buffers[pid+":"+stream]
	s.mu.Unlock()
	if b == nil {
		return nil, false, repository.ErrNotFound
	}
	d, eof := b.Read(offset, limit)
	return d, eof, nil
}
func (s *Service) Kill(ctx context.Context, pid string) error {
	s.mu.Lock()
	h, ok := s.procHandles[pid]
	s.mu.Unlock()
	if !ok {
		return repository.ErrNotFound
	}
	return s.Runtime.KillProcess(ctx, h, syscall.SIGTERM)
}
func normalizeWorkdir(w string) string {
	if w == "" {
		return "/workspace"
	}
	return path.Clean(w)
}
func underWorkspace(p string) bool { return p == "/workspace" || strings.HasPrefix(p, "/workspace/") }
func (s *Service) FSRead(ctx context.Context, worldID, p string) (string, error) {
	w, e := s.Repo.GetWorld(worldID)
	if e != nil {
		return "", e
	}
	p, e = safePath(p)
	if e != nil {
		return "", e
	}
	h := s.getWorld(worldID, w)
	ph, e := s.Runtime.StartProcess(ctx, h, runtime.ProcessSpec{Argv: []string{"cat", p}, Workdir: "/workspace", Timeout: 10 * time.Second, OutputLimit: 4 << 20})
	if e != nil {
		return "", e
	}
	buf := runtime.NewBuffer(4 << 20)
	go buf.Consume(ph.Stdout)
	<-ph.Done
	b, _ := buf.Read(0, 4<<20)
	return string(b), nil
}
func (s *Service) FSWrite(ctx context.Context, worldID, p, content string) error {
	w, e := s.Repo.GetWorld(worldID)
	if e != nil {
		return e
	}
	p, e = safePath(p)
	if e != nil {
		return e
	}
	h := s.getWorld(worldID, w)
	cmd := []string{"sh", "-c", "mkdir -p $(dirname -- \"$1\") && printf %s \"$2\" | base64 -d > \"$1\"", "sh", p, base64(content)}
	ph, e := s.Runtime.StartProcess(ctx, h, runtime.ProcessSpec{Argv: cmd, Workdir: "/workspace", Timeout: 10 * time.Second, OutputLimit: 1 << 20})
	if e != nil {
		return e
	}
	r := <-ph.Done
	if r.ExitCode != 0 {
		return fmt.Errorf("write failed: %v", r.Err)
	}
	return nil
}
func (s *Service) getWorld(id string, w *domain.World) runtime.WorldHandle {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.worldHandles[id]
	if h.ID == "" {
		h = runtime.WorldHandle{ID: w.ID, Generation: w.Generation, Ref: w.RuntimeRef}
	}
	return h
}
func safePath(p string) (string, error) {
	p = path.Clean(p)
	if !underWorkspace(p) {
		return "", errors.New("path must be under /workspace")
	}
	if strings.Contains(p, "..") && strings.HasPrefix(p, "/workspace/../") {
		return "", errors.New("path traversal")
	}
	return p, nil
}
func base64(s string) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(s); i += 3 {
		a := s[i]
		var b, c byte
		if i+1 < len(s) {
			b = s[i+1]
		}
		if i+2 < len(s) {
			c = s[i+2]
		}
		out.WriteByte(table[a>>2])
		out.WriteByte(table[((a&3)<<4)|(b>>4)])
		if i+1 < len(s) {
			out.WriteByte(table[((b&15)<<2)|(c>>6)])
		} else {
			out.WriteByte('=')
		}
		if i+2 < len(s) {
			out.WriteByte(table[c&63])
		} else {
			out.WriteByte('=')
		}
	}
	return out.String()
}

var _ = os.ErrNotExist

func (s *Service) Reconcile(ctx context.Context) {
	worlds, e := s.Repo.ListRecoverableWorlds()
	if e != nil {
		return
	}
	for _, w := range worlds {
		_ = s.DestroyWorld(ctx, w.ID, w.Generation)
	}
}
