package todo

import (
	"database/sql"
	"encoding/json"
	"github.com/domainry/domainry-orm/dialect"
	"github.com/domainry/domainry-todo/contract"
	sdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestStandaloneTodoReceiptAndScope(t *testing.T) {
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "todo.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	d, _ := dialect.New(dialect.SQLite)
	migration, e := LegacyMigration(d.WithSchema(""))
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range migration.Statements {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
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
