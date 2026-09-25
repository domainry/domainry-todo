package todo

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sharedoperation "github.com/domainry/domainry-foundation/operation"
	lifecyclecontract "github.com/domainry/domainry-lifecycle-sdk/contract"
	lifecyclemodel "github.com/domainry/domainry-lifecycle-sdk/model"
	"github.com/domainry/domainry-orm/query"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

const sharedSubjectStepsTable = "_subject_steps"

type SubjectLifecycle struct {
	store     *Store
	runtimeID string
}

type persistedSubjectStep struct {
	WorkspaceID string          `json:"workspace_id"`
	RequestID   string          `json:"request_id"`
	Owner       string          `json:"owner"`
	Operation   string          `json:"operation"`
	Payload     json.RawMessage `json:"payload"`
	CompletedAt int64           `json:"completed_at"`
}

func NewSubjectLifecycle(store *Store, runtimeID string) SubjectLifecycle {
	return SubjectLifecycle{store: store, runtimeID: strings.TrimSpace(runtimeID)}
}

func (SubjectLifecycle) Owner(context.Context) string { return "todo" }

func (s SubjectLifecycle) authority(workspaceID, subjectID string) (toolsdk.Authority, error) {
	a := toolsdk.Authority{Known: true, RuntimeID: s.runtimeID, WorkspaceID: strings.TrimSpace(workspaceID), UserID: strings.TrimSpace(subjectID)}
	if s.store == nil || s.runtimeID == "" || todoAuthority(a) != nil {
		return toolsdk.Authority{}, fmt.Errorf("todo subject scope is required")
	}
	return a, nil
}

func (s SubjectLifecycle) PreviewSubject(ctx context.Context, workspaceID, subjectID string) (json.RawMessage, error) {
	a, err := s.authority(workspaceID, subjectID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	statement, args, buildErr := query.NewSelectBuilder(s.store.store.Renderer(), todoTable).Projections(query.Project(query.CountAll())).Where(query.Equal("owner_key", todoOwner(a))).Build()
	if buildErr != nil {
		return nil, buildErr
	}
	var todoCount int64
	if err := s.store.store.Database().QueryRowContext(ctx, statement, args...).Scan(&todoCount); err != nil {
		return nil, err
	}
	counts["todos"] = todoCount
	receipts, err := s.store.operations.SearchRecords(ctx, sharedoperation.RecordFilter{WorkspaceID: a.WorkspaceID, Owner: "todo", Kind: "todo.mutation", RequestedBy: a.UserID, Limit: 1}, true)
	if err != nil {
		return nil, err
	}
	counts["mutation_receipts"] = int64(receipts.Count)
	return json.Marshal(counts)
}

func (s SubjectLifecycle) ExportSubjectForRequest(ctx context.Context, _ string, workspaceID, subjectID string) (json.RawMessage, error) {
	a, err := s.authority(workspaceID, subjectID)
	if err != nil {
		return nil, err
	}
	statement, args, err := query.NewSelectBuilder(s.store.store.Renderer(), todoTable).Columns("payload_json").Where(query.Equal("owner_key", todoOwner(a))).OrderBy(query.Ascending("created_at"), query.Ascending("todo_id")).Build()
	if err != nil {
		return nil, err
	}
	rows, err := s.store.store.Database().QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		items = append(items, append(json.RawMessage(nil), raw...))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"todos": items})
}

func (s SubjectLifecycle) EraseSubjectForRequest(ctx context.Context, requestID, workspaceID, subjectID string, holds []lifecyclemodel.LegalHold) (json.RawMessage, error) {
	if len(holds) > 0 {
		return nil, fmt.Errorf("todo subject erasure blocked by legal hold")
	}
	a, err := s.authority(workspaceID, subjectID)
	requestID = strings.TrimSpace(requestID)
	if err != nil || requestID == "" || len(requestID) > 96 {
		return nil, fmt.Errorf("todo subject erasure request is required")
	}
	var receipt json.RawMessage
	err = s.store.transaction(ctx, func(tx *sql.Tx) error {
		lookup, args, buildErr := query.NewWorkspaceSelectBuilder(s.store.store.Renderer(), sharedSubjectStepsTable, a.WorkspaceID).
			Columns("payload_json").
			Where(query.And(query.Equal("request_id", requestID), query.Equal("owner", "todo"), query.Equal("operation", "erase"))).Build()
		if buildErr != nil {
			return buildErr
		}
		var stored string
		if scanErr := tx.QueryRowContext(ctx, lookup, args...).Scan(&stored); scanErr == nil {
			var step persistedSubjectStep
			if json.Unmarshal([]byte(stored), &step) != nil || step.WorkspaceID != a.WorkspaceID || step.RequestID != requestID || step.Owner != "todo" || step.Operation != "erase" || !json.Valid(step.Payload) {
				return fmt.Errorf("todo shared subject execution step is invalid")
			}
			receipt = append(json.RawMessage(nil), step.Payload...)
			return nil
		} else if !errorsIsNoRows(scanErr) {
			return scanErr
		}
		changed := map[string]int64{}
		statement, deleteArgs, deleteErr := query.NewDeleteBuilder(s.store.store.Renderer(), todoTable).Where(query.Equal("owner_key", todoOwner(a))).Build()
		if deleteErr != nil {
			return deleteErr
		}
		result, deleteErr := tx.ExecContext(ctx, statement, deleteArgs...)
		if deleteErr != nil {
			return deleteErr
		}
		changed["todos"], _ = result.RowsAffected()
		changed["mutation_receipts"], deleteErr = s.store.operations.DeleteRecords(sharedoperation.WithExecutor(ctx, tx), sharedoperation.RecordFilter{WorkspaceID: a.WorkspaceID, Owner: "todo", Kind: "todo.mutation", RequestedBy: a.UserID})
		if deleteErr != nil {
			return deleteErr
		}
		completedAt := time.Now().UTC()
		receipt, _ = json.Marshal(map[string]any{"request_id": requestID, "changed": changed, "source_references_removed": changed["todos"], "completed_at": completedAt.UnixMilli()})
		step, marshalErr := json.Marshal(persistedSubjectStep{WorkspaceID: a.WorkspaceID, RequestID: requestID, Owner: "todo", Operation: "erase", Payload: append(json.RawMessage(nil), receipt...), CompletedAt: completedAt.UnixMilli()})
		if marshalErr != nil {
			return marshalErr
		}
		statement, insertArgs, buildErr := query.NewWorkspaceInsertBuilder(s.store.store.Renderer(), sharedSubjectStepsTable, a.WorkspaceID).
			Columns("request_id", "owner", "operation", "payload_json", "completed_at").
			Values(requestID, "todo", "erase", string(step), completedAt.UnixMilli()).Build()
		if buildErr != nil {
			return buildErr
		}
		_, buildErr = tx.ExecContext(ctx, statement, insertArgs...)
		return buildErr
	})
	return receipt, err
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }

var _ lifecyclecontract.SubjectExecutionHandler = SubjectLifecycle{}
