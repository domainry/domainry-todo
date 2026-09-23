package todo

import (
	"context"
	"fmt"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	sharedsubjectlifecycle "github.com/domainry/domainry-foundation/subjectlifecycle"
	ormdriver "github.com/domainry/domainry-orm/driver"
	"github.com/domainry/domainry-orm/migration"
	ormschema "github.com/domainry/domainry-orm/schema"
	"github.com/domainry/domainry-orm/sqlhost"
)

const MigrationOwner = "todo"

type MigrationRegistrar interface {
	ApplyOwnedMigrations(context.Context, string, []migration.Migration) error
}

func Open(ctx context.Context, db sqlhost.Database, dialect Dialect, profile ormdriver.Profile, migrations MigrationRegistrar, source SourceAuthorizer) (*Store, error) {
	if migrations == nil {
		return nil, fmt.Errorf("Todo migration registrar is required")
	}
	if _, err := sharedoperation.Open(ctx, db, sharedoperation.AdaptDialect(dialect), migrations); err != nil {
		return nil, err
	}
	sharedDialect, ok := dialect.(sharedsubjectlifecycle.Dialect)
	if !ok {
		return nil, fmt.Errorf("Todo dialect does not support shared Subject Lifecycle persistence")
	}
	if err := sharedsubjectlifecycle.EnsureSchema(ctx, sharedDialect, migrations); err != nil {
		return nil, err
	}
	values, err := SchemaMigrations(dialect)
	if err != nil {
		return nil, err
	}
	if err := migrations.ApplyOwnedMigrations(ctx, MigrationOwner, values); err != nil {
		return nil, fmt.Errorf("apply Todo migrations: %w", err)
	}
	return NewStore(db, dialect, profile, source)
}

// SchemaMigrations is the only source of Todo-owned DDL. A Module host applies
// this history to its shared database under owner "todo"; a standalone Todo
// deployment applies the same history to its own database.
func SchemaMigrations(d Dialect) ([]migration.Migration, error) {
	m := migration.Migration{Version: 1, Name: "todo"}
	statement, _, err := ormschema.NewTable(d, "_agent_user_todos").IfNotExists().Columns(
		required("owner_key", ormschema.TextKey(64)), required("todo_id", ormschema.TextKey(96)), required("batch_id", ormschema.TextKey(96)), required("position", ormschema.BigInt()), required("created_at", ormschema.BigInt()), required("source_conversation_id", ormschema.TextKey(96)), required("status", ormschema.TextKey(16)), required("revision", ormschema.BigInt()), required("payload_json", ormschema.LongText()),
	).PrimaryKey("owner_key", "todo_id").Unique("owner_key", "batch_id", "position").Build()
	if err != nil {
		return nil, err
	}
	m.Statements = append(m.Statements, statement)
	statement, _, err = ormschema.NewIndex(d, "idx_agent_todos_owner_v5", "_agent_user_todos").Columns("owner_key", "created_at", "batch_id", "position").Build()
	if err != nil {
		return nil, err
	}
	m.Statements = append(m.Statements, statement)
	return []migration.Migration{m}, nil
}

func required(name string, kind ormschema.ColumnType) ormschema.ColumnDefinition {
	return ormschema.Column(name, kind).NotNull()
}
