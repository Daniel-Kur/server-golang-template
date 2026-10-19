package httpapi

import (
	"net/http"
)

type Route struct {
	Method, Path, OperationID, Summary, Description string
	Request                                         *ObjectSchema
	Response                                        any
	Tags                                            []string
	Errors                                          []int
	Parameters                                      []Parameter
	// Authenticated and Permissions describe policy applied by auth.Protect.
	Authenticated bool
	Permissions   []string
	Handler       http.HandlerFunc
}

// Parameter describes an OpenAPI path or query parameter. Handlers remain
// responsible for parsing and validating these values.
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Schema      any    `json:"schema"`
}

// Router uses Go's method-aware ServeMux, including path parameters and HEAD.
// Only the mux's own 404/405 responses are adapted to the API envelope.
func Router(routes []Route) http.Handler {
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.HandleFunc(route.Method+" "+route.Path, route.Handler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler, pattern := mux.Handler(r)
		if pattern == "" {
			handler.ServeHTTP(&routingErrorWriter{ResponseWriter: w}, r)
			return
		}
		// ServeHTTP (not Handler.ServeHTTP) populates PathValue and Pattern.
		mux.ServeHTTP(w, r)
	})
}

type routingErrorWriter struct {
	http.ResponseWriter
	replaced bool
}

func (w *routingErrorWriter) WriteHeader(status int) {
	switch status {
	case http.StatusNotFound:
		w.replaced = true
		w.Header().Del("Content-Length")
		Failure(w.ResponseWriter, status, "NOT_FOUND", "Route not found")
	case http.StatusMethodNotAllowed:
		w.replaced = true
		w.Header().Del("Content-Length")
		Failure(w.ResponseWriter, status, "METHOD_NOT_ALLOWED", "Method not allowed")
	default:
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *routingErrorWriter) Write(body []byte) (int, error) {
	if w.replaced {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}
