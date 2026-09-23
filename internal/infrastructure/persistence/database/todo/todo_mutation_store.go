package todo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	"github.com/domainry/domainry-todo/contract"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

type Mutation = contract.Mutation
type MutationResult = contract.MutationResult

func (s *Store) ApplyMutation(ctx context.Context, in Mutation, a toolsdk.Authority) (MutationResult, error) {
	var out MutationResult
	err := s.todoMutation(ctx, "tool-"+todoHash(in.Key), in.Operation, in, a, func(tx *sql.Tx) (any, error) { return s.ApplyInTransaction(ctx, tx, in, a) }, &out)
	return out, err
}

// ApplyInTransaction supports the existing embedded migration path. The
// caller supplies a transaction; no Agent table is read or written here.
// Independent callers use ApplyMutation to persist the domain receipt.
func (s *Store) ApplyInTransaction(ctx context.Context, tx *sql.Tx, in Mutation, a toolsdk.Authority) (MutationResult, error) {
	var out MutationResult
	if err := todoAuthority(a); err != nil {
		return out, err
	}
	if in.Key == "" || len(in.Key) > 2048 || len(in.Data) > 65536 {
		return out, todoError("bad_request", "todo_invalid")
	}
	var args struct {
		ID               string               `json:"id"`
		Items            []contract.TodoInput `json:"items"`
		ExpectedRevision int64                `json:"expected_revision"`
		Patch            contract.TodoPatch   `json:"patch"`
	}
	decoder := json.NewDecoder(bytes.NewReader(in.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return out, todoError("bad_request", "todo_invalid")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return out, todoError("bad_request", "todo_invalid")
	}
	var value any
	var err error
	switch in.Operation {
	case "todo_create":
		var batch contract.TodoBatch
		batch, err = s.createTodos(ctx, tx, args.Items, in.SourceConversationID, in.SourceRunID, in.Key, a)
		for i := range batch.Items {
			batch.Items[i].Description = ""
		}
		value, out.ResourceID = batch, batch.BatchID
	case "todo_update":
		value, err = s.updateTodo(ctx, tx, args.ID, args.ExpectedRevision, args.Patch, a)
		out.ResourceID = args.ID
	case "todo_delete":
		err = s.deleteTodo(ctx, tx, args.ID, args.ExpectedRevision, a)
		value = map[string]any{"id": args.ID, "deleted": err == nil}
		out.ResourceID = args.ID
	default:
		err = todoError("bad_request", "todo_operation_invalid")
	}
	if err != nil {
		return MutationResult{}, err
	}
	out.Content = todoJSON(value)
	return out, nil
}

// MutationReceipt never reapplies an operation. Unknown outcomes can be
// reconciled after a lost response without duplicating the business effect.
func (s *Store) MutationReceipt(ctx context.Context, in Mutation, a toolsdk.Authority) (MutationResult, bool, error) {
	if err := todoAuthority(a); err != nil {
		return MutationResult{}, false, err
	}
	command := todoOperationCommand("tool-"+todoHash(in.Key), in.Operation, in, a)
	receipt, found, err := s.operations.GetRecord(ctx, sharedoperation.RecordFilter{WorkspaceID: a.WorkspaceID, Owner: command.Owner, Kind: command.Kind, IdempotencyKey: command.IdempotencyKey})
	if err != nil || !found {
		return MutationResult{}, found, err
	}
	if receipt.RequestFingerprint != command.RequestFingerprint {
		return MutationResult{}, false, todoError("conflict", "idempotency_conflict")
	}
	if receipt.Status != sharedoperation.StatusSucceeded {
		return MutationResult{}, false, nil
	}
	var out MutationResult
	err = json.Unmarshal(receipt.ResultJSON, &out)
	return out, true, err
}
