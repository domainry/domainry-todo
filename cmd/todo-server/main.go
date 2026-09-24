package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	saasservice "github.com/domainry/domainry-todo/internal/assembly/saas"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "domainry-todo:", err)
		os.Exit(1)
	}
}
func run() error {
	runtimeID := strings.TrimSpace(os.Getenv("TODO_RUNTIME_ID"))
	token := strings.TrimSpace(os.Getenv("TODO_SERVICE_ACCESS_TOKEN"))
	databasePath := strings.TrimSpace(os.Getenv("TODO_DATABASE_PATH"))
	address := strings.TrimSpace(os.Getenv("TODO_HTTP_ADDR"))
	if address == "" {
		address = ":8093"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := saasservice.Open(ctx, saasservice.Options{RuntimeID: runtimeID, ServiceAccessToken: token, DatabasePath: databasePath})
	if err != nil {
		return fmt.Errorf("open Todo SaaS service: %w", err)
	}
	defer service.Close(context.Background())
	server := &http.Server{Addr: address, Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	select {
	case err = <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
