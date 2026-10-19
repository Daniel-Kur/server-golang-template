package server

import (
	"embed"
	"net/http"

	"example.com/server-golang-template/internal/httpapi"
)

//go:embed docs.html docs.js docs.css
var documentation embed.FS

func documentationRoutes(spec any) []httpapi.Route {
	return []httpapi.Route{
		{Method: "GET", Path: "/openapi/json", Handler: func(w http.ResponseWriter, r *http.Request) { httpapi.JSON(w, 200, spec) }},
		{Method: "GET", Path: "/openapi", Handler: func(w http.ResponseWriter, r *http.Request) {
			// Assets and API requests stay on this origin; no inline or third-party scripts.
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			body, _ := documentation.ReadFile("docs.html")
			_, _ = w.Write(body)
		}},
		{Method: "GET", Path: "/openapi/style.css", Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			body, _ := documentation.ReadFile("docs.css")
			_, _ = w.Write(body)
		}},
		{Method: "GET", Path: "/openapi/app.js", Handler: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			body, _ := documentation.ReadFile("docs.js")
			_, _ = w.Write(body)
		}},
	}
}
