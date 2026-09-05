package lease

import (
	"context"
	"sync"
	"time"
)

type Manager struct {
	mu     sync.Mutex
	leases map[string]Lease
	TTL    time.Duration
}
type Lease struct {
	Worker    string
	Version   int64
	ExpiresAt time.Time
}

func New() *Manager { return &Manager{leases: map[string]Lease{}, TTL: 10 * time.Second} }
func (m *Manager) Acquire(ctx context.Context, world, worker string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if x, ok := m.leases[world]; ok && time.Now().Before(x.ExpiresAt) {
		return 0, false, nil
	}
	var v int64
	if x, ok := m.leases[world]; ok {
		v = x.Version + 1
	}
	m.leases[world] = Lease{Worker: worker, Version: v, ExpiresAt: time.Now().Add(m.TTL)}
	return v, true, nil
}
func (m *Manager) Renew(ctx context.Context, world, worker string, version int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	x, ok := m.leases[world]
	if !ok || x.Worker != worker || x.Version != version {
		return context.Canceled
	}
	x.ExpiresAt = time.Now().Add(m.TTL)
	m.leases[world] = x
	return nil
}
