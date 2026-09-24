package saas

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	lifecyclecontract "github.com/domainry/domainry-lifecycle-sdk/contract"
	lifecyclemodel "github.com/domainry/domainry-lifecycle-sdk/model"
	todocontract "github.com/domainry/domainry-todo-sdk/contract"
	"github.com/domainry/domainry-todo-sdk/saashost"
	toolsdk "github.com/domainry/domainry-tools-sdk"
)

const maxEnvelopeBytes = 6 << 20

type Dependencies struct {
	Audience, ServiceAccessToken string
	Todos                        todocontract.TodoService
	Mutations                    todocontract.MutationService
	Subjects                     lifecyclecontract.SubjectExecutionHandler
}
type Server struct {
	dependencies Dependencies
	secret       []byte
}

func New(d Dependencies) (*Server, error) {
	if strings.TrimSpace(d.Audience) == "" || strings.TrimSpace(d.ServiceAccessToken) == "" || d.Todos == nil || d.Mutations == nil || d.Subjects == nil {
		return nil, errors.New("Todo SaaS server dependencies are incomplete")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &Server{dependencies: d, secret: secret}, nil
}
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }
func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	want := "Bearer " + strings.TrimSpace(s.dependencies.ServiceAccessToken)
	got := r.Header.Get("Authorization")
	if len(got) != len(want) || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		writeStatus(w, http.StatusUnauthorized, "forbidden", "todo.service_credential_invalid")
		return
	}
	if r.Header.Get(saashost.RuntimeIDHeader) != s.dependencies.Audience {
		writeStatus(w, http.StatusForbidden, "forbidden", "todo.runtime_mismatch")
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == todocontract.SaaSDiscoveryPath:
		writeJSON(w, http.StatusOK, todocontract.Descriptor{ProtocolVersion: todocontract.SaaSProtocolVersionV1, Mode: todocontract.DeploymentModeSaaS, Audience: s.dependencies.Audience, Capabilities: append([]string(nil), todocontract.SaaSCapabilitiesV1...)})
	case r.Method == http.MethodPost && r.URL.Path == todocontract.SaaSInvokePath:
		s.invoke(w, r)
	default:
		writeStatus(w, http.StatusNotFound, "not_found", "todo.route_not_found")
	}
}

type grantContextKey struct{}
type grantContext struct {
	operation string
	secret    []byte
	grants    map[string]json.RawMessage
}
type challengeError struct {
	kind  string
	input json.RawMessage
}

func (*challengeError) Error() string { return "Todo source authorization challenge required" }
func challengeToken(secret []byte, operation, kind string, input []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(operation))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(input)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func WithSourceGrants(ctx context.Context, operation string, secret []byte, values []todocontract.SaaSGrant) context.Context {
	grants := map[string]json.RawMessage{}
	for _, value := range values {
		if token := strings.TrimSpace(value.Token); token != "" && len(token) <= 256 {
			grants[token] = value.Result
		}
	}
	return context.WithValue(ctx, grantContextKey{}, grantContext{operation: operation, secret: secret, grants: grants})
}

type SourceBridge struct{}

func (SourceBridge) Authorize(ctx context.Context, reference string, authority toolsdk.Authority) error {
	state, ok := ctx.Value(grantContextKey{}).(grantContext)
	if !ok {
		return &toolsdk.Error{Class: "unavailable", Code: "todo.grant_context_unavailable"}
	}
	input, _ := json.Marshal(struct {
		Reference string            `json:"reference"`
		Authority toolsdk.Authority `json:"authority"`
	}{reference, authority})
	token := challengeToken(state.secret, state.operation, "source.authorize", input)
	if _, ok = state.grants[token]; !ok {
		return &challengeError{kind: "source.authorize", input: input}
	}
	return nil
}
func (s *Server) invoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEnvelopeBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var envelope todocontract.SaaSRequest
	if decoder.Decode(&envelope) != nil || decoder.Decode(&struct{}{}) != io.EOF || strings.TrimSpace(envelope.Operation) == "" || len(envelope.Operation) > 128 {
		writeStatus(w, http.StatusBadRequest, "bad_request", "todo.request_invalid")
		return
	}
	ctx := WithSourceGrants(r.Context(), envelope.Operation, s.secret, envelope.Grants)
	result, err := s.dispatch(ctx, envelope.Operation, envelope.Input)
	if err != nil {
		var challenge *challengeError
		if errors.As(err, &challenge) {
			writeJSON(w, http.StatusOK, todocontract.SaaSResponse{Challenge: &todocontract.SaaSChallenge{Token: challengeToken(s.secret, envelope.Operation, challenge.kind, challenge.input), Kind: challenge.kind, Input: challenge.input}})
			return
		}
		writeJSON(w, http.StatusOK, todocontract.SaaSResponse{Error: safeError(err)})
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		writeJSON(w, http.StatusOK, todocontract.SaaSResponse{Error: &todocontract.SaaSError{Class: "unavailable", Code: "todo.response_invalid"}})
		return
	}
	writeJSON(w, http.StatusOK, todocontract.SaaSResponse{Result: raw})
}
func decodeInput(raw json.RawMessage, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return &toolsdk.Error{Class: "bad_request", Code: "todo.request_invalid"}
	}
	return nil
}
func (s *Server) dispatch(ctx context.Context, operation string, raw json.RawMessage) (any, error) {
	switch operation {
	case "todos.list":
		var in struct {
			Input     todocontract.TodoQuery `json:"input"`
			Authority toolsdk.Authority      `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		return s.dependencies.Todos.Todos(ctx, in.Input, in.Authority)
	case "todos.get":
		var in struct {
			ID        string            `json:"id"`
			Authority toolsdk.Authority `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		return s.dependencies.Todos.Todo(ctx, in.ID, in.Authority)
	case "todos.create":
		var in struct {
			Input     todocontract.TodoCreate `json:"input"`
			Authority toolsdk.Authority       `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		return s.dependencies.Todos.CreateTodos(ctx, in.Input, in.Authority)
	case "todos.update":
		var in struct {
			ID        string                  `json:"id"`
			Input     todocontract.TodoUpdate `json:"input"`
			Authority toolsdk.Authority       `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		return s.dependencies.Todos.UpdateTodo(ctx, in.ID, in.Input, in.Authority)
	case "todos.delete":
		var in struct {
			ID        string                  `json:"id"`
			Input     todocontract.TodoDelete `json:"input"`
			Authority toolsdk.Authority       `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		e := s.dependencies.Todos.DeleteTodo(ctx, in.ID, in.Input, in.Authority)
		return struct{}{}, e
	case "mutations.apply":
		var in struct {
			Input     todocontract.Mutation `json:"input"`
			Authority toolsdk.Authority     `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		return s.dependencies.Mutations.ApplyMutation(ctx, in.Input, in.Authority)
	case "mutations.receipt":
		var in struct {
			Input     todocontract.Mutation `json:"input"`
			Authority toolsdk.Authority     `json:"authority"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		result, found, e := s.dependencies.Mutations.MutationReceipt(ctx, in.Input, in.Authority)
		return struct {
			Result todocontract.MutationResult `json:"result"`
			Found  bool                        `json:"found"`
		}{result, found}, e
	case "subjects.preview", "subjects.export", "subjects.erase":
		var in struct {
			RequestID   string                     `json:"request_id,omitempty"`
			WorkspaceID string                     `json:"workspace_id"`
			SubjectID   string                     `json:"subject_id"`
			LegalHolds  []lifecyclemodel.LegalHold `json:"legal_holds,omitempty"`
		}
		if e := decodeInput(raw, &in); e != nil {
			return nil, e
		}
		if operation == "subjects.preview" {
			return s.dependencies.Subjects.PreviewSubject(ctx, in.WorkspaceID, in.SubjectID)
		}
		if operation == "subjects.export" {
			return s.dependencies.Subjects.ExportSubjectForRequest(ctx, in.RequestID, in.WorkspaceID, in.SubjectID)
		}
		return s.dependencies.Subjects.EraseSubjectForRequest(ctx, in.RequestID, in.WorkspaceID, in.SubjectID, in.LegalHolds)
	default:
		return nil, &toolsdk.Error{Class: "not_found", Code: "todo.operation_not_found"}
	}
}
func safeError(err error) *todocontract.SaaSError {
	var coded *toolsdk.Error
	if errors.As(err, &coded) {
		return &todocontract.SaaSError{Class: safeClass(coded.Class), Code: safeCode(coded.Code), Retryable: coded.Retryable}
	}
	return &todocontract.SaaSError{Class: "unavailable", Code: "todo.internal_unavailable"}
}
func safeClass(v string) string {
	switch v {
	case "bad_request", "forbidden", "not_found", "conflict", "unavailable":
		return v
	default:
		return "unavailable"
	}
}
func safeCode(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 128 {
		return "todo.request_failed"
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_') {
			return "todo.request_failed"
		}
	}
	return v
}
func writeStatus(w http.ResponseWriter, status int, class, code string) {
	writeJSON(w, status, todocontract.SaaSResponse{Error: &todocontract.SaaSError{Class: class, Code: code}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
