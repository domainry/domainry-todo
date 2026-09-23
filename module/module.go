// Package module is the stable in-process facade for domainry-todo.
// Implementations are private; callers select and assemble capabilities explicitly.
package module

import (
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

func NewStore(db sqlhost.Database, dialect Dialect, profile driver.Profile, source SourceAuthorizer) (*Store, error) {
	return store.NewStore(db, dialect, profile, source)
}

type Mutation = store.Mutation
type MutationResult = store.MutationResult

func LegacyMigration(d Dialect) (migration.Migration, error) {
	return store.LegacyMigration(d)
}

type SubjectLifecycle = store.SubjectLifecycle

func NewSubjectLifecycle(value *Store, runtimeID string) SubjectLifecycle {
	return store.NewSubjectLifecycle(value, runtimeID)
}
func Validate(in contract.TodoInput) error { return service.Validate(in) }
