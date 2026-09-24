package todo

import (
	"bytes"
	"database/sql"
	"encoding/json"
	sharedoperation "github.com/domainry/domainry-foundation/operation"
	sharedsubjectlifecycle "github.com/domainry/domainry-foundation/subjectlifecycle"
	lifecyclemodel "github.com/domainry/domainry-lifecycle-sdk/model"
	"github.com/domainry/domainry-orm/dialect"
	"github.com/domainry/domainry-todo-sdk/contract"
	sdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func openLifecycleTodoStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "todo-lifecycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	d, _ := dialect.New(dialect.SQLite)
	operations, err := sharedoperation.SchemaMigrationsForDialect(sharedoperation.AdaptDialect(d.WithSchema("")))
	if err != nil {
		t.Fatal(err)
	}
	shared, err := sharedsubjectlifecycle.SchemaMigrationsForDialect(d.WithSchema(""))
	if err != nil {
		t.Fatal(err)
	}
	owned, err := SchemaMigrations(d.WithSchema(""))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range append(append(operations, shared...), owned...) {
		for _, statement := range m.Statements {
			if _, err = db.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	store, err := NewStore(db, d.WithSchema(""), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return store, db
}

func TestSubjectLifecycleErasesOneOwnerAndReplaysReceipt(t *testing.T) {
	store, db := openLifecycleTodoStore(t)
	a := sdk.Authority{Known: true, RuntimeID: "office", WorkspaceID: "workspace", UserID: "alice"}
	b := a
	b.UserID = "bob"
	mutation := func(key, title string, authority sdk.Authority) Mutation {
		in := Mutation{Key: key, Operation: "todo_create", Data: json.RawMessage(`{"items":[{"title":"` + title + `","timezone":"Asia/Shanghai"}]}`)}
		if _, err := store.ApplyMutation(t.Context(), in, authority); err != nil {
			t.Fatal(err)
		}
		return in
	}
	aliceMutation := mutation("alice-create", "Alice private task", a)
	mutation("bob-create", "Bob private task", b)
	lifecycle := NewSubjectLifecycle(store, a.RuntimeID)
	preview, err := lifecycle.PreviewSubject(t.Context(), a.WorkspaceID, a.UserID)
	if err != nil || !bytes.Contains(preview, []byte(`"todos":1`)) || !bytes.Contains(preview, []byte(`"mutation_receipts":1`)) {
		t.Fatalf("preview=%s err=%v", preview, err)
	}
	exported, err := lifecycle.ExportSubjectForRequest(t.Context(), "export", a.WorkspaceID, a.UserID)
	if err != nil || !bytes.Contains(exported, []byte("Alice private task")) || bytes.Contains(exported, []byte("Bob private task")) {
		t.Fatalf("export=%s err=%v", exported, err)
	}
	if _, err = lifecycle.EraseSubjectForRequest(t.Context(), "erase-held", a.WorkspaceID, a.UserID, []lifecyclemodel.LegalHold{{ID: "hold"}}); err == nil {
		t.Fatal("legal hold did not block Todo erasure")
	}
	receipt, err := lifecycle.EraseSubjectForRequest(t.Context(), "erase-alice", a.WorkspaceID, a.UserID, nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := lifecycle.EraseSubjectForRequest(t.Context(), "erase-alice", a.WorkspaceID, a.UserID, nil)
	if err != nil || !bytes.Equal(receipt, replayed) {
		t.Fatalf("receipt replay=%s err=%v", replayed, err)
	}
	var steps int
	if err = db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM _subject_steps WHERE workspace_id=? AND request_id=? AND owner='todo' AND operation='erase'`, a.WorkspaceID, "erase-alice").Scan(&steps); err != nil || steps != 1 {
		t.Fatalf("shared Todo subject steps=%d err=%v", steps, err)
	}
	if page, err := store.Todos(t.Context(), contract.TodoQuery{}, a); err != nil || len(page.Items) != 0 {
		t.Fatalf("Alice todos=%+v err=%v", page, err)
	}
	if _, found, err := store.MutationReceipt(t.Context(), aliceMutation, a); err != nil || found {
		t.Fatalf("Alice mutation receipt found=%v err=%v", found, err)
	}
	if page, err := store.Todos(t.Context(), contract.TodoQuery{}, b); err != nil || len(page.Items) != 1 || page.Items[0].Title != "Bob private task" {
		t.Fatalf("Bob isolation=%+v err=%v", page, err)
	}
}

func TestStandaloneTodoReceiptAndScope(t *testing.T) {
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "todo.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	d, _ := dialect.New(dialect.SQLite)
	operations, e := sharedoperation.SchemaMigrationsForDialect(sharedoperation.AdaptDialect(d.WithSchema("")))
	if e != nil {
		t.Fatal(e)
	}
	migrations, e := SchemaMigrations(d.WithSchema(""))
	if e != nil {
		t.Fatal(e)
	}
	for _, migration := range append(operations, migrations...) {
		for _, q := range migration.Statements {
			if _, e = db.Exec(q); e != nil {
				t.Fatal(e)
			}
		}
	}
	s, e := NewStore(db, d.WithSchema(""), nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	a := sdk.Authority{Known: true, RuntimeID: "office", WorkspaceID: "w", UserID: "a"}
	in := Mutation{Key: "invocation-1", Operation: "todo_create", Data: json.RawMessage(`{"items":[{"title":"Follow up","timezone":"Asia/Shanghai"}]}`)}
	first, e := s.ApplyMutation(t.Context(), in, a)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := s.ApplyMutation(t.Context(), in, a)
	if e != nil || string(first.Content) != string(retry.Content) {
		t.Fatal("duplicate business effect", e)
	}
	if _, ok, e := s.MutationReceipt(t.Context(), in, a); e != nil || !ok {
		t.Fatal("receipt unavailable", e)
	}
	page, e := s.Todos(t.Context(), contract.TodoQuery{}, a)
	if e != nil || len(page.Items) != 1 {
		t.Fatal(e)
	}
	b := a
	b.UserID = "b"
	if _, e = s.Todo(t.Context(), page.Items[0].ID, b); e == nil {
		t.Fatal("cross-owner read")
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='_agent_conversations'`).Scan(&n)
	if n != 0 {
		t.Fatal("Todo depends on Agent tables")
	}
}
