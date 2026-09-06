package main

import (
	"context"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/api"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/execution"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/recovery"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/repository"
	"github.com/daqiaoliang-coder/agent-execution-gateway/internal/runtime"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	repo := repository.Repository(repository.NewMemory())
	svc := execution.NewService(repo, runtime.NewDockerRuntime(os.Getenv("EXECUTION_IMAGE")))
	rw := &recovery.Worker{S: svc, Interval: 5 * time.Second}
	ctx, c := context.WithCancel(context.Background())
	defer c()
	go rw.Run(ctx)
	srv := &http.Server{Addr: ":8080", Handler: api.New(svc).Routes()}
	log.Println("agent execution gateway listening on :8080")
	log.Fatal(srv.ListenAndServe())
}
