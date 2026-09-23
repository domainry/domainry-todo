// Package module is the stable in-process facade for domainry-todo.
// Implementations are private; callers select and assemble capabilities explicitly.
package module

import (
	"context"

	driver "github.com/domainry/domainry-orm/driver"
	migration "github.com/domainry/domainry-orm/migration"
	sqlhost "github.com/domainry/domainry-orm/sqlhost"
	contract "github.com/domainry/domainry-todo/contract"
	"github.com/domainry/domainry-todo/internal/domain/todo/service"
	store "github.com/domainry/domainry-todo/internal/infrastructure/persistence/database/todo"
)

type Dialect = store.Dialect
type DB = store.DB
type SourceAuthorizer = store.SourceAuthorizer
type Store = store.Store

const MigrationOwner = store.MigrationOwner

type MigrationRegistrar = store.MigrationRegistrar

// Open installs the canonical shared Subject Lifecycle schema and Todo-owned
// schema through the caller's migration ledger, then constructs a local Store.
// Shared Module databases converge on the same physical tables; standalone
// services invoke the same function against their independent database.
func Open(ctx context.Context, db sqlhost.Database, dialect Dialect, profile driver.Profile, migrations MigrationRegistrar, source SourceAuthorizer) (*Store, error) {
	return store.Open(ctx, db, dialect, profile, migrations, source)
}

func NewStore(db sqlhost.Database, dialect Dialect, profile driver.Profile, source SourceAuthorizer) (*Store, error) {
	return store.NewStore(db, dialect, profile, source)
}

type Mutation = store.Mutation
type MutationResult = store.MutationResult

func SchemaMigrations(d Dialect) ([]migration.Migration, error) {
	return store.SchemaMigrations(d)
}

type SubjectLifecycle = store.SubjectLifecycle

func NewSubjectLifecycle(value *Store, runtimeID string) SubjectLifecycle {
	return store.NewSubjectLifecycle(value, runtimeID)
}
func Validate(in contract.TodoInput) error { return service.Validate(in) }
