# Go server template

A small, production-minded Go HTTP API template built with the standard library.
No external runtime dependencies or code generation step.

Includes health and echo routes, shared validation/OpenAPI request schemas,
JSON response envelopes, structured `slog` logging, request IDs, exact-origin
CORS, security headers, reusable bearer authentication and permission checks,
bounded body sizes, a concurrent in-memory rate limiter,
graceful shutdown, golangci-lint checks, race tests, coverage enforcement, CI, and
a non-root container.

## Quick start

Requires Go 1.25 or newer and Make. Installing the linter also requires a POSIX
shell, curl, tar, and a SHA-256 utility (shasum, sha256sum, or openssl). Use a
currently supported Go release with its latest security patches for deployments. The module's minimum version is
1.25; development verification was performed with Go 1.25.5.

```sh
cp .env.example .env
# Go does not automatically load .env. Export this trusted local file:
set -a
. ./.env
set +a
make run
```

The server listens on `http://localhost:3000`. Configuration comes only from
the process environment; there is no implicit `.env` dependency. The example
file is shell-compatible. Source only files you trust. Without any environment
variables, `make run` also works with development defaults.

```sh
curl http://localhost:3000/health
curl http://localhost:3000/health/uptime
curl -X POST http://localhost:3000/health \
  -H 'Content-Type: application/json' -d '{"source":"example"}'
```

Before adopting the template, replace `example.com/server-golang-template` in
`go.mod` and Go imports with your repository's module path, then run `go mod tidy`
and `make check`. There is no `go.sum` because there are no external modules.

## Commands

| Command | Purpose |
| --- | --- |
| `make run` | Run the server (restart manually after edits) |
| `make build` | Build `bin/server` |
| `make fmt` | Format Go code with `gofmt` |
| `make fmt-check` | Check formatting without changing files |
| `make install-lint` | Install the pinned golangci-lint binary locally |
| `make lint` | Run golangci-lint on all packages, including tests |
| `make vet` | Run `go vet` |
| `make test` | Run tests with the race detector |
| `make coverage` | Race tests and an enforced 80% aggregate statement coverage floor |
| `make check` | Formatting, lint, vet, race tests, coverage, and build; also used in CI |
| `make clean` | Remove build output, installed local tools, and coverage output |

`make lint` and `make check` automatically install golangci-lint **v2.15.0**
on first use into the ignored `bin/tools/golangci-lint-v2.15.0/` directory.
The version is pinned in `GOLANGCI_LINT_VERSION` in the Makefile; CI uses the same
installation target and quality gate. The official versioned installer verifies
the release archive's SHA-256 checksum. Initial installation requires internet
access; subsequent runs reuse the local binary. This development tool adds no
application module dependencies and does not change `go.mod` or `go.sum`.

`.golangci.yml` enables `bodyclose`, `durationcheck`, `errcheck`, `errorlint`,
`govet`, `ineffassign`, `nolintlint`, `staticcheck`, and `unused`, including in
tests. Formatting remains owned by `gofmt`; run `make fmt` to apply it.
Fix lint findings rather than suppressing them. Any necessary `nolint` directive
must name the specific linter and explain the exception.

View coverage locally with `go tool cover -html=coverage.out`. Coverage includes
all packages, including the process entry point; there are no coverage exclusions.
Coverage uses `-count=1` to avoid stale cross-package profiles from cached tests.
CI selects the latest Go 1.25 patch release; `go.mod` declares the language
minimum. Coverage measures Go statements across all packages. The race detector
needs a supported platform and C compiler (standard GitHub Ubuntu runners
include one). On a restricted machine,
set `GOCACHE` and `GOLANGCI_LINT_CACHE` to writable directories.

## Development workflow

Use test-driven development (TDD) for new features, behavior changes, and bug
fixes. Follow the red-green-refactor cycle:

1. **Red:** Write or update a test for the intended behavior before changing the
   implementation. Run it and confirm it fails for the expected reason. For bug
   fixes, first write a regression test that reproduces the bug.
2. **Green:** Make the smallest implementation change that passes the test.
3. **Refactor:** Improve the code while keeping the tests green. Test observable
   behavior, and never weaken assertions to accommodate incorrect behavior.

Run focused tests during each cycle, for example:

```sh
go test ./internal/server -run '^TestAPI$' -count=1
```

For behavior-preserving refactors, confirm the relevant tests pass before and
after the change. Add missing coverage before refactoring uncovered behavior.
Before handing off implementation changes, run `make check` for formatting,
linting, vet, race tests, the coverage threshold, and the production binary build.
See [AGENTS.md](AGENTS.md) for the complete development requirements.

### Keep OpenAPI documentation current

Update OpenAPI documentation in the same change as every API addition,
modification, or removal. Update the route descriptors and schemas beside the
feature implementation, including parameters, request and response payloads,
status codes, and errors. Update operation IDs, descriptions, and tags when their
meaning changes. Keep runtime request validation aligned with its schema.

Add or update API contract tests and verify that the generated `/openapi/json`
specification and `/openapi` interactive documentation reflect the final behavior.
Use development or test configuration with `OPENAPI_ENABLED=true` to inspect
them. Do not maintain a separate handwritten specification or defer documentation
to a later task. For changes that do not affect the API, confirm that no OpenAPI
update is needed.

## Structure

Start with the [new feature guide](docs/adding-a-feature.md) for a worked example
covering TDD, route registration, authentication, and OpenAPI contracts.

```text
cmd/server/main.go             Process configuration, logger, OS signals
internal/
  features/                   Feature-specific endpoints, schemas, and policies
    account/                  Protected current-account example
    health/                   Health endpoints and liveness exemption policy
  auth/                       Shared authentication and permission middleware
  config/                     Validated environment configuration
  httpapi/                    Shared routing, validation, envelopes, middleware
  server/                     Application composition and integration tests
    app.go                    Dependency wiring and middleware composition
    routes.go                 Central feature and documentation route registry
    run.go                    Listener, timeouts, shutdown lifecycle
    docs.*                    Embedded API reference UI and asset routes
docs/authentication.md        Identity-provider integration and handoff guide
docs/adding-a-feature.md      Feature implementation and registration walkthrough
.golangci.yml                 Explicit lint rules, including test files
.github/workflows/ci.yml       Same quality gate as local development
Dockerfile                    Static binary in a non-root scratch image
```

`server.New(config, logger)` returns an `http.Handler`; it never binds a port or
starts background goroutines. Most tests use `httptest.ResponseRecorder`.
Lifecycle tests use ephemeral loopback listeners. `server.Run` owns the
listener and drains active requests on `SIGINT`/`SIGTERM`; after the configured
shutdown deadline it closes remaining connections and exits with an error.

Keep feature-specific code in `internal/features/<feature>` (for example,
`services`, `matching`, or `payment`). Shared infrastructure stays in
`internal/auth`, `internal/httpapi`, and `internal/config`; `internal/server`
composes the application. Keep handlers, route descriptors, schemas, and tests
together in each feature package. Put HTTP parsing and responses in
handlers, business rules in services, and database or external API access in
repositories when those layers become necessary. Pass dependencies explicitly;
define small interfaces at the consuming package. Propagate request contexts to
blocking operations. Do not add empty layers to simple features.

Shared infrastructure must not import feature packages or hard-code feature URLs
or permissions. The server composition layer may import both and connect them.
For example, health owns `IsLivenessRequest`; `server.New` supplies it through
`httpapi.WithRateLimitExemption`. Shared middleware exempts nothing by default.
Keep feature unit tests beside the feature; application-level tests in
`internal/server` verify route registration, global middleware, authentication,
and generated OpenAPI together. Shared infrastructure tests use generic handlers.

To add a route, return an `httpapi.Route` from the feature and register it in
`applicationRoutes` in `internal/server/routes.go`. Pass feature dependencies
explicitly there; `server.New` applies shared middleware to the assembled router.
The registry also adds enabled documentation routes after generating OpenAPI
from the feature routes. Include an operation ID, summary, description, feature tags,
documented error status codes, and response schema.
The router uses the standard `http.ServeMux`, including method matching, path
parameters (`/items/{id}` and `r.PathValue("id")`), automatic HEAD support,
path canonicalization redirects, and startup checks for conflicting routes. Its
404/405 responses retain the JSON error envelope. Declare path parameters in
`Route.Parameters` when introducing parameterized API routes. The example
`ObjectSchema` intentionally handles optional string fields only; extend its decoder and schema together, or adopt an established
schema library when the API needs richer types. It is not a general JSON Schema
validator. Responses use Go types and co-located OpenAPI schemas; they are not
validated against those schemas at runtime. Object and string schema types and
the unknown-key rejection policy are fixed by their JSON marshaling methods,
so metadata cannot opt into behavior the decoder does not implement.

## API

| Method and path | Success status | Result |
| --- | --- | --- |
| `GET /health` | `200 OK` | Liveness: `{"success":true,"data":{"status":"ok"}}` |
| `GET /health/uptime` | `200 OK` | `uptimeMs`, `uptimeSeconds`, and UTC `startedAt` |
| `POST /health` | `200 OK` | `status`, `from: "POST"`, and `received` in the success envelope |
| `GET /account/me` | `200 OK` | Verified subject; requires bearer authentication and `profile:read` |
| `GET /openapi` | `200 OK` | Interactive documentation page when enabled |
| `GET /openapi/json` | `200 OK` | Generated OpenAPI 3.1 document when enabled |

`POST /health` is a diagnostic echo: it does not create or persist a resource,
so its success status is 200, not 201. There is no database-backed creation
endpoint in this template yet. HEAD requests to GET routes return the same
status and representation headers without a response body.

POST accepts `{}` or `{"source":"example"}` with an optional string of 1–100
Unicode code points. Unknown/duplicate keys, nulls, invalid JSON, trailing values,
and invalid field types produce 422. JSON media type is required (415 otherwise).
Oversized known-length and chunked POST bodies produce 413. Request bodies are
never included in error messages.

Errors have one envelope:

```json
{"success":false,"error":{"code":"VALIDATION_ERROR","message":"Request validation failed"}}
```

Expected errors use `httpapi.Error` and the centralized `httpapi.WriteError`,
which supports wrapped errors and optional, deliberately public `details`.
`httpapi.Failure` is a convenience helper for errors without details.
Unexpected panics before headers are committed become generic 500 responses; after commitment, the
connection is aborted because a second response cannot safely be written.
Recovery removes stale entity headers before sending JSON. JSON helpers set the
encoded content length, including for HEAD responses whose bodies are suppressed.

### HTTP response code guidelines

The actual HTTP status must reflect the outcome. Never return 200 or another
success status for a failed operation, even when the body has `success: false`.
Use the named `net/http` status constants in Go code.

| Status | When to use it |
| --- | --- |
| `200 OK` | Successful read, update, or other operation returning a body |
| `201 Created` | A resource was created; include `Location` when its URL is available |
| `204 No Content` | Successful operation with no response body; do not send a JSON envelope |
| `400 Bad Request` | Malformed or unreadable request, subject to the endpoint's documented validation contract |
| `401 Unauthorized` | Missing or invalid authentication; include the appropriate `WWW-Authenticate` challenge |
| `403 Forbidden` | Access is forbidden; also used for rejected CORS preflight requests |
| `404 Not Found` | Route or requested resource does not exist |
| `405 Method Not Allowed` | The path exists but does not support the method; include `Allow` |
| `409 Conflict` | Conflict with existing state, such as a recognized duplicate primary key or unique value |
| `413 Content Too Large` | Request body exceeds the configured limit |
| `415 Unsupported Media Type` | Request content type is unsupported |
| `422 Unprocessable Content` | Request fails the endpoint's validation rules |
| `429 Too Many Requests` | Client quota exceeded; include `Retry-After` |
| `500 Internal Server Error` | Unexpected failure; return a generic message without internal details |
| `503 Service Unavailable` | Temporary inability to serve; the bounded rate limiter uses this when its client store is full |

`httpapi.Success` writes 200. For a different success status with a body, use
`httpapi.JSON(w, http.StatusCreated, httpapi.Envelope[YourType]{Success: true,
Data: value})`. For 204, use `w.WriteHeader(http.StatusNoContent)` without writing
a body. Complete database writes before committing a success response.

Translate recognized database constraint violations into domain conflicts using
driver error codes and relevant constraint identifiers, not string matching.
Handle the write's error even after checking for an existing record, because
concurrent requests can race. Map a duplicate conflict explicitly:

```go
httpapi.WriteError(w, &httpapi.Error{
    Status:  http.StatusConflict,
    Code:    "ALREADY_EXISTS",
    Message: "A record with this identifier already exists.",
})
```

Do not map all database errors to 409. Unexpected errors passed to
`httpapi.WriteError` produce a generic 500. The helper does not automatically
recognize database driver errors, and no database integration is included.

The current health endpoints document 200 successes and their applicable errors
in OpenAPI. The diagnostic POST preserves its established contract: malformed
JSON and invalid fields return 422, body-read failures return 400, wrong media
types return 415, and oversized bodies return 413. Quota and limiter-capacity
failures return 429 and 503 for rate-limited operations. GET/HEAD `/health` are
exempt from quotas. Unknown routes and unsupported methods return JSON 404/405;
accepted CORS preflights return an empty 204 and rejected preflights return 403.
These routing and preflight responses are not separate business API operations
in the specification. Documentation pages/assets are likewise not API operations.

For every new endpoint, test both the HTTP status and envelope on success and
failure, including duplicate conflicts for database-backed creation. Update
`Route.Errors` and the response schemas in the same change. The current OpenAPI
generator declares 200 for successful operations; extend it to describe the
actual success status and body rules before introducing a 201 or 204 endpoint.
Do not add 201 or 409 responses to the diagnostic echo, which never creates data.

OpenAPI paths and operations come from the registered routes. The diagnostic
request schema is shared with runtime validation. `/openapi` generates
searchable endpoint navigation and readable schema tables with field types,
required flags, and constraints. Each endpoint includes example responses, a
response-status selector, and a request console with JSON formatting, cURL
commands, response timing, and headers. Requests stay on this origin and time out
after 30 seconds. The responsive layout works on desktop and narrow screens.
Embedded assets are served at `/openapi/app.js` and `/openapi/style.css`; all
documentation assets default off in production.
This lightweight UI uses no CDN or inline scripts. Import the specification into
Scalar or another OpenAPI client for additional tooling and client generation.
Liveness checks indicate process responsiveness, not dependency
readiness; add a separate readiness endpoint when dependencies are introduced.

## Authentication and authorization

The security layer spans global middleware in `internal/httpapi` and reusable
route authentication/authorization in `internal/auth`. The protected example
`GET /account/me` requires a verified bearer token with `profile:read`. Health
and enabled documentation routes are public.

**A real identity provider must still be integrated.** The executable has no
configured verifier, login flow, token issuer, or JWT validation implementation.
It fails closed: missing credentials return 401 and a syntactically valid bearer
token returns 503 until a verifier is injected. There is no development bypass.

Implement the concurrency-safe `auth.Verifier` interface, then pass
`server.WithVerifier(verifier)` to `server.Run` or `server.New`.
`auth.Protect(route, verifier, "items:write")` enforces authentication and all
listed permissions while updating OpenAPI security metadata. Without permission
arguments it requires authentication only. Retrieve the verified identity with
`auth.FromContext(r.Context())`; enforce ownership and tenant boundaries in your
feature/service as well.

Missing/invalid credentials return 401 with a Bearer challenge, malformed bearer
headers return 400, missing permissions return 403, and verifier failures return
503. Auth responses use `Cache-Control: no-store`; credentials are never logged.
Only the Authorization header is accepted, not cookies or query parameters.
Bearer tokens require HTTPS in deployed environments.

The documentation UI displays required permissions and provides a masked token
input for protected endpoints. It does not persist the token, and cURL previews
use `$TOKEN`. The UI cannot issue tokens. OpenAPI uses HTTP bearer security and
`x-required-permissions` for application permissions.

The developer taking over should follow the **[authentication integration guide](docs/authentication.md)**.
It explains provider validation, startup wiring, new protected routes, exact
HTTP contracts, test cases, deployment requirements, and what remains to build.
Follow TDD and update OpenAPI in the same change for every new policy or endpoint.

## Configuration

Configuration is validated once before binding a port. Booleans must be exactly
`true` or `false`; explicitly empty values are invalid except `CORS_ORIGINS`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `APP_ENV` | `development` | `development`, `test`, or `production` |
| `HOST` | `0.0.0.0` | Bind address; IPv6 supported |
| `PORT` | `3000` | Port, 1–65535 |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `OPENAPI_ENABLED` | `true` outside production | Enable documentation and specification |
| `CORS_ORIGINS` | `http://localhost:3000` outside production; empty in production | Comma-separated exact HTTP(S) origins; no wildcards or paths |
| `CORS_CREDENTIALS` | `true` | Allow credentials for approved origins |
| `BODY_LIMIT_BYTES` | `1048576` | Maximum body bytes, 1–1073741824 |
| `RATE_LIMIT_MAX` | `100` | Requests per client per fixed window, 1–1000000 |
| `RATE_LIMIT_CLIENTS` | `10000` | Maximum tracked clients per window, 1–1000000 |
| `RATE_LIMIT_WINDOW_MS` | `60000` | Window in milliseconds, 1–86400000 |
| `TRUST_PROXY` | `false` | Use the first `X-Forwarded-For` entry if it is a valid IP |
| `SHUTDOWN_TIMEOUT_MS` | `10000` | Drain timeout in milliseconds, 1–86400000 |

Only GET/HEAD `/health` bypass rate limiting. POST diagnostics consume quota.
Quotas use a shared fixed window per handler
instance; bursts are possible across window boundaries. Entries are cleared on
the first request after expiry. Capacity exhaustion returns 503 until the next
window, without evicting active quotas. Exceeded quotas return 429. Both include
`Retry-After`; rate headers expose limit, remaining requests, and seconds to reset.
The store is process-local. Use a shared limiter when quotas must span replicas.

Forwarding headers are ignored by default. Enable `TRUST_PROXY` only behind a
trusted proxy that overwrites `X-Forwarded-For` and blocks direct application
access. Provider-specific headers such as `CF-Connecting-IP` are not trusted.
CORS is a browser policy, not authentication. Protect application routes with
`auth.Protect` and inject a real token verifier. Preflight requests validate origins, methods, and headers.

Logs include request ID, method, matched route pattern, status, duration, and an
aborted flag, omitting bodies, headers,
query strings, raw paths, and panic values. Safe supplied `X-Request-ID` values
(1–128 ASCII letters, digits, dots, underscores, or hyphens) are preserved;
otherwise a random ID is generated. Process logs also include service and
environment attributes. Matched route patterns identify operations without
logging actual path parameter values; unmatched requests have no route pattern.
Handlers can use `httpapi.RequestID(ctx)`. Server errors (5xx) and aborted requests
are logged at error level, so `LOG_LEVEL=error` retains failure visibility.
Aborted requests retain the committed status (or 0 if no response was committed).

The HTTP server sets a 5s header read timeout, 15s request read timeout, 30s write
timeout, 60s idle timeout, and 1 MiB header limit. These are network deadlines;
services still need context-aware outbound operations and their own time budgets.
Revisit these settings when adding streaming or large uploads.

## Container deployment

```sh
docker build -t server-golang-template .
docker run --rm -p 3000:3000 \
  -e CORS_ORIGINS=https://app.example.com server-golang-template
```

The image defaults to production, runs as UID/GID 65532, and includes CA roots
for outbound HTTPS. It has no shell. Terminate TLS at a trusted ingress; production
responses emit HSTS. Configure your orchestrator's HTTP liveness probe for
`/health` and allow a termination grace period longer than the shutdown timeout.
Do not reuse the development `.env` wholesale in production: it explicitly
enables documentation. Build with a maintained Go image and pin its digest in
your deployment pipeline when immutable base images are required.

Standard-library references: [HTTP server and shutdown](https://pkg.go.dev/net/http),
[structured logging](https://pkg.go.dev/log/slog), and
[module layout guidance](https://go.dev/doc/modules/layout).
