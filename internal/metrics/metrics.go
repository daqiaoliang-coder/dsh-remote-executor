package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects lightweight execution counters and latency samples.
// It intentionally has zero external dependencies: the /metrics endpoint
// emits a small Prometheus text format without a client library.
type Metrics struct {
	started   atomic.Int64
	completed atomic.Int64
	failed    atomic.Int64
	inflight  atomic.Int64

	mu         sync.Mutex
	samples    []float64 // duration seconds, bounded ring
	maxSamples int
}

func New() *Metrics {
	return &Metrics{maxSamples: 1000}
}

func (m *Metrics) ExecutionStarted(_ string) {
	m.started.Add(1)
	m.inflight.Add(1)
}

func (m *Metrics) ExecutionFinished(_ string, failed bool, d time.Duration) {
	m.completed.Add(1)
	m.inflight.Add(-1)
	if failed {
		m.failed.Add(1)
	}
	m.mu.Lock()
	m.samples = append(m.samples, d.Seconds())
	if len(m.samples) > m.maxSamples {
		m.samples = m.samples[len(m.samples)-m.maxSamples:]
	}
	m.mu.Unlock()
}

type Snapshot struct {
	Started   int64
	Completed int64
	Failed    int64
	Inflight  int64
	P50       float64
	P95       float64
}

func (m *Metrics) Snapshot() Snapshot {
	s := Snapshot{
		Started:   m.started.Load(),
		Completed: m.completed.Load(),
		Failed:    m.failed.Load(),
		Inflight:  m.inflight.Load(),
	}
	m.mu.Lock()
	cp := append([]float64(nil), m.samples...)
	m.mu.Unlock()
	if len(cp) > 0 {
		sort.Float64s(cp)
		s.P50 = quantile(cp, 0.50)
		s.P95 = quantile(cp, 0.95)
	}
	return s
}

func quantile(sorted []float64, q float64) float64 {
	idx := int(q * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	return sorted[idx]
}

// Handler emits Prometheus text format.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s := m.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, "# HELP agent_execution_started_total Total executions started\n# TYPE agent_execution_started_total counter\nagent_execution_started_total %d\n", s.Started)
		fmt.Fprintf(w, "# HELP agent_execution_completed_total Total executions completed\n# TYPE agent_execution_completed_total counter\nagent_execution_completed_total %d\n", s.Completed)
		fmt.Fprintf(w, "# HELP agent_execution_failed_total Total executions failed\n# TYPE agent_execution_failed_total counter\nagent_execution_failed_total %d\n", s.Failed)
		fmt.Fprintf(w, "# HELP agent_execution_inflight Executions currently running\n# TYPE agent_execution_inflight gauge\nagent_execution_inflight %d\n", s.Inflight)
		fmt.Fprintf(w, "# HELP agent_execution_duration_seconds Execution latency quantiles\n# TYPE agent_execution_duration_seconds summary\nagent_execution_duration_seconds{quantile=\"0.5\"} %g\n", s.P50)
		fmt.Fprintf(w, "agent_execution_duration_seconds{quantile=\"0.95\"} %g\n", s.P95)
	})
}
