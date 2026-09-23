package todo

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-orm/query"
	contract "github.com/domainry/domainry-todo/contract"
	"github.com/domainry/domainry-todo/internal/domain/todo/service"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

const todoTable = "_agent_user_todos"

func todoScope(a toolsdk.Authority, id string) query.Predicate {
	return query.And(query.Equal("owner_key", todoOwner(a)), query.Equal("todo_id", id))
}

func (s *Store) Todo(ctx context.Context, id string, a toolsdk.Authority) (contract.Todo, error) {
	if err := todoAuthority(a); err != nil {
		return contract.Todo{}, err
	}
	return s.todo(ctx, s.store.Database(), id, a)
}
func (s *Store) todo(ctx context.Context, db DB, id string, a toolsdk.Authority) (contract.Todo, error) {
	var out contract.Todo
	if !validKey(id) {
		return out, todoError("bad_request", "todo_invalid")
	}
	found, err := s.readPayload(ctx, db, todoTable, todoScope(a, id), &out)
	if err == nil && !found {
		err = todoError("not_found", "todo_not_found")
	}
	return out, err
}

type todoCursor struct {
	Owner, Query, Batch string
	Created, Cutoff     int64
	Position            int
}

func (s *Store) Todos(ctx context.Context, in contract.TodoQuery, a toolsdk.Authority) (contract.TodoPage, error) {
	out := contract.TodoPage{Items: []contract.Todo{}, Complete: true}
	if err := todoAuthority(a); err != nil {
		return out, err
	}
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Status == "" {
		in.Status = "all"
	}
	if !validText(in.Query, 256, false) || len(in.Cursor) > 2048 || in.Limit < 1 || in.Limit > 50 || in.Status != "all" && in.Status != "open" && in.Status != "completed" || in.BatchID != "" && !validKey(in.BatchID) || in.SourceConversationID != "" && !validKey(in.SourceConversationID) {
		return out, todoError("bad_request", "todo_query_invalid")
	}
	key := in
	key.Cursor = ""
	owner, hash := todoOwner(a), todoHash(key)
	cursor := todoCursor{Owner: owner, Query: hash, Cutoff: time.Now().UnixMilli()}
	filters := []query.Predicate{query.Equal("owner_key", owner)}
	if in.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.Owner != owner || cursor.Query != hash || !validKey(cursor.Batch) || cursor.Position < 1 || cursor.Created < 1 || cursor.Cutoff < cursor.Created {
			return out, todoError("bad_request", "todo_cursor_invalid")
		}
		filters = append(filters, query.Or(query.LessThan("created_at", cursor.Created), query.And(query.Equal("created_at", cursor.Created), query.LessThan("batch_id", cursor.Batch)), query.And(query.Equal("created_at", cursor.Created), query.Equal("batch_id", cursor.Batch), query.GreaterThan("position", cursor.Position))))
	}
	filters = append(filters, query.LessThanOrEqual("created_at", cursor.Cutoff))
	if in.Status != "all" {
		filters = append(filters, query.Equal("status", in.Status))
	}
	if in.BatchID != "" {
		filters = append(filters, query.Equal("batch_id", in.BatchID))
	}
	if in.SourceConversationID != "" {
		filters = append(filters, query.Equal("source_conversation_id", in.SourceConversationID))
	}
	statement, args, err := query.NewSelectBuilder(s.store.Renderer(), todoTable).Columns("payload_json").Where(query.And(filters...)).OrderBy(query.Descending("created_at"), query.Descending("batch_id"), query.Ascending("position")).Limit(301).Build()
	if err != nil {
		return out, err
	}
	rows, err := s.store.Database().QueryContext(ctx, statement, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	scanned, size := 0, 0
	needle := strings.ToLower(in.Query)
	for rows.Next() {
		if scanned >= 300 || len(out.Items) >= in.Limit {
			out.Complete = false
			break
		}
		var raw []byte
		var item contract.Todo
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return out, err
		}
		matches := strings.Contains(strings.ToLower(item.Title+"\n"+item.Description), needle)
		if matches && size+len(raw) > 24576 && len(out.Items) > 0 {
			out.Complete = false
			break
		}
		scanned++
		cursor.Created, cursor.Batch, cursor.Position = item.CreatedAt.UnixMilli(), item.BatchID, item.Position
		if matches {
			out.Items = append(out.Items, item)
			size += len(raw)
		}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if !out.Complete {
		out.NextCursor = base64.RawURLEncoding.EncodeToString(todoJSON(cursor))
	}
	return out, nil
}

func (s *Store) createTodos(ctx context.Context, tx *sql.Tx, items []contract.TodoInput, source, run, key string, a toolsdk.Authority) (contract.TodoBatch, error) {
	out := contract.TodoBatch{BatchID: "tb_" + todoHash([]string{todoOwner(a), key})[:32], Items: []contract.Todo{}}
	if len(items) < 1 || len(items) > 20 {
		return out, todoError("bad_request", "todo_batch_invalid")
	}
	for _, item := range items {
		if err := service.Validate(item); err != nil {
			return out, err
		}
	}
	if source != "" {
		if err := s.authorizeSource(ctx, tx, source, a); err != nil {
			return out, err
		}
	}
	statement, args, err := query.NewSelectBuilder(s.store.Renderer(), todoTable).Projections(query.Project(query.CountAll())).Where(query.Equal("owner_key", todoOwner(a))).Build()
	if err != nil {
		return out, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, statement, args...).Scan(&count); err != nil {
		return out, err
	}
	if count+len(items) > 1000 {
		return out, todoError("conflict", "todo_limit")
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	for index, in := range items {
		item := contract.Todo{ID: "todo_" + todoHash([]any{out.BatchID, index})[:32], Title: in.Title, Description: in.Description, Status: "open", DueDate: in.DueDate, DueAt: in.DueAt, Timezone: in.Timezone, Revision: 1, BatchID: out.BatchID, Position: index + 1, SourceConversationID: source, SourceRunID: run, CreatedAt: now, UpdatedAt: now}
		statement, args, err = query.NewInsertBuilder(s.store.Renderer(), todoTable).Columns("owner_key", "todo_id", "batch_id", "position", "created_at", "source_conversation_id", "status", "revision", "payload_json").Values(todoOwner(a), item.ID, item.BatchID, item.Position, now.UnixMilli(), source, item.Status, item.Revision, todoJSON(item)).Build()
		if err = todoExec(ctx, tx, statement, args, err); err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func (s *Store) updateTodo(ctx context.Context, tx *sql.Tx, id string, revision int64, patch contract.TodoPatch, a toolsdk.Authority) (contract.Todo, error) {
	item, err := s.todo(ctx, tx, id, a)
	if err != nil {
		return item, err
	}
	if revision < 1 || item.Revision != revision {
		return item, todoError("conflict", "revision_conflict")
	}
	if patch.Title == nil && patch.Description == nil && patch.Status == nil && patch.DueDate == nil && patch.DueAt == nil && patch.Timezone == nil {
		return item, todoError("bad_request", "todo_patch_invalid")
	}
	if patch.Title != nil {
		item.Title = *patch.Title
	}
	if patch.Description != nil {
		item.Description = *patch.Description
	}
	if patch.Timezone != nil {
		item.Timezone = *patch.Timezone
	}
	if patch.DueDate != nil && patch.DueAt != nil {
		item.DueDate, item.DueAt = *patch.DueDate, *patch.DueAt
	} else if patch.DueDate != nil {
		item.DueDate, item.DueAt = *patch.DueDate, ""
	} else if patch.DueAt != nil {
		item.DueDate, item.DueAt = "", *patch.DueAt
	}
	if patch.DueDate != nil && patch.DueAt != nil && *patch.DueDate != "" && *patch.DueAt != "" {
		return item, todoError("bad_request", "todo_date_invalid")
	}
	if err = service.Validate(contract.TodoInput{Title: item.Title, Description: item.Description, DueDate: item.DueDate, DueAt: item.DueAt, Timezone: item.Timezone}); err != nil {
		return item, err
	}
	now := time.Now().UTC()
	if patch.Status != nil {
		if *patch.Status != "open" && *patch.Status != "completed" {
			return item, todoError("bad_request", "todo_status_invalid")
		}
		if item.Status != *patch.Status {
			item.Status = *patch.Status
			item.CompletedAt = nil
			if item.Status == "completed" {
				item.CompletedAt = &now
			}
		}
	}
	item.Revision++
	item.UpdatedAt = now
	statement, args, err := query.NewUpdateBuilder(s.store.Renderer(), todoTable).Set("status", item.Status).Set("revision", item.Revision).Set("payload_json", todoJSON(item)).Where(query.And(todoScope(a, id), query.Equal("revision", revision))).Build()
	return item, todoCAS(ctx, tx, statement, args, err)
}

func (s *Store) deleteTodo(ctx context.Context, tx *sql.Tx, id string, revision int64, a toolsdk.Authority) error {
	if !validKey(id) || revision < 1 {
		return todoError("bad_request", "todo_invalid")
	}
	statement, args, err := query.NewDeleteBuilder(s.store.Renderer(), todoTable).Where(query.And(todoScope(a, id), query.Equal("revision", revision))).Build()
	return todoCAS(ctx, tx, statement, args, err)
}

// UI mutation receipts live independently from conversations so deleting a
// conversation cannot make a retried personal todo creation run again.
func (s *Store) todoMutation(ctx context.Context, clientID, operation string, input any, a toolsdk.Authority, apply func(*sql.Tx) (any, error), out any) error {
	if err := todoAuthority(a); err != nil {
		return err
	}
	if !validKey(clientID) {
		return todoError("bad_request", "todo_client_id_required")
	}
	return s.transaction(ctx, func(tx *sql.Tx) error {
		command := todoOperationCommand(clientID, operation, input, a)
		receipt, claimed, err := s.operations.Claim(sharedoperation.WithExecutor(ctx, tx), command)
		if errors.Is(err, sharedoperation.ErrIdempotencyConflict) {
			return todoError("conflict", "idempotency_conflict")
		}
		if err != nil {
			return err
		}
		if !claimed {
			if receipt.Status != sharedoperation.StatusSucceeded {
				return todoError("conflict", "mutation_in_progress")
			}
			return json.Unmarshal(receipt.Result, out)
		}
		value, err := apply(tx)
		if err != nil {
			return err
		}
		result := json.RawMessage(todoJSON(value))
		if err = s.operations.Complete(sharedoperation.WithExecutor(ctx, tx), sharedoperation.Completion{ID: command.ID, Scope: command.Scope, Owner: command.Owner, Kind: command.Kind, IdempotencyKey: command.IdempotencyKey, RequestFingerprint: command.RequestFingerprint, Result: result, CompletedAt: time.Now().UTC()}); err != nil {
			return err
		}
		return json.Unmarshal(result, out)
	})
}

func todoOperationCommand(clientID, action string, input any, a toolsdk.Authority) sharedoperation.Command {
	key := todoOwner(a) + ":" + todoHash(clientID)
	id := "todo-operation:" + todoHash([]string{a.WorkspaceID, key})
	return sharedoperation.Command{
		ID: id, Scope: sharedoperation.Scope{WorkspaceID: a.WorkspaceID, ResourceType: "todo_owner", ResourceID: todoOwner(a)},
		Owner: "todo", Kind: "todo.mutation", ActionKey: action, IdempotencyKey: key, RequestFingerprint: todoHash([]any{action, input}),
		RequestedBy: a.UserID, Reason: "Todo mutation", Reference: clientID, StatusURL: "todo://operations/" + id, CreatedAt: time.Now().UTC(),
	}
}
func (s *Store) CreateTodos(ctx context.Context, in contract.TodoCreate, a toolsdk.Authority) (contract.TodoBatch, error) {
	var out contract.TodoBatch
	err := s.todoMutation(ctx, in.ClientID, "create", in, a, func(tx *sql.Tx) (any, error) {
		return s.createTodos(ctx, tx, in.Items, in.SourceConversationID, "", "todo-ui:"+in.ClientID, a)
	}, &out)
	return out, err
}
func (s *Store) UpdateTodo(ctx context.Context, id string, in contract.TodoUpdate, a toolsdk.Authority) (contract.Todo, error) {
	var out contract.Todo
	err := s.todoMutation(ctx, in.ClientID, "update", []any{id, in}, a, func(tx *sql.Tx) (any, error) { return s.updateTodo(ctx, tx, id, in.ExpectedRevision, in.Patch, a) }, &out)
	return out, err
}
func (s *Store) DeleteTodo(ctx context.Context, id string, in contract.TodoDelete, a toolsdk.Authority) error {
	var out map[string]bool
	return s.todoMutation(ctx, in.ClientID, "delete", []any{id, in}, a, func(tx *sql.Tx) (any, error) {
		err := s.deleteTodo(ctx, tx, id, in.ExpectedRevision, a)
		return map[string]bool{"deleted": err == nil}, err
	}, &out)
}
