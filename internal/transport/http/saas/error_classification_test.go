package saas

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	toolsdk "github.com/domainry/domainry-tools-sdk"
)

type classificationCapture struct {
	*httptest.ResponseRecorder
	kind, code  string
	internalErr error
}

func (capture *classificationCapture) RecordError(kind, code string) {
	capture.kind, capture.code = kind, code
}

func (capture *classificationCapture) RecordInternalError(err error) {
	capture.internalErr = err
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

func TestWriteSaaSErrorUsesUnavailableOnlyForExplicitDependencyFailure(t *testing.T) {
	capture := &classificationCapture{ResponseRecorder: httptest.NewRecorder()}
	writeSaaSError(capture, &toolsdk.Error{Class: "unavailable", Code: "todo.database_unavailable", Retryable: true})
	if capture.Code != http.StatusServiceUnavailable || capture.kind != "unavailable" || capture.code != "todo.database_unavailable" || capture.internalErr != nil {
		t.Fatalf("status=%d kind=%q code=%q internal=%v", capture.Code, capture.kind, capture.code, capture.internalErr)
	}
}

func TestWriteSaaSErrorRecordsUnknownFailureWithoutLeakingRawText(t *testing.T) {
	capture := &classificationCapture{ResponseRecorder: httptest.NewRecorder()}
	writeSaaSError(capture, errors.New("database password=private"))
	if capture.Code != http.StatusInternalServerError || capture.kind != "internal" || capture.internalErr == nil {
		t.Fatalf("status=%d kind=%q internal=%v", capture.Code, capture.kind, capture.internalErr)
	}
	if strings.Contains(capture.Body.String(), "private") || strings.Contains(capture.Body.String(), "password") {
		t.Fatalf("raw error leaked: %s", capture.Body.String())
	}
}
