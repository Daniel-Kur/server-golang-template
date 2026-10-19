package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

func Specification(routes []Route) map[string]any {
	paths := map[string]map[string]any{}
	for _, route := range routes {
		responses := map[string]any{
			"200": map[string]any{"description": "Success", "content": jsonContent(route.Response)},
		}
		for _, status := range route.Errors {
			responses[strconv.Itoa(status)] = map[string]any{
				"description": http.StatusText(status),
				"content":     jsonContent(map[string]string{"$ref": "#/components/schemas/ApiError"}),
			}
		}
		op := map[string]any{
			"operationId": route.OperationID, "summary": route.Summary,
			"description": route.Description, "tags": route.Tags, "responses": responses,
		}
		if route.Authenticated {
			op["security"] = []map[string][]string{{"bearerAuth": {}}}
			if len(route.Permissions) > 0 {
				op["x-required-permissions"] = route.Permissions
			}
			for _, code := range []string{"400", "401", "403"} {
				if response, ok := responses[code].(map[string]any); ok {
					response["headers"] = map[string]any{"WWW-Authenticate": map[string]any{"description": "Bearer authentication challenge", "schema": map[string]string{"type": "string"}}}
				}
			}
		}
		if len(route.Parameters) > 0 {
			op["parameters"] = route.Parameters
		}
		if route.Request != nil {
			op["requestBody"] = map[string]any{"required": true, "content": jsonContent(route.Request)}
		}
		if paths[route.Path] == nil {
			paths[route.Path] = map[string]any{}
		}
		paths[route.Path][strings.ToLower(route.Method)] = op
	}
	errorSchema := map[string]any{
		"type": "object", "required": []string{"success", "error"},
		"properties": map[string]any{
			"success": map[string]any{"const": false},
			"error": map[string]any{
				"type": "object", "required": []string{"code", "message"},
				"properties": map[string]any{
					"code":    map[string]string{"type": "string"},
					"message": map[string]string{"type": "string"},
					"details": map[string]any{},
				},
			},
		},
	}
	return map[string]any{
		"openapi":    "3.1.0",
		"info":       map[string]string{"title": "Server Template API", "version": "1.0.0", "description": "HTTP API documentation generated from Go route schemas."},
		"paths":      paths,
		"components": map[string]any{"schemas": map[string]any{"ApiError": errorSchema}, "securitySchemes": map[string]any{"bearerAuth": map[string]string{"type": "http", "scheme": "bearer", "description": "Access token validated by the configured verifier. Required application permissions are listed in x-required-permissions; they are not OAuth scopes."}}},
	}
}
