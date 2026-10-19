package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeMuxContracts(t *testing.T) {
	routes := []Route{
		{Method: "GET", Path: "/items/{id}", Handler: func(w http.ResponseWriter, r *http.Request) {
			Success(w, map[string]string{"id": r.PathValue("id"), "pattern": r.Pattern})
		}},
		{Method: "POST", Path: "/items/{id}", Handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }},
		{Method: "HEAD", Path: "/items/{id}", Handler: func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Explicit-Head", "true") }},
	}
	app := Router(routes)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/items/a%2Fb", nil))
	if !strings.Contains(w.Body.String(), `"id":"a/b"`) || !strings.Contains(w.Body.String(), `"pattern":"GET /items/{id}"`) {
		t.Fatal(w.Body)
	}
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("HEAD", "/items/1", nil))
	if w.Header().Get("Explicit-Head") != "true" {
		t.Fatal("GET shadowed explicit HEAD")
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"DELETE", "/items/1", 405}, {"GET", "/unknown", 404}, {"GET", "/items//1", 301},
	} {
		w = httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body)
		}
		if tc.status >= 400 {
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["success"] != false {
				t.Fatal(w.Body)
			}
		}
		if tc.status == 405 && w.Header().Get("Allow") != "GET, HEAD, POST" {
			t.Fatal(w.Header())
		}
	}
}

func TestConflictingRoutesFailAtStartup(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate routes silently accepted")
		}
	}()
	handler := func(http.ResponseWriter, *http.Request) {}
	Router([]Route{{Method: "GET", Path: "/items", Handler: handler}, {Method: "GET", Path: "/items", Handler: handler}})
}

func TestOpenAPIMetadata(t *testing.T) {
	spec := Specification([]Route{{Method: "GET", Path: "/items", OperationID: "listItems", Tags: []string{"Items"}, Errors: []int{401, 500}, Parameters: []Parameter{{Name: "limit", In: "query", Schema: map[string]string{"type": "integer"}}}}})
	paths := spec["paths"].(map[string]map[string]any)
	op := paths["/items"]["get"].(map[string]any)
	if op["tags"].([]string)[0] != "Items" {
		t.Fatal(op)
	}
	if op["parameters"].([]Parameter)[0].Name != "limit" {
		t.Fatal(op)
	}
	responses := op["responses"].(map[string]any)
	if responses["401"] == nil || responses["422"] != nil {
		t.Fatal(responses)
	}
}
