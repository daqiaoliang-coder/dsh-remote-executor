package repository

import (
	"errors"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/domain"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	CreateWorld(*domain.World) error
	GetWorld(string) (*domain.World, error)
	UpdateWorld(*domain.World) error
	CreateExecution(*domain.Execution) error
	GetExecution(string) (*domain.Execution, error)
	FindExecutionByIdempotency(string, string) (*domain.Execution, error)
	UpdateExecution(*domain.Execution) error
	CreateProcess(*domain.Process) error
	GetProcess(string) (*domain.Process, error)
	UpdateProcess(*domain.Process) error
	ListRecoverableWorlds() ([]*domain.World, error)
}

type Memory struct {
	mu         sync.RWMutex
	worlds     map[string]*domain.World
	executions map[string]*domain.Execution
	idem       map[string]string
	processes  map[string]*domain.Process
}

func NewMemory() *Memory {
	return &Memory{worlds: map[string]*domain.World{}, executions: map[string]*domain.Execution{}, idem: map[string]string{}, processes: map[string]*domain.Process{}}
}
func (m *Memory) CreateWorld(w *domain.World) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.worlds[w.ID] = w
	return nil
}
func (m *Memory) GetWorld(id string) (*domain.World, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.worlds[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *w
	return &c, nil
}
func (m *Memory) UpdateWorld(w *domain.World) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.worlds[w.ID]; !ok {
		return ErrNotFound
	}
	c := *w
	m.worlds[w.ID] = &c
	return nil
}
func (m *Memory) CreateExecution(e *domain.Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.IdempotencyKey != "" {
		k := e.TenantID + "\x00" + e.IdempotencyKey
		if _, ok := m.idem[k]; ok {
			return errors.New("duplicate idempotency key")
		}
		m.idem[k] = e.ID
	}
	m.executions[e.ID] = e
	return nil
}
func (m *Memory) GetExecution(id string) (*domain.Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.executions[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *e
	return &c, nil
}
func (m *Memory) FindExecutionByIdempotency(t, k string) (*domain.Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.idem[t+"\x00"+k]
	if !ok {
		return nil, ErrNotFound
	}
	e := m.executions[id]
	c := *e
	return &c, nil
}
func (m *Memory) UpdateExecution(e *domain.Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.executions[e.ID]; !ok {
		return ErrNotFound
	}
	c := *e
	m.executions[e.ID] = &c
	return nil
}
func (m *Memory) CreateProcess(p *domain.Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processes[p.ID] = p
	return nil
}
func (m *Memory) GetProcess(id string) (*domain.Process, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.processes[id]
	if !ok {
		return nil, ErrNotFound
	}
	c := *p
	return &c, nil
}
func (m *Memory) UpdateProcess(p *domain.Process) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.processes[p.ID]; !ok {
		return ErrNotFound
	}
	c := *p
	m.processes[p.ID] = &c
	return nil
}
func (m *Memory) ListRecoverableWorlds() ([]*domain.World, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*domain.World
	for _, w := range m.worlds {
		if w.Status == domain.WorldReady && w.ExpiresAt.Before(timeNow()) {
			c := *w
			out = append(out, &c)
		}
	}
	return out, nil
}

var timeNow = func() time.Time { return time.Now() }
