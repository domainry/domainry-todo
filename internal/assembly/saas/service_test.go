package saas

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	todocontract "github.com/domainry/domainry-todo-sdk/contract"
	todoremote "github.com/domainry/domainry-todo-sdk/remote"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	_ "modernc.org/sqlite"
)

func TestRemoteTodoOwnsDatabaseReplaysReceiptAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	databasePath := filepath.Join(root, "todo.db")
	authority := toolsdk.Authority{Known: true, RuntimeID: "runtime-a", WorkspaceID: "workspace-a", UserID: "user-a"}
	sourceChecks := 0
	open := func() (*Service, *httptest.Server, todocontract.TodoService, todocontract.MutationService) {
		service, err := Open(t.Context(), Options{RuntimeID: "runtime-a", ServiceAccessToken: "secret-token", DatabasePath: databasePath})
		if err != nil {
			t.Fatal(err)
		}
		httpServer := httptest.NewServer(service.Handler())
		binding, err := todoremote.NewFactory(todoremote.Config{Endpoint: httpServer.URL, ServiceAccessToken: "secret-token", AuthorizeSource: func(_ context.Context, reference string, a toolsdk.Authority) error {
			if reference != "conversation-a" || a != authority {
				t.Fatalf("source=%s authority=%+v", reference, a)
			}
			sourceChecks++
			return nil
		}}).OpenSaaS(t.Context(), todocontract.ApplicationRef{RuntimeID: "runtime-a"})
		if err != nil {
			t.Fatal(err)
		}
		return service, httpServer, binding.Todos(), binding.Mutations()
	}
	service, httpServer, todos, mutations := open()
	data, _ := json.Marshal(map[string]any{"items": []todocontract.TodoInput{{Title: "Remote", Timezone: "UTC"}}})
	mutation := todocontract.Mutation{Key: "tool-call-a", Operation: "todo_create", Data: data, SourceConversationID: "conversation-a", SourceRunID: "run-a"}
	result, err := mutations.ApplyMutation(t.Context(), mutation, authority)
	if err != nil || result.ResourceID == "" || sourceChecks == 0 {
		t.Fatalf("result=%+v checks=%d err=%v", result, sourceChecks, err)
	}
	receipt, found, err := mutations.MutationReceipt(t.Context(), mutation, authority)
	if err != nil || !found || receipt.ResourceID != result.ResourceID {
		t.Fatalf("receipt=%+v found=%v err=%v", receipt, found, err)
	}
	httpServer.Close()
	if err = service.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	service, httpServer, todos, _ = open()
	defer httpServer.Close()
	defer service.Close(t.Context())
	page, err := todos.Todos(t.Context(), todocontract.TodoQuery{Limit: 10}, authority)
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Remote" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var agentConversationTables int
	if err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='_agent_conversations'`).Scan(&agentConversationTables); err != nil || agentConversationTables != 0 {
		t.Fatalf("Agent conversation tables=%d err=%v", agentConversationTables, err)
	}
	var todoTables int
	if err = database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='_agent_user_todos'`).Scan(&todoTables); err != nil || todoTables != 1 {
		t.Fatalf("Todo tables=%d err=%v", todoTables, err)
	}
}
