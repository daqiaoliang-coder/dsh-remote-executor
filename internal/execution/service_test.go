package execution

import (
	"context"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/domain"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/repository"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/runtime"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type fake struct{ next int }

func (f *fake) CreateWorld(context.Context, runtime.WorldSpec) (runtime.WorldHandle, error) {
	f.next++
	return runtime.WorldHandle{ID: "w", Generation: 1, Ref: "r"}, nil
}
func (f *fake) DestroyWorld(context.Context, runtime.WorldHandle) error { return nil }
func (f *fake) StartProcess(context.Context, runtime.WorldHandle, runtime.ProcessSpec) (runtime.ProcessHandle, error) {
	f.next++
	done := make(chan runtime.Result, 1)
	done <- runtime.Result{ExitCode: 0, StartedAt: time.Now(), FinishedAt: time.Now()}
	return runtime.ProcessHandle{ID: "p", PID: f.next, ProcessGroup: f.next, Done: done, Stdout: io.NopCloser(strings.NewReader("ok\n")), Stderr: io.NopCloser(strings.NewReader(""))}, nil
}
func (f *fake) KillProcess(context.Context, runtime.ProcessHandle, os.Signal) error { return nil }
func TestWorldAndIdempotency(t *testing.T) {
	s := NewService(repository.NewMemory(), &fake{})
	w, e := s.CreateWorld(context.Background(), "t", "s", domain.ResourceSpec{TimeoutSec: 60})
	if e != nil || w.Status != domain.WorldReady {
		t.Fatal(w, e)
	}
	a, p, e := s.Start(context.Background(), "t", w.ID, "k", domain.CommandSpec{Argv: []string{"echo", "x"}})
	if e != nil || p == nil {
		t.Fatal(e)
	}
	b, p2, e := s.Start(context.Background(), "t", w.ID, "k", domain.CommandSpec{Argv: []string{"echo", "x"}})
	if e != nil || a.ID != b.ID || p2 != nil {
		t.Fatalf("idempotency failed: %#v %#v", a, b)
	}
}
