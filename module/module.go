// Package module is the stable in-process facade for domainry-todo.
// Implementations are private; callers select and assemble capabilities explicitly.
package module

import (
	"github.com/domainry/domainry-foundation/schemaownership"
	migration "github.com/domainry/domainry-orm/migration"
	assembly "github.com/domainry/domainry-todo/internal/assembly/module"
	store "github.com/domainry/domainry-todo/internal/infrastructure/persistence/database/todo"
	modulehost "github.com/domainry/domainry-todo/modulehost"
)

type Dialect = store.Dialect

const MigrationOwner = store.MigrationOwner

type MigrationRegistrar = store.MigrationRegistrar

func SchemaMigrations(d Dialect) ([]migration.Migration, error) {
	return store.SchemaMigrations(d)
}
func SchemaOwnership() []schemaownership.Table { return store.SchemaOwnership() }
func OwnedTables() []string                    { return schemaownership.Names(SchemaOwnership()) }

func NewFactory() modulehost.Factory { return assembly.NewFactory() }
