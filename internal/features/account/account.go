// Package account demonstrates a protected feature with no persistence layer.
package account

import (
	"net/http"

	"example.com/server-golang-template/internal/auth"
	"example.com/server-golang-template/internal/httpapi"
)

type profile struct {
	Subject string `json:"subject"`
}

func Routes(verifier auth.Verifier) []httpapi.Route {
	return []httpapi.Route{auth.Protect(httpapi.Route{
		Method: http.MethodGet, Path: "/account/me", OperationID: "getCurrentAccount", Tags: []string{"Account"},
		Summary:     "Current authenticated account",
		Description: "Returns the verified subject. Requires a bearer token and the profile:read permission. Missing or invalid credentials return 401; malformed Authorization headers return 400; insufficient permissions return 403. A missing or unavailable verifier returns 503. No identity provider is configured by default.",
		Errors:      []int{http.StatusRequestEntityTooLarge, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable},
		Response: map[string]any{"type": "object", "required": []string{"success", "data"}, "additionalProperties": false, "properties": map[string]any{
			"success": map[string]any{"const": true},
			"data":    map[string]any{"type": "object", "required": []string{"subject"}, "additionalProperties": false, "properties": map[string]any{"subject": map[string]any{"type": "string", "minLength": 1}}},
		}},
		Handler: func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.FromContext(r.Context())
			if !ok {
				httpapi.Failure(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
				return
			}
			httpapi.Success(w, profile{Subject: p.Subject})
		},
	}, verifier, "profile:read")}
}
