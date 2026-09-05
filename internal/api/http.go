package api

import (
	"context"
	"encoding/json"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/domain"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/execution"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct{ S *execution.Service }

func New(s *execution.Service) *Server { return &Server{S: s} }
func (s *Server) Routes() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("/v1/worlds", s.worlds)
	m.HandleFunc("/v1/worlds/", s.worldSub)
	m.HandleFunc("/v1/executions/", s.execution)
	m.HandleFunc("/v1/processes/", s.process)
	return m
}
func tenant(r *http.Request) string {
	t := r.Header.Get("X-Tenant-ID")
	if t == "" {
		t = "default"
	}
	return t
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func (s *Server) worlds(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req struct {
		SessionID string              `json:"session_id"`
		Resource  domain.ResourceSpec `json:"resource"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		write(w, 400, map[string]string{"error": "bad json"})
		return
	}
	ctx, c := timeout(r, 30*time.Second)
	defer c()
	out, e := s.S.CreateWorld(ctx, tenant(r), req.SessionID, req.Resource)
	if e != nil {
		write(w, 500, map[string]string{"error": e.Error()})
		return
	}
	write(w, 200, out)
}
func (s *Server) worldSub(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(404)
		return
	}
	id := parts[2]
	if len(parts) >= 4 && parts[3] == "executions" {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		var req struct {
			Argv        []string          `json:"argv"`
			Workdir     string            `json:"workdir"`
			Env         map[string]string `json:"env"`
			TimeoutSec  int               `json:"timeout_sec"`
			OutputLimit int64             `json:"output_limit"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			write(w, 400, map[string]string{"error": "bad json"})
			return
		}
		ex, p, e := s.S.Start(r.Context(), tenant(r), id, r.Header.Get("Idempotency-Key"), domain.CommandSpec{Argv: req.Argv, Workdir: req.Workdir, Env: req.Env, TimeoutSec: req.TimeoutSec, OutputLimit: req.OutputLimit})
		if e != nil {
			write(w, 500, map[string]string{"error": e.Error(), "execution_id": exID(ex)})
			return
		}
		write(w, 200, map[string]string{"execution_id": ex.ID, "process_id": p.ID})
		return
	}
	if len(parts) >= 4 && parts[3] == "fs" {
		s.fs(w, r, id, parts[4:])
		return
	}
	if r.Method == "DELETE" {
		g, _ := strconv.ParseInt(r.URL.Query().Get("generation"), 10, 64)
		e := s.S.DestroyWorld(r.Context(), id, g)
		if e != nil {
			write(w, 500, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, map[string]any{"ok": true})
		return
	}
	w.WriteHeader(404)
}
func exID(e *domain.Execution) string {
	if e == nil {
		return ""
	}
	return e.ID
}
func (s *Server) execution(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/executions/")
	e, err := s.S.GetExecution(id)
	if err != nil {
		write(w, 404, map[string]string{"error": err.Error()})
		return
	}
	write(w, 200, e)
}
func (s *Server) process(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(404)
		return
	}
	id := parts[2]
	if len(parts) >= 4 && parts[3] == "output" {
		stream := r.URL.Query().Get("stream")
		off, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		lim, _ := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
		d, eof, err := s.S.ReadOutput(id, stream, off, lim)
		if err != nil {
			write(w, 404, map[string]string{"error": err.Error()})
			return
		}
		write(w, 200, map[string]any{"offset": off, "data": string(d), "eof": eof})
		return
	}
	if len(parts) >= 4 && parts[3] == "kill" && r.Method == "POST" {
		if err := s.S.Kill(r.Context(), id); err != nil {
			write(w, 500, map[string]string{"error": err.Error()})
			return
		}
		write(w, 200, map[string]any{"ok": true})
		return
	}
	p, e := s.S.GetProcess(id)
	if e != nil {
		write(w, 404, map[string]string{"error": e.Error()})
		return
	}
	write(w, 200, p)
}
func (s *Server) fs(w http.ResponseWriter, r *http.Request, id string, rest []string) {
	if len(rest) == 0 {
		w.WriteHeader(404)
		return
	}
	switch rest[0] {
	case "read":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		v, e := s.S.FSRead(r.Context(), id, r.URL.Query().Get("path"))
		if e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, map[string]string{"content": v})
	case "write":
		if r.Method != "PUT" {
			w.WriteHeader(405)
			return
		}
		var q struct{ Path, Content string }
		if json.NewDecoder(r.Body).Decode(&q) != nil {
			write(w, 400, map[string]string{"error": "bad json"})
			return
		}
		if e := s.S.FSWrite(r.Context(), id, q.Path, q.Content); e != nil {
			write(w, 400, map[string]string{"error": e.Error()})
			return
		}
		write(w, 200, map[string]any{"ok": true})
	default:
		w.WriteHeader(404)
	}
}
func timeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
