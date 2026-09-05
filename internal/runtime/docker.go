package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type DockerRuntime struct {
	Image string
	Grace time.Duration
	mu    sync.Mutex
}

func NewDockerRuntime(image string) *DockerRuntime {
	if image == "" {
		image = "alpine:3.20"
	}
	return &DockerRuntime{Image: image, Grace: 2 * time.Second}
}
func (d *DockerRuntime) CreateWorld(ctx context.Context, s WorldSpec) (WorldHandle, error) {
	args := []string{"run", "-d", "--name", s.ID, "--init", "--network", "none", "--pids-limit", strconv.Itoa(max(s.PIDs, 16)), "--memory", fmt.Sprintf("%dm", max64(s.MemoryMB, 64)), "--cpus", fmt.Sprintf("%d", max(s.ResourceCPU, 1)), "-w", "/workspace", d.Image, "sh", "-lc", "mkdir -p /workspace && sleep 365d"}
	out, e := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if e != nil {
		return WorldHandle{}, fmt.Errorf("docker run: %w: %s", e, out)
	}
	return WorldHandle{ID: s.ID, Generation: 1, Ref: strings.TrimSpace(string(out))}, nil
}
func (d *DockerRuntime) DestroyWorld(ctx context.Context, w WorldHandle) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", w.RefOrID())
	return cmd.Run()
}
func (d *DockerRuntime) StartProcess(ctx context.Context, w WorldHandle, s ProcessSpec) (ProcessHandle, error) {
	if len(s.Argv) == 0 {
		return ProcessHandle{}, errors.New("empty argv")
	}
	args := []string{"exec", "-i"}
	if s.Workdir != "" {
		args = append(args, "-w", s.Workdir)
	}
	for k, v := range s.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, w.RefOrID())
	args = append(args, s.Argv...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, e := cmd.StdoutPipe()
	if e != nil {
		return ProcessHandle{}, e
	}
	stderr, e := cmd.StderrPipe()
	if e != nil {
		return ProcessHandle{}, e
	}
	started := time.Now()
	if e = cmd.Start(); e != nil {
		return ProcessHandle{}, e
	}
	done := make(chan Result, 1)
	go func() {
		err := cmd.Wait()
		r := Result{StartedAt: started, FinishedAt: time.Now(), Err: err}
		if cmd.ProcessState != nil {
			r.ExitCode = cmd.ProcessState.ExitCode()
		}
		done <- r
		close(done)
	}()
	h := ProcessHandle{ID: fmt.Sprintf("p-%d", time.Now().UnixNano()), PID: cmd.Process.Pid, ProcessGroup: cmd.Process.Pid, Stdout: stdout, Stderr: stderr, Done: done}
	if s.Timeout > 0 {
		go func() {
			select {
			case <-time.After(s.Timeout):
				_ = d.KillProcess(context.Background(), h, syscall.SIGTERM)
			case <-h.Done:
			}
		}()
	}
	return h, nil
}
func (d *DockerRuntime) KillProcess(ctx context.Context, h ProcessHandle, s os.Signal) error {
	sig, ok := s.(syscall.Signal)
	if !ok {
		sig = syscall.SIGTERM
	}
	_ = syscall.Kill(-h.ProcessGroup, sig)
	if sig == syscall.SIGTERM {
		select {
		case <-h.Done:
		case <-time.After(d.Grace):
			_ = syscall.Kill(-h.ProcessGroup, syscall.SIGKILL)
		}
	}
	return nil
}
func (w WorldHandle) RefOrID() string {
	if w.Ref != "" {
		return w.Ref
	}
	return w.ID
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Drain copies a stream into a bounded byte buffer. It intentionally never grows beyond limit.
type Buffer struct {
	mu     sync.RWMutex
	data   []byte
	limit  int64
	total  int64
	closed bool
}

func NewBuffer(limit int64) *Buffer {
	if limit <= 0 {
		limit = 1 << 20
	}
	return &Buffer{limit: limit}
}
func (b *Buffer) Consume(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		chunk, e := br.ReadBytes('\n')
		b.mu.Lock()
		b.total += int64(len(chunk))
		if int64(len(b.data)) < b.limit {
			remain := b.limit - int64(len(b.data))
			if int64(len(chunk)) > remain {
				chunk = chunk[:remain]
			}
			b.data = append(b.data, chunk...)
		}
		b.mu.Unlock()
		if e != nil {
			b.mu.Lock()
			b.closed = true
			b.mu.Unlock()
			return
		}
	}
}
func (b *Buffer) Read(offset, limit int64) ([]byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if offset < 0 {
		offset = 0
	}
	if offset >= int64(len(b.data)) {
		return nil, b.closed
	}
	end := int64(len(b.data))
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return append([]byte(nil), b.data[offset:end]...), end == int64(len(b.data)) && b.closed
}
func (b *Buffer) Total() int64 { b.mu.RLock(); defer b.mu.RUnlock(); return b.total }
