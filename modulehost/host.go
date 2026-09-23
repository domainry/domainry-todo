// Package modulehost defines the narrow infrastructure ports used to embed
// Todo. It intentionally exposes capabilities and a binding, never Todo's
// concrete Store.
package modulehost

import (
	"context"
	"database/sql"

	lifecyclecontract "github.com/domainry/domainry-lifecycle-sdk/contract"
	ormdriver "github.com/domainry/domainry-orm/driver"
	"github.com/domainry/domainry-orm/migration"
	"github.com/domainry/domainry-orm/sqlhost"
	todocontract "github.com/domainry/domainry-todo/contract"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Dialect interface {
	Identifier(string) string
	Table(string) string
	Placeholder(int) string
}

type MigrationRegistrar interface {
	ApplyOwnedMigrations(context.Context, string, []migration.Migration) error
}

type Host interface {
	Database() sqlhost.Database
	Dialect() Dialect
	Profile() ormdriver.Profile
	Migrations() MigrationRegistrar
}

// SourceAuthorizer is an optional host capability. It verifies an Agent-owned
// source reference without allowing Todo to read Agent tables itself.
type SourceAuthorizer interface {
	AuthorizeTodoSource(context.Context, DB, string, toolsdk.Authority) error
}

type TransactionalMutator interface {
	ApplyMutation(context.Context, todocontract.Mutation, toolsdk.Authority) (todocontract.MutationResult, error)
	ApplyInTransaction(context.Context, *sql.Tx, todocontract.Mutation, toolsdk.Authority) (todocontract.MutationResult, error)
	MutationReceipt(context.Context, todocontract.Mutation, toolsdk.Authority) (todocontract.MutationResult, bool, error)
}

type ModuleBinding interface {
	Todos() todocontract.TodoService
	Mutations() TransactionalMutator
	Validate(todocontract.TodoInput) error
	SubjectLifecycle() lifecyclecontract.SubjectExecutionHandler
	Close(context.Context) error
}

type Factory interface {
	OpenModule(context.Context, todocontract.ApplicationRef, Host) (ModuleBinding, error)
}
