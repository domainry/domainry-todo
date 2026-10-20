package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/domainry/domainry-foundation/logging"
	"github.com/domainry/domainry-foundation/processhost"
	saasservice "github.com/domainry/domainry-todo/internal/assembly/saas"
)

func main() {
	if run() != nil {
		os.Exit(1)
	}
}
func run() (runErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	observability, err := processhost.Open(ctx, processhost.Config{ServiceName: "domainry-todo"})
	if err != nil {
		return err
	}
	started := false
	startupFailure := logging.Failure{Stage: "configuration_validate"}
	defer func() {
		if !started && runErr != nil {
			observability.StartFailed(context.Background(), runErr, startupFailure)
			runErr = errors.Join(runErr, observability.Close(context.Background(), nil))
			return
		}
		runErr = observability.Finish(context.Background(), nil, runErr)
	}()
	runtimeID := strings.TrimSpace(os.Getenv("TODO_RUNTIME_ID"))
	token := strings.TrimSpace(os.Getenv("TODO_SERVICE_ACCESS_TOKEN"))
	databasePath := strings.TrimSpace(os.Getenv("TODO_DATABASE_PATH"))
	address := strings.TrimSpace(os.Getenv("TODO_HTTP_ADDR"))
	if address == "" {
		address = ":8093"
	}
	startupFailure = logging.Failure{Stage: "module_assembly"}
	service, err := saasservice.Open(ctx, saasservice.Options{RuntimeID: runtimeID, ServiceAccessToken: token, DatabasePath: databasePath})
	if err != nil {
		return fmt.Errorf("open Todo SaaS service: %w", err)
	}
	defer service.Close(context.Background())
	server := &http.Server{Addr: address, Handler: observability.Handler("public", service.Handler()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	startupFailure = logging.Failure{Stage: "listener_bind", Dependency: "public_listener"}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	started = true
	observability.Started(ctx, map[string]any{"listener_group": "public"})
	select {
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		observability.Stopping(context.Background(), map[string]any{"reason": "signal"})
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
