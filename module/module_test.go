package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-foundation/schemaownership"
	sharedsubjectlifecycle "github.com/domainry/domainry-foundation/subjectlifecycle"
	ormdialect "github.com/domainry/domainry-orm/dialect"
	ormdriver "github.com/domainry/domainry-orm/driver"
	ormmigration "github.com/domainry/domainry-orm/migration"
	"github.com/domainry/domainry-orm/sqlhost"
	"github.com/domainry/domainry-todo/contract"
	todomodulehost "github.com/domainry/domainry-todo/modulehost"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
)

type registrar struct {
	database *sql.DB
	owners   []string
	applied  map[string]bool
}

type host struct {
	database   *sql.DB
	dialect    todomodulehost.Dialect
	migrations todomodulehost.MigrationRegistrar
}

func (h host) Database() sqlhost.Database                    { return h.database }
func (h host) Dialect() todomodulehost.Dialect               { return h.dialect }
func (host) Profile() ormdriver.Profile                      { return nil }
func (h host) Migrations() todomodulehost.MigrationRegistrar { return h.migrations }

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
	first, err := NewFactory().OpenModule(t.Context(), contract.ApplicationRef{RuntimeID: "runtime"}, host{database: sharedDB, dialect: dialect, migrations: firstRegistrar})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewFactory().OpenModule(t.Context(), contract.ApplicationRef{RuntimeID: "runtime"}, host{database: sharedDB, dialect: dialect, migrations: firstRegistrar})
	if err != nil {
		t.Fatal(err)
	}
	wantOwners := sharedoperation.MigrationOwner + "," + sharedsubjectlifecycle.MigrationOwner + "," + MigrationOwner
	if strings.Join(firstRegistrar.owners, ",") != wantOwners+","+wantOwners {
		t.Fatalf("migration owner calls=%v", firstRegistrar.owners)
	}
	for _, table := range []string{"_agent_user_todos", sharedoperation.TableName, sharedsubjectlifecycle.RequestTableName, sharedsubjectlifecycle.StepTableName} {
		var count int
		if err = sharedDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("shared table %s count=%d err=%v", table, count, err)
		}
	}
	var retired int
	if err = sharedDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='_agent_todo_mutations'`).Scan(&retired); err != nil || retired != 0 {
		t.Fatalf("retired Todo mutation table count=%d err=%v", retired, err)
	}
	authority := toolsdk.Authority{Known: true, RuntimeID: "runtime", WorkspaceID: "workspace", UserID: "alice"}
	mutation := contract.Mutation{Key: "shared-create", Operation: "todo_create", Data: json.RawMessage(`{"items":[{"title":"shared","timezone":"UTC"}]}`)}
	if _, err = first.Mutations().ApplyMutation(t.Context(), mutation, authority); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err = sharedDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM _operations WHERE workspace_id=? AND owner='todo' AND kind='todo.mutation' AND status='succeeded'`, authority.WorkspaceID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("shared Todo operation receipts=%d err=%v", receipts, err)
	}
	if page, readErr := second.Todos().Todos(t.Context(), contract.TodoQuery{}, authority); readErr != nil || len(page.Items) != 1 {
		t.Fatalf("shared module rows=%+v err=%v", page, readErr)
	}

	standaloneDB, standaloneDialect := openDatabase(t, "todo-standalone")
	standaloneRegistrar := &registrar{database: standaloneDB}
	standalone, err := NewFactory().OpenModule(t.Context(), contract.ApplicationRef{RuntimeID: "runtime"}, host{database: standaloneDB, dialect: standaloneDialect, migrations: standaloneRegistrar})
	if err != nil {
		t.Fatal(err)
	}
	if page, readErr := standalone.Todos().Todos(t.Context(), contract.TodoQuery{}, authority); readErr != nil || len(page.Items) != 0 {
		t.Fatalf("standalone database leaked shared rows=%+v err=%v", page, readErr)
	}
	if err = standaloneDB.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM _operations WHERE owner='todo'`).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("standalone database leaked shared operation receipts=%d err=%v", receipts, err)
	}
}

func TestTodoSchemaOwnershipMatchesFreshPhysicalPrimaryKey(t *testing.T) {
	tables := SchemaOwnership()
	if err := schemaownership.ValidateAll(tables); err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || len(OwnedTables()) != 1 || OwnedTables()[0] != tables[0].Name {
		t.Fatalf("Todo ownership=%+v names=%v", tables, OwnedTables())
	}
	database, dialect := openDatabase(t, "todo-ownership")
	registrar := &registrar{database: database}
	if _, err := NewFactory().OpenModule(t.Context(), contract.ApplicationRef{RuntimeID: "runtime"}, host{database: database, dialect: dialect, migrations: registrar}); err != nil {
		t.Fatal(err)
	}
	rows, err := database.QueryContext(t.Context(), `SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, tables[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	primaryKey := []string{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		primaryKey = append(primaryKey, column)
	}
	if strings.Join(primaryKey, ",") != strings.Join(tables[0].PrimaryKey, ",") {
		t.Fatalf("Todo physical primary key=%v ownership=%v", primaryKey, tables[0].PrimaryKey)
	}
}
