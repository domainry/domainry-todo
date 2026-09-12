package todo

import (
	"github.com/domainry/domainry-orm/migration"
	ormschema "github.com/domainry/domainry-orm/schema"
)

func LegacyMigration(d Dialect) (migration.Migration, error) {
	m := migration.Migration{Version: 5, Name: "agent_personal_todos"}
	statement, _, err := ormschema.NewTable(d, "_agent_user_todos").IfNotExists().Columns(
		required("owner_key", ormschema.TextKey(64)), required("todo_id", ormschema.TextKey(96)), required("batch_id", ormschema.TextKey(96)), required("position", ormschema.BigInt()), required("created_at", ormschema.BigInt()), required("source_conversation_id", ormschema.TextKey(96)), required("status", ormschema.TextKey(16)), required("revision", ormschema.BigInt()), required("payload_json", ormschema.LongText()),
	).PrimaryKey("owner_key", "todo_id").Unique("owner_key", "batch_id", "position").Build()
	if err != nil {
		return m, err
	}
	m.Statements = append(m.Statements, statement)
	statement, _, err = ormschema.NewIndex(d, "idx_agent_todos_owner_v5", "_agent_user_todos").Columns("owner_key", "created_at", "batch_id", "position").Build()
	if err != nil {
		return m, err
	}
	m.Statements = append(m.Statements, statement)
	statement, _, err = ormschema.NewTable(d, "_agent_todo_mutations").IfNotExists().Columns(required("owner_key", ormschema.TextKey(64)), required("client_key", ormschema.TextKey(64)), required("created_at", ormschema.BigInt()), required("payload_json", ormschema.LongText())).PrimaryKey("owner_key", "client_key").Build()
	if err != nil {
		return m, err
	}
	m.Statements = append(m.Statements, statement)
	return m, nil
}

func SubjectLifecycleMigration(d Dialect) (migration.Migration, error) {
	statement, _, err := ormschema.NewTable(d, subjectReceiptTable).IfNotExists().Columns(
		required("owner_key", ormschema.TextKey(64)), required("request_id", ormschema.TextKey(96)),
		required("payload_json", ormschema.LongText()),
	).PrimaryKey("owner_key", "request_id").Build()
	return migration.Migration{Version: 2, Name: "todo_subject_erasure_receipts", Statements: []string{statement}}, err
}

func required(name string, kind ormschema.ColumnType) ormschema.ColumnDefinition {
	return ormschema.Column(name, kind).NotNull()
}
