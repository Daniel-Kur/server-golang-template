// Package health provides the liveness and diagnostic example feature.
package health

import (
	"net/http"
	"time"

	"example.com/server-golang-template/internal/httpapi"
)

const livenessPath = "/health"

// IsLivenessRequest identifies the quota-exempt process liveness probe.
// Diagnostic POST and uptime requests must still consume quota.
func IsLivenessRequest(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == livenessPath
}

type statusResponse struct {
	Status string `json:"status"`
}
type uptimeResponse struct {
	UptimeMS      int64     `json:"uptimeMs"`
	UptimeSeconds float64   `json:"uptimeSeconds"`
	StartedAt     time.Time `json:"startedAt"`
}
type echoResponse struct {
	Status   string            `json:"status"`
	From     string            `json:"from"`
	Received map[string]string `json:"received"`
}

func Routes(started time.Time) []httpapi.Route {
	echo := httpapi.ObjectSchema{
		Properties: map[string]httpapi.StringSchema{
			"source": {MinLength: 1, MaxLength: 100},
		},
	}
	return []httpapi.Route{
		{
			Method: http.MethodGet, Path: livenessPath, OperationID: "getHealthStatus", Tags: []string{"Health"}, Errors: []int{413, 500},
			Summary: "Liveness check", Description: "Confirms that the API process can handle requests.",
			Response: envelope(object(map[string]any{"status": literal("ok")}, "status")),
			Handler: func(w http.ResponseWriter, r *http.Request) {
				httpapi.Success(w, statusResponse{Status: "ok"})
			},
		},
		{
			Method: http.MethodGet, Path: "/health/uptime", OperationID: "getHealthUptime", Tags: []string{"Health"}, Errors: []int{413, 429, 500, 503},
			Summary: "Process uptime", Description: "Returns application start time and elapsed uptime.",
			Response: envelope(object(map[string]any{
				"uptimeMs":      map[string]any{"type": "integer", "minimum": 0},
				"uptimeSeconds": map[string]any{"type": "number", "minimum": 0},
				"startedAt":     map[string]string{"type": "string", "format": "date-time"},
			}, "uptimeMs", "uptimeSeconds", "startedAt")),
			Handler: func(w http.ResponseWriter, r *http.Request) {
				elapsed := time.Since(started)
				httpapi.Success(w, uptimeResponse{
					UptimeMS: elapsed.Milliseconds(), UptimeSeconds: elapsed.Seconds(), StartedAt: started.UTC(),
				})
			},
		},
		{
			Method: http.MethodPost, Path: livenessPath, OperationID: "postHealthEcho", Tags: []string{"Health"}, Errors: []int{400, 413, 415, 422, 429, 500, 503},
			Summary: "Health echo", Description: "Validates and echoes a small diagnostic payload. Returns 200 OK because this operation does not create or persist a resource.",
			Request: &echo,
			Response: envelope(object(map[string]any{
				"status": literal("ok"), "from": literal("POST"), "received": echo,
			}, "status", "from", "received")),
			Handler: echoHandler(echo),
		},
	}
}

func echoHandler(schema httpapi.ObjectSchema) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := schema.Decode(r)
		if err != nil {
			httpapi.WriteError(w, err)
			return
		}
		httpapi.Success(w, echoResponse{Status: "ok", From: "POST", Received: body})
	}
}

func literal(value string) any { return map[string]any{"type": "string", "const": value} }
func object(properties map[string]any, required ...string) any {
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func envelope(data any) any {
	return object(map[string]any{"success": map[string]any{"const": true}, "data": data}, "success", "data")
}
