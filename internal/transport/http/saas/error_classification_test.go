package saas

import (
	"net/http/httptest"
	"testing"
)

type classificationCapture struct {
	*httptest.ResponseRecorder
	kind, code string
}

func (capture *classificationCapture) RecordError(kind, code string) {
	capture.kind, capture.code = kind, code
}

func TestTodoErrorClassificationMatrix(t *testing.T) {
	tests := []struct {
		status int
		kind   string
		code   string
	}{
		{503, "unavailable", "todo.database_unavailable"},
		{502, "unavailable", "todo.dependency_unavailable"},
		{409, "conflict", "todo.revision_fence_rejected"},
		{400, "bad_request", "todo.request_invalid"},
		{403, "forbidden", "todo.permission_denied"},
	}
	for _, test := range tests {
		capture := &classificationCapture{ResponseRecorder: httptest.NewRecorder()}
		writeStatus(capture, test.status, test.kind, test.code)
		if capture.kind != test.kind || capture.code != test.code {
			t.Fatalf("status=%d kind=%q code=%q", test.status, capture.kind, capture.code)
		}
	}
}
