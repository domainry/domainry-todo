package contract

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ApplicationRef identifies the embedding application without exposing any
// host-owned persistence object to Todo.
type ApplicationRef struct{ RuntimeID string }

func (ref ApplicationRef) Validate() error {
	if strings.TrimSpace(ref.RuntimeID) == "" || len(strings.TrimSpace(ref.RuntimeID)) > 255 {
		return fmt.Errorf("Todo Runtime identity is required")
	}
	return nil
}

// Mutation is the Todo-owned command envelope used by conversation tools.
// The host contributes source references and an already-open transaction, but
// Todo remains the only module that interprets or persists this payload.
type Mutation struct {
	Key                  string
	Operation            string
	Data                 json.RawMessage
	SourceConversationID string
	SourceRunID          string
}

type MutationResult struct {
	ResourceID string
	Content    json.RawMessage
}
