// Package saas assembles an independently deployed Todo service.
package saas

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	ormdialect "github.com/domainry/domainry-orm/dialect"
	ormsqlite "github.com/domainry/domainry-orm/sqlite"
	todomodulehost "github.com/domainry/domainry-todo-sdk/modulehost"
	store "github.com/domainry/domainry-todo/internal/infrastructure/persistence/database/todo"
	"github.com/domainry/domainry-todo/internal/infrastructure/persistence/migrationhost"
	saashttp "github.com/domainry/domainry-todo/internal/transport/http/saas"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
)

type Options struct{ RuntimeID, ServiceAccessToken, DatabasePath string }
type Service struct {
	server   *saashttp.Server
	database *sql.DB
}

func Open(ctx context.Context, options Options) (_ *Service, resultErr error) {
	if strings.TrimSpace(options.RuntimeID) == "" || strings.TrimSpace(options.ServiceAccessToken) == "" || strings.TrimSpace(options.DatabasePath) == "" {
		return nil, fmt.Errorf("Todo SaaS configuration is incomplete")
	}
	databasePath, err := filepath.Abs(options.DatabasePath)
	if err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	service := &Service{database: database}
	defer func() {
		if resultErr != nil {
			_ = service.Close(context.Background())
		}
	}()
	database.SetMaxOpenConns(8)
	if _, err = database.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return nil, err
	}
	if _, err = database.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return nil, err
	}
	if _, err = database.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}
	renderer, err := ormdialect.New(ormdialect.SQLite)
	if err != nil {
		return nil, err
	}
	dialect := renderer.WithSchema("")
	registrar := &migrationhost.Registrar{DB: database, Renderer: dialect}
	if err = registrar.Prepare(ctx); err != nil {
		return nil, err
	}
	bridge := saashttp.SourceBridge{}
	source := func(ctx context.Context, _ store.DB, reference string, authority toolsdk.Authority) error {
		return bridge.Authorize(ctx, reference, authority)
	}
	repository, err := store.Open(ctx, database, dialect, ormsqlite.NewProfile(), registrar, source)
	if err != nil {
		return nil, err
	}
	subjects := store.NewSubjectLifecycle(repository, options.RuntimeID)
	server, err := saashttp.New(saashttp.Dependencies{Audience: options.RuntimeID, ServiceAccessToken: options.ServiceAccessToken, Todos: repository, Mutations: repository, Subjects: subjects})
	if err != nil {
		return nil, err
	}
	service.server = server
	return service, nil
}
func (s *Service) Handler() http.Handler {
	if s == nil || s.server == nil {
		return http.NotFoundHandler()
	}
	return s.server.Handler()
}
func (s *Service) Close(context.Context) error {
	if s == nil || s.database == nil {
		return nil
	}
	return s.database.Close()
}

var _ todomodulehost.MigrationRegistrar = (*migrationhost.Registrar)(nil)
