package contract

import (
	"context"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	"time"
)

// A personal todo is an owned work item, not an Agent run or scheduled job.
// BatchID/Position preserve the original order even after another item is
// completed or deleted. Source references never grant access to that source.
type Todo struct {
	ID                   string     `json:"id"`
	Title                string     `json:"title"`
	Description          string     `json:"description,omitempty"`
	Status               string     `json:"status"`
	DueDate              string     `json:"due_date,omitempty"`
	DueAt                string     `json:"due_at,omitempty"`
	Timezone             string     `json:"timezone"`
	Revision             int64      `json:"revision"`
	BatchID              string     `json:"batch_id"`
	Position             int        `json:"position"`
	SourceConversationID string     `json:"source_conversation_id,omitempty"`
	SourceRunID          string     `json:"source_run_id,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
}

// Dates represent a calendar day in Timezone, not an implicit midnight.
// Instants require RFC3339 with an offset consistent with the named IANA zone.
type TodoInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	DueDate     string `json:"due_date,omitempty"`
	DueAt       string `json:"due_at,omitempty"`
	Timezone    string `json:"timezone"`
}
type TodoCreate struct {
	ClientID             string      `json:"client_id"`
	SourceConversationID string      `json:"source_conversation_id,omitempty"`
	Items                []TodoInput `json:"items"`
}
type TodoBatch struct {
	BatchID string `json:"batch_id"`
	Items   []Todo `json:"items"`
}
type TodoPatch struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Status      *string `json:"status,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
	DueAt       *string `json:"due_at,omitempty"`
	Timezone    *string `json:"timezone,omitempty"`
}
type TodoUpdate struct {
	ClientID         string    `json:"client_id"`
	ExpectedRevision int64     `json:"expected_revision"`
	Patch            TodoPatch `json:"patch"`
}
type TodoDelete struct {
	ClientID         string `json:"client_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type TodoQuery struct {
	Query                string `json:"query,omitempty"`
	Status               string `json:"status,omitempty"` // all, open or completed
	SourceConversationID string `json:"source_conversation_id,omitempty"`
	BatchID              string `json:"batch_id,omitempty"`
	Cursor               string `json:"cursor,omitempty"`
	Limit                int    `json:"limit,omitempty"`
}
type TodoPage struct {
	Items      []Todo `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Complete   bool   `json:"complete"`
}

type TodoService interface {
	Todos(context.Context, TodoQuery, toolsdk.Authority) (TodoPage, error)
	Todo(context.Context, string, toolsdk.Authority) (Todo, error)
	CreateTodos(context.Context, TodoCreate, toolsdk.Authority) (TodoBatch, error)
	UpdateTodo(context.Context, string, TodoUpdate, toolsdk.Authority) (Todo, error)
	DeleteTodo(context.Context, string, TodoDelete, toolsdk.Authority) error
}
