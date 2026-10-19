package server

import (
	"time"

	"example.com/server-golang-template/internal/auth"
	"example.com/server-golang-template/internal/config"
	"example.com/server-golang-template/internal/features/account"
	"example.com/server-golang-template/internal/features/health"
	"example.com/server-golang-template/internal/httpapi"
)

// applicationRoutes is the central registry for feature and documentation routes.
// Feature packages own their handlers, schemas, and access policies; dependencies
// are passed explicitly here. Generate the spec before adding documentation routes.
func applicationRoutes(c config.Config, verifier auth.Verifier) []httpapi.Route {
	routes := health.Routes(time.Now())
	routes = append(routes, account.Routes(verifier)...)
	if c.OpenAPI {
		routes = append(routes, documentationRoutes(httpapi.Specification(routes))...)
	}
	return routes
}
