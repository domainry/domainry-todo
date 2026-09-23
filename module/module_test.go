package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	sharedsubjectlifecycle "github.com/domainry/domainry-foundation/subjectlifecycle"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	ormmigration "github.com/domainry/domainry-orm/migration"
	"github.com/domainry/domainry-todo/contract"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
)

type registrar struct {
	database *sql.DB
	owners   []string
	applied  map[string]bool
}

func (r *registrar) ApplyOwnedMigrations(ctx context.Context, owner string, migrations []ormmigration.Migration) error {
	r.owners = append(r.owners, owner)
	if r.applied == nil {
		r.applied = map[string]bool{}
	}
	for _, migration := range migrations {
		key := fmt.Sprintf("%s/%d", owner, migration.Version)
		if r.applied[key] {
			continue
		}
		for _, statement := range migration.Statements {
			if _, err := r.database.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		r.applied[key] = true
	}
	return nil
}

func openDatabase(t *testing.T, name string) (*sql.DB, Dialect) {
	t.Helper()
	database, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	dialect, err := ormdialect.New(ormdialect.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	return database, dialect.WithSchema("")
}

func TestOpenUsesTodoAndSharedSubjectMigrationsAcrossDeploymentTopologies(t *testing.T) {
	sharedDB, dialect := openDatabase(t, "todo-shared")
	firstRegistrar := &registrar{database: sharedDB}
	first, err := Open(t.Context(), sharedDB, dialect, nil, firstRegistrar, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(t.Context(), sharedDB, dialect, nil, firstRegistrar, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantOwners := sharedsubjectlifecycle.MigrationOwner + "," + MigrationOwner
	if strings.Join(firstRegistrar.owners, ",") != wantOwners+","+wantOwners {
		t.Fatalf("migration owner calls=%v", firstRegistrar.owners)
	}
	for _, table := range []string{"_agent_user_todos", "_agent_todo_mutations", sharedsubjectlifecycle.RequestTableName, sharedsubjectlifecycle.StepTableName} {
		var count int
		if err = sharedDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("shared table %s count=%d err=%v", table, count, err)
		}
	}
	authority := toolsdk.Authority{Known: true, RuntimeID: "runtime", WorkspaceID: "workspace", UserID: "alice"}
	mutation := Mutation{Key: "shared-create", Operation: "todo_create", Data: json.RawMessage(`{"items":[{"title":"shared","timezone":"UTC"}]}`)}
	if _, err = first.ApplyMutation(t.Context(), mutation, authority); err != nil {
		t.Fatal(err)
	}
	if page, readErr := second.Todos(t.Context(), contract.TodoQuery{}, authority); readErr != nil || len(page.Items) != 1 {
		t.Fatalf("shared module rows=%+v err=%v", page, readErr)
	}

	standaloneDB, standaloneDialect := openDatabase(t, "todo-standalone")
	standaloneRegistrar := &registrar{database: standaloneDB}
	standalone, err := Open(t.Context(), standaloneDB, standaloneDialect, nil, standaloneRegistrar, nil)
	if err != nil {
		t.Fatal(err)
	}
	if page, readErr := standalone.Todos(t.Context(), contract.TodoQuery{}, authority); readErr != nil || len(page.Items) != 0 {
		t.Fatalf("standalone database leaked shared rows=%+v err=%v", page, readErr)
	}
}
