package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/domain"
	"time"
)

type MySQL struct{ db *sql.DB }

func NewMySQL(db *sql.DB) *MySQL { return &MySQL{db: db} }
func (r *MySQL) CreateWorld(w *domain.World) error {
	b, _ := json.Marshal(w.Resource)
	_, e := r.db.Exec(`INSERT INTO execution_world(id,generation,tenant_id,session_id,runtime_type,runtime_ref,resource_json,status,expires_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, w.ID, w.Generation, w.TenantID, w.SessionID, w.RuntimeType, w.RuntimeRef, b, w.Status, w.ExpiresAt, w.CreatedAt, w.UpdatedAt)
	return e
}
func (r *MySQL) GetWorld(id string) (*domain.World, error) {
	var w domain.World
	var b []byte
	err := r.db.QueryRow(`SELECT id,generation,tenant_id,session_id,runtime_type,COALESCE(runtime_ref,''),resource_json,status,expires_at,created_at,updated_at FROM execution_world WHERE id=?`, id).Scan(&w.ID, &w.Generation, &w.TenantID, &w.SessionID, &w.RuntimeType, &w.RuntimeRef, &b, &w.Status, &w.ExpiresAt, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &w.Resource); err != nil {
		return nil, err
	}
	return &w, nil
}
func (r *MySQL) UpdateWorld(w *domain.World) error {
	res, e := r.db.Exec(`UPDATE execution_world SET status=?,runtime_ref=?,expires_at=?,updated_at=? WHERE id=? AND generation=?`, w.Status, w.RuntimeRef, w.ExpiresAt, w.UpdatedAt, w.ID, w.Generation)
	if e == nil {
		n, _ := res.RowsAffected()
		if n == 0 {
			return ErrNotFound
		}
	}
	return e
}
func (r *MySQL) CreateExecution(e *domain.Execution) error {
	a, _ := json.Marshal(e.Command.Argv)
	env, _ := json.Marshal(e.Command.Env)
	_, err := r.db.Exec(`INSERT INTO execution(id,world_id,tenant_id,session_id,idempotency_key,status,attempt,argv_json,workdir,env_json,timeout_sec,output_limit,started_at,finished_at,exit_code,termination_reason,lease_version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.WorldID, e.TenantID, e.SessionID, nullIfEmpty(e.IdempotencyKey), e.Status, e.Attempt, a, e.Command.Workdir, env, e.Command.TimeoutSec, e.Command.OutputLimit, e.StartedAt, e.FinishedAt, e.ExitCode, nullIfEmpty(e.TerminationReason), e.LeaseVersion, e.CreatedAt, e.UpdatedAt)
	return err
}
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (r *MySQL) GetExecution(id string) (*domain.Execution, error) {
	var e domain.Execution
	var a, env []byte
	var idem, term sql.NullString
	err := r.db.QueryRow(`SELECT id,world_id,tenant_id,session_id,idempotency_key,status,attempt,argv_json,workdir,env_json,timeout_sec,output_limit,started_at,finished_at,exit_code,termination_reason,lease_version,created_at,updated_at FROM execution WHERE id=?`, id).Scan(&e.ID, &e.WorldID, &e.TenantID, &e.SessionID, &idem, &e.Status, &e.Attempt, &a, &e.Command.Workdir, &env, &e.Command.TimeoutSec, &e.Command.OutputLimit, &e.StartedAt, &e.FinishedAt, &e.ExitCode, &term, &e.LeaseVersion, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.IdempotencyKey = idem.String
	e.TerminationReason = term.String
	json.Unmarshal(a, &e.Command.Argv)
	json.Unmarshal(env, &e.Command.Env)
	return &e, nil
}
func (r *MySQL) FindExecutionByIdempotency(t, k string) (*domain.Execution, error) {
	var id string
	err := r.db.QueryRow(`SELECT id FROM execution WHERE tenant_id=? AND idempotency_key=?`, t, k).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetExecution(id)
}
func (r *MySQL) UpdateExecution(e *domain.Execution) error {
	_, err := r.db.Exec(`UPDATE execution SET status=?,attempt=?,started_at=?,finished_at=?,exit_code=?,termination_reason=?,lease_version=?,updated_at=? WHERE id=?`, e.Status, e.Attempt, e.StartedAt, e.FinishedAt, e.ExitCode, nullIfEmpty(e.TerminationReason), e.LeaseVersion, e.UpdatedAt, e.ID)
	return err
}
func (r *MySQL) CreateProcess(p *domain.Process) error {
	_, e := r.db.Exec(`INSERT INTO process(id,execution_id,world_id,worker_id,pid,process_group,status,started_at,finished_at,exit_code,signal) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.ExecutionID, p.WorldID, p.WorkerID, p.PID, p.ProcessGroup, p.Status, p.StartedAt, p.FinishedAt, p.ExitCode, nullIfEmpty(p.Signal))
	return e
}
func (r *MySQL) GetProcess(id string) (*domain.Process, error) {
	var p domain.Process
	var sig sql.NullString
	err := r.db.QueryRow(`SELECT id,execution_id,world_id,worker_id,pid,process_group,status,started_at,finished_at,exit_code,signal FROM process WHERE id=?`, id).Scan(&p.ID, &p.ExecutionID, &p.WorldID, &p.WorkerID, &p.PID, &p.ProcessGroup, &p.Status, &p.StartedAt, &p.FinishedAt, &p.ExitCode, &sig)
	p.Signal = sig.String
	return &p, err
}
func (r *MySQL) UpdateProcess(p *domain.Process) error {
	_, e := r.db.Exec(`UPDATE process SET status=?,finished_at=?,exit_code=?,signal=? WHERE id=?`, p.Status, p.FinishedAt, p.ExitCode, nullIfEmpty(p.Signal), p.ID)
	return e
}
func (r *MySQL) ListRecoverableWorlds() ([]*domain.World, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, e := r.db.QueryContext(ctx, `SELECT id,generation,tenant_id,session_id,runtime_type,COALESCE(runtime_ref,''),resource_json,status,expires_at,created_at,updated_at FROM execution_world WHERE status IN ('READY','TERMINATING') AND expires_at < NOW(6)`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []*domain.World
	for rows.Next() {
		var w domain.World
		var b []byte
		if e = rows.Scan(&w.ID, &w.Generation, &w.TenantID, &w.SessionID, &w.RuntimeType, &w.RuntimeRef, &b, &w.Status, &w.ExpiresAt, &w.CreatedAt, &w.UpdatedAt); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, &w.Resource); e != nil {
			return nil, e
		}
		out = append(out, &w)
	}
	return out, rows.Err()
}
