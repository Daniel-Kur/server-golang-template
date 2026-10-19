package httpapi

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteError(t *testing.T) {
	for _, tc := range []struct {
		err      error
		status   int
		contains string
	}{
		{fmt.Errorf("wrapped: %w", &Error{Status: 409, Code: "CONFLICT", Message: "Already exists", Details: map[string]string{"resource": "example"}}), 409, `"details":{"resource":"example"}`},
		{errors.New("password=secret"), 500, `"message":"Internal Server Error"`},
		{&Error{Status: 200, Code: "BAD", Message: "secret"}, 500, `"code":"INTERNAL_SERVER_ERROR"`},
		{&Error{Status: 400, Code: "BAD", Message: "Invalid", Details: make(chan int)}, 500, `"code":"INTERNAL_SERVER_ERROR"`},
	} {
		w := httptest.NewRecorder()
		WriteError(w, tc.err)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) || strings.Contains(w.Body.String(), "secret") {
			t.Fatal(w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	Failure(w, 400, "BAD_REQUEST", "Invalid")
	if strings.Contains(w.Body.String(), "details") {
		t.Fatal("absent details should be omitted")
	}
}
