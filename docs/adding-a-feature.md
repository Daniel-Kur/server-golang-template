# Adding a feature

Use this guide when adding endpoints such as `/services/*`, `/matching/*`, or
`/payment/*`. The worked example adds a protected `GET /services` endpoint.
The example is documentation only; it is not registered in the shipped template.
Commands below run from the repository root. Use your module path if you have
renamed `example.com/server-golang-template`.

## 1. Choose the boundary and contract

Create `internal/features/services/`. Keep route descriptors, handlers, schemas,
feature rules, and unit tests together. Start with `routes.go` and
`routes_test.go`; split handlers or schemas into separate files as they grow.
Add service/repository layers only when business logic or data access needs them.

Shared authentication belongs in `internal/auth`, shared HTTP behavior in
`internal/httpapi`, and validated configuration in `internal/config`. These
packages must not import feature packages or hard-code feature URLs/permissions.
The server composition layer connects features and infrastructure. Avoid importing
another feature's HTTP handlers; express cross-feature needs through a small
consumer-owned interface and inject the implementation.

Before implementation, decide the method, path, operation ID, request/response
shape, success status, errors, and access policy. For this example:

- `GET /services`, operation ID `listServices`, tag `Services`.
- Bearer authentication and the exact `services:read` permission.
- 200 with `{"success":true,"data":[]}` for an empty catalogue.
- No request body or persistence in this starter example.

An empty list is a successful read, not a 404. Replace the starter data source
with your actual service catalogue as a separate tested change.

## 2. Write and run a failing feature test

Create `internal/features/services/routes_test.go` first:

```go
package services

import (
    "context"
    "net/http"
    "net/http/httptest"
    "testing"

    "example.com/server-golang-template/internal/auth"
    "example.com/server-golang-template/internal/httpapi"
)

func TestListServicesAccess(t *testing.T) {
    for _, tc := range []struct {
        name        string
        token       string
        permissions []string
        status      int
    }{
        {name: "missing token", status: http.StatusUnauthorized},
        {name: "missing permission", token: "test-token", status: http.StatusForbidden},
        {name: "allowed", token: "test-token", permissions: []string{"services:read"}, status: http.StatusOK},
    } {
        t.Run(tc.name, func(t *testing.T) {
            // This deterministic verifier belongs only in tests.
            verifier := auth.VerifierFunc(func(context.Context, string) (auth.Principal, error) {
                return auth.Principal{Subject: "test-user", Permissions: tc.permissions}, nil
            })
            handler := httpapi.Router(Routes(verifier))
            r := httptest.NewRequest(http.MethodGet, "/services", nil)
            if tc.token != "" {
                r.Header.Set("Authorization", "Bearer "+tc.token)
            }
            w := httptest.NewRecorder()
            handler.ServeHTTP(w, r)
            if w.Code != tc.status {
                t.Fatalf("want %d, got %d: %s", tc.status, w.Code, w.Body)
            }
            if tc.status == http.StatusOK && w.Body.String() != "{\"success\":true,\"data\":[]}\n" {
                t.Fatalf("unexpected response: %s", w.Body)
            }
        })
    }
}
```

Run `go test ./internal/features/services -count=1`. Initially it fails because
`Routes` does not exist. For subsequent behavior changes, first reproduce the
missing or incorrect behavior with an assertion, rather than relying only on a
compilation failure. Implement the smallest change that passes, then refactor.

This feature-level test deliberately omits global middleware. Test its interaction
with global body limits, rate limits, HEAD handling, and CORS through `server.New`.

## 3. Implement the route and its OpenAPI schema together

Create `internal/features/services/routes.go`:

```go
// Package services exposes the service catalogue.
package services

import (
    "net/http"

    "example.com/server-golang-template/internal/auth"
    "example.com/server-golang-template/internal/httpapi"
)

func Routes(verifier auth.Verifier) []httpapi.Route {
    route := httpapi.Route{
        Method:      http.MethodGet,
        Path:        "/services",
        OperationID: "listServices",
        Summary:     "List services",
        Description: "Returns the service catalogue. Requires services:read. The starter implementation returns an empty list.",
        Tags:        []string{"Services"},
        Errors: []int{
            http.StatusRequestEntityTooLarge,
            http.StatusTooManyRequests,
            http.StatusInternalServerError,
            http.StatusServiceUnavailable,
        },
        Response: map[string]any{
            "type": "object", "required": []string{"success", "data"},
            "additionalProperties": false,
            "properties": map[string]any{
                "success": map[string]any{"const": true},
                "data": map[string]any{
                    "type": "array",
                    "items": map[string]any{"type": "string"},
                },
            },
        },
        Handler: func(w http.ResponseWriter, r *http.Request) {
            // Use a non-nil slice so the JSON value is [], not null.
            httpapi.Success(w, []string{})
        },
    }
    return []httpapi.Route{auth.Protect(route, verifier, "services:read")}
}
```

Run the feature test again. Keep schemas next to the endpoint and model the whole
success envelope. Response schemas document behavior; they do not validate Go
responses at runtime. Tests must check the actual payload.

`auth.Protect` authenticates first, then requires every listed permission. It also
adds OpenAPI bearer security, `x-required-permissions`, and 400/401/403/503 errors.
Those permission names are application permissions, not OAuth scopes.

- For authentication without a permission requirement, use
  `auth.Protect(route, verifier)`.
- For an intentionally public endpoint, return the unwrapped route and document
  that decision. Do not make routes public merely to simplify local testing.
- Never set `Route.Authenticated` or `Route.Permissions` alone and assume access
  is enforced. Use the wrapper.
- Use `auth.FromContext(r.Context())` when the handler needs the verified identity;
  check its boolean result. Enforce ownership and tenant boundaries in the service
  before accessing data, even after route permissions pass.

The shipped executable has no real verifier. Protected calls return 401 without
credentials and 503 with a syntactically valid bearer token until one is injected.
Follow the [authentication integration guide](authentication.md) to implement a
real verifier and pass `server.WithVerifier(verifier)` to `New` or `Run`. Never
put the test verifier above in application code.

## 4. Register the feature centrally

In `internal/server/routes.go`, import:

```go
"example.com/server-golang-template/internal/features/services"
```

Inside `applicationRoutes`, append the feature **before** the `if c.OpenAPI` block:

```go
routes = append(routes, services.Routes(verifier)...)
```

Registering after specification generation would omit the feature from the docs.
Keep `server.New` responsible for shared middleware composition; do not build a
second server or bypass its router inside the feature.

For dependencies beyond the verifier, define a narrow interface in the feature,
construct its implementation at the composition boundary, and pass it explicitly
through server options, `applicationRoutes`, and the feature's `Routes` function.
Do not use package-level database clients or mutable globals. Blocking methods
accept request context and propagate cancellation. Add configuration validation,
`.env.example`, and README entries when introducing environment settings.
New endpoints remain rate-limited; the only current exemption is health liveness.

## 5. Test registration and generated documentation

Add `internal/server/services_test.go` with an application-level contract test:

```go
package server

import (
    "context"
    "encoding/json"
    "net/http"
    "testing"

    "example.com/server-golang-template/internal/auth"
)

func TestServicesRegistration(t *testing.T) {
    verifier := auth.VerifierFunc(func(context.Context, string) (auth.Principal, error) {
        return auth.Principal{Subject: "test-user", Permissions: []string{"services:read"}}, nil
    })
    app := New(testConfig(t, nil), quiet(), WithVerifier(verifier))
    w := request(app, http.MethodGet, "/services", "", map[string]string{
        "Authorization": "Bearer test-token",
    })
    if w.Code != http.StatusOK {
        t.Fatalf("route not wired correctly: %d %s", w.Code, w.Body)
    }

    docs := request(app, http.MethodGet, "/openapi/json", "", nil)
    var spec struct {
        Paths map[string]map[string]struct {
            OperationID string                      `json:"operationId"`
            Security    []map[string][]string        `json:"security"`
            Permissions []string                     `json:"x-required-permissions"`
            Responses   map[string]json.RawMessage   `json:"responses"`
        } `json:"paths"`
    }
    if docs.Code != http.StatusOK {
        t.Fatalf("documentation status: %d", docs.Code)
    }
    if err := json.Unmarshal(docs.Body.Bytes(), &spec); err != nil {
        t.Fatal(err)
    }
    op := spec.Paths["/services"]["get"]
    if op.OperationID != "listServices" || len(op.Security) != 1 {
        t.Fatal("missing operation or authentication metadata")
    }
    if scopes, ok := op.Security[0]["bearerAuth"]; !ok || len(scopes) != 0 {
        t.Fatal("expected HTTP bearer security with no OAuth scopes")
    }
    if len(op.Permissions) != 1 || op.Permissions[0] != "services:read" {
        t.Fatal("wrong permission policy")
    }
    for _, status := range []string{"200", "400", "401", "403", "413", "429", "500", "503"} {
        if op.Responses[status] == nil {
            t.Errorf("undocumented status %s", status)
        }
    }
}
```

This uses existing helpers in the `server` test package. Run
`go test ./internal/server -run TestServicesRegistration -count=1` **before** adding
the registration line to observe the 404, then add registration and rerun.
The current `TestAPI` also asserts the number of OpenAPI paths: update that
expected count from 3 to 4 when adding `/services`, retaining the new explicit
operation assertion above. Do not relax unrelated assertions.

Extend the tests as real behavior is added: invalid/expired credentials, provider
outages, permission denial, validation failures, not found, duplicate conflicts,
resource/tenant isolation, cancellation, and concurrency where relevant. Assert
both HTTP status and JSON envelope; rejected requests must not perform writes.
Shared auth tests cover token parsing, but each feature must test its own policy.

## 6. Keep the API contract and handoff documentation current

For every added, changed, or removed endpoint, update descriptors and tests in the
same change. There is no separate OpenAPI YAML/JSON file to edit or generate.
`/openapi/json` is generated from the central registry at startup.

- Keep method/path, globally unique operation ID, tags, summary, description,
  parameters, request/response schemas, errors, and permissions aligned.
- Declare path parameters in `Route.Parameters` and read them with
  `r.PathValue("id")`; validate path/query values in the handler. A directory name
  does not create a URL prefix: every descriptor declares its full path.
- `ObjectSchema` currently validates optional string fields only. For richer
  request bodies, extend validation and schema support together with failing tests;
  do not merely document unsupported fields. Preserve strict decoding and limits.
- `httpapi.Success` and the current OpenAPI generator use 200. Before adding a
  201 or 204 endpoint, extend the generator and its tests to describe that actual
  success status. A 204 must have no body; creation may need a Location header.
- Use 409 for recognized database uniqueness conflicts, not all database errors.
  Unexpected errors remain generic 500 responses; never expose internal details.
- Include applicable global errors (413, 429, 500, 503) and feature errors in
  `Route.Errors`. Routing 404/405 and CORS preflight behavior remain shared concerns.
- Update README's endpoint table and usage guidance. Update AGENTS and these guides
  if architecture or conventions change, and the authentication guide if the
  identity integration changes.

With documentation enabled, run `make run`, inspect `/openapi/json`, and open
`http://localhost:3000/openapi`. Verify navigation, readable schemas, response
statuses, required permissions, and requests with permitted and denied test
credentials. Credentials require the real verifier integration; the UI does not
issue tokens. Do not commit live credentials.

## 7. Finish the quality gate

```sh
go test ./internal/features/services -count=1
go test ./internal/server -count=1
make check
```

`make check` verifies gofmt, golangci-lint, vet, race tests, at least 80% aggregate
coverage, and a production build. If formatting fails, run `make fmt` and review
the changes. Use writable GOCACHE/GOLANGCI_LINT_CACHE paths when needed, and do not
disable race tests to bypass environment restrictions. In a Git checkout, also
run `git diff --check`. Hand off the behavior, validation results, configuration
requirements, and any remaining integration work to the next developer.
