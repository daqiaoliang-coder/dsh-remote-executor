package recovery

import (
	"context"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/execution"
	"time"
)

type Worker struct {
	S        *execution.Service
	Interval time.Duration
}

func (w *Worker) Run(ctx context.Context) {
	if w.Interval <= 0 {
		w.Interval = 5 * time.Second
	}
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.S.Reconcile(ctx)
		}
	}
}
