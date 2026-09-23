package todo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	sharedoperation "github.com/domainry/domainry-foundation/operation"
	ormdriver "github.com/domainry/domainry-orm/driver"
	"github.com/domainry/domainry-orm/query"
	"github.com/domainry/domainry-orm/sqlhost"
	toolsdk "github.com/domainry/domainry-tools-sdk"
	"strings"
	"time"
	"unicode/utf8"
)

type Dialect interface {
	Identifier(string) string
	Table(string) string
	Placeholder(int) string
}
type database struct {
	db      sqlhost.Database
	dialect Dialect
	profile ormdriver.Profile
}

func (d *database) Database() sqlhost.Database { return d.db }
func (d *database) Renderer() Dialect          { return d.dialect }
func (d *database) IsTransientError(err error) bool {
	if d.profile == nil {
		return false
	}
	switch d.profile.ClassifyError(err) {
	case ormdriver.ErrorSerialization, ormdriver.ErrorDeadlock, ormdriver.ErrorUnavailable, ormdriver.ErrorTimeout:
		return true
	}
	return false
}

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SourceAuthorizer is optional provenance validation supplied by composition.
// Todo never reads a conversation table or depends on Agent execution state.
type SourceAuthorizer func(context.Context, DB, string, toolsdk.Authority) error
type Store struct {
	store      *database
	source     SourceAuthorizer
	operations *sharedoperation.SQLStore
}

func NewStore(db sqlhost.Database, dialect Dialect, profile ormdriver.Profile, source SourceAuthorizer) (*Store, error) {
	if db == nil || dialect == nil {
		return nil, fmt.Errorf("todo database and dialect are required")
	}
	return &Store{store: &database{db: db, dialect: dialect, profile: profile}, source: source, operations: sharedoperation.NewSQLStore(db, sharedoperation.AdaptDialect(dialect))}, nil
}
func (s *Store) authorizeSource(ctx context.Context, db DB, ref string, a toolsdk.Authority) error {
	if s.source != nil {
		return s.source(ctx, db, ref, a)
	}
	return nil
}
func todoHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func todoJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func todoError(class, code string) error {
	return &toolsdk.Error{Class: class, Code: "agent.conversation." + code}
}

func todoAuthority(a toolsdk.Authority) error {
	if !a.Known || strings.TrimSpace(a.RuntimeID) == "" || strings.TrimSpace(a.WorkspaceID) == "" || strings.TrimSpace(a.UserID) == "" || len(a.RuntimeID) > 255 || len(a.WorkspaceID) > 255 || len(a.UserID) > 255 {
		return todoError("forbidden", "principal_required")
	}
	return nil
}

func todoOwner(a toolsdk.Authority) string {
	return todoHash([]string{a.RuntimeID, a.WorkspaceID, a.UserID})
}

func todoExec(ctx context.Context, db DB, statement string, args []any, err error) error {
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, statement, args...)
	return err
}

func todoCAS(ctx context.Context, db DB, statement string, args []any, err error) error {
	if err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, statement, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return todoError("conflict", "revision_conflict")
	}
	return nil
}

func (s *Store) transaction(ctx context.Context, fn func(*sql.Tx) error) error {
	for attempt := 0; ; attempt++ {
		tx, err := s.store.Database().BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err == nil {
			err = fn(tx)
			if err == nil {
				err = tx.Commit()
			}
			_ = tx.Rollback()
		}
		if err == nil || attempt >= 4 || !s.store.IsTransientError(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(5<<attempt) * time.Millisecond):
		}
	}
}

func (s *Store) readPayload(ctx context.Context, db DB, table string, predicate query.Predicate, out any) (bool, error) {
	q, args, err := query.NewSelectBuilder(s.store.Renderer(), table).Columns("payload_json").Where(predicate).Build()
	if err != nil {
		return false, err
	}
	var raw []byte
	err = db.QueryRowContext(ctx, q, args...).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(raw, out)
}

func validText(value string, limit int, required bool) bool {
	return limit > 0 && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && (!required || strings.TrimSpace(value) != "")
}

func validKey(value string) bool {
	if len(value) == 0 || len(value) > 96 {
		return false
	}
	for _, c := range value {
		if c != '_' && c != '-' && c != '.' && c != ':' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
