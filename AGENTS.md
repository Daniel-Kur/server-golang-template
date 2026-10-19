# Repository guidance

Applies to the entire repository. Keep this file and README.md aligned with the
implemented architecture, commands, configuration, and API contracts. Document
this as a standalone Go project, with self-contained setup and usage guidance.

## Go and architecture

- Follow `docs/adding-a-feature.md` when adding a feature; keep its examples and
  registration/testing instructions aligned with the implementation.

- Use Go 1.25 or newer, Go modules, `gofmt`, `golangci-lint`, `go vet`, and
  standard `testing`.
- Prefer the standard library. Add dependencies only for a concrete requirement;
  use `go get` and `go mod tidy`, and commit go.sum when dependencies require it.
- The module path is `example.com/server-golang-template` until adoption. Update
  go.mod and all imports together when renaming it.
- `cmd/server` owns environment loading, logger construction, and signal handling.
- `internal/server.New` composes an `http.Handler` without binding sockets or
  launching goroutines. `Run` owns listening, timeouts, and shutdown.
- Keep feature-specific handlers, routes, schemas, and tests under
  `internal/features/<feature>`. Shared authentication stays in `internal/auth`,
  cross-cutting HTTP concerns in `internal/httpapi`, and validated configuration
  in `internal/config`. Do not put feature business logic in shared infrastructure.
- Centralize route registration and feature dependency injection in
  `internal/server/routes.go` (applicationRoutes). Feature packages define their
  own descriptors and access policies; server.New composes shared middleware and
  the router. Generate OpenAPI from feature routes before adding documentation
  routes. New features such as services, matching, or payment belong under
  internal/features; do not add placeholder packages or empty layers.
- Shared auth, HTTP, and configuration packages must not import features or
  hard-code feature URLs/permissions. The server composition layer imports and
  connects both. Health owns IsLivenessRequest; server.New injects it with
  httpapi.WithRateLimitExemption. Middleware defaults to no exemptions; exemptions
  must not bypass body limits, security headers, or other protections.
- Keep feature unit tests beside features and shared middleware tests generic.
  Application integration tests stay in internal/server to verify registration,
  middleware, authentication, and OpenAPI together.
- Pass dependencies explicitly; define small interfaces at the consumer. Avoid
  mutable globals, generic utility packages, speculative abstractions, and empty
  service/repository layers. Introduce those layers when business logic needs them.
- Pass context.Context first to blocking service/repository methods; do not store
  request contexts in structs. Wrap errors with `%w` and use errors.Is/As.
- Do not panic for expected errors or call os.Exit outside the process entry point.
- Keep goroutine ownership and cancellation explicit; always join shutdown work.

## Test-driven development

- Adopt TDD for new features, behavior changes, and bug fixes throughout the
  development lifecycle. Follow the red-green-refactor cycle.
- Before changing implementation, write or update a behavior-focused test and
  run it to confirm it fails for the intended reason. For a bug fix, first add
  a regression test that reproduces the bug.
- Implement the smallest change that makes the test pass, then refactor while
  keeping the tests green. Test observable behavior rather than implementation
  details; do not weaken assertions to accommodate incorrect behavior.
- For behavior-preserving refactors, confirm the relevant tests pass before and
  after the change. Add missing coverage before refactoring uncovered behavior.
- Run focused tests during each cycle and the full `make check` quality gate
  before handing off implementation changes.

## HTTP contracts

- Success: `{ "success": true, "data": ... }`.
- Error: `{ "success": false, "error": { "code": "...", "message": "..." } }`.
- Return the HTTP status that matches the actual outcome. Never return 200 or
  another success status for a failed operation, even if the JSON body says
  `success: false`. Use the named net/http status constants in new code.
- Use 200 for successful operations with a response body, 201 when a resource is
  created (include Location when its URL is available), and 204 for successful
  operations with no response body. Never send a JSON envelope with 204.
  httpapi.Success currently writes 200; use httpapi.JSON with a success envelope
  for other body-bearing success statuses, or WriteHeader for 204.
- Use appropriate error statuses: 400 for malformed requests, 401 for missing or
  invalid authentication, 403 for forbidden access, 404 for missing resources,
  409 for conflicts with existing state, 413 for oversized bodies, 415 for
  unsupported media types, 422 for request validation failures, and 429 for
  exceeded quotas. Preserve established endpoint contracts, including the
  diagnostic POST endpoint's 422 response for invalid JSON.
- Translate recognized database primary-key or unique-constraint violations into
  a domain conflict and return 409 through httpapi.Error/httpapi.WriteError.
  Identify errors using driver error codes and relevant constraint identifiers,
  not string matching. Do not map every database failure to 409 or expose database
  details; unexpected failures must return a generic 500. Perform writes before
  committing success headers, and handle constraint violations even if a prior
  existence check passed, since concurrent requests can race.
- Add tests asserting the actual HTTP status and response body for success and
  failure paths, including duplicate records. Keep OpenAPI response statuses and
  schemas aligned with those tests; extend the specification generator when a
  new success status is needed rather than documenting every success as 200.
- Use typed Go responses and shared envelope helpers. Use httpapi.Error for
  operational failures. Never expose internal errors or panic values to clients.
- Register routes with stable operation IDs, summaries, descriptions, request
  schemas, response schemas, feature tags, and error status codes. Keep metadata
  next to the feature implementation.
- Generate OpenAPI from route descriptors; do not commit a separately maintained
  specification. Runtime request validation and request schema must stay aligned.
- Always keep OpenAPI documentation current. Every API change must update the
  relevant route descriptors and schemas in the same change, including added,
  modified, and removed endpoints, parameters, payloads, status codes, and errors.
  Update operation IDs, descriptions, and tags when their meaning changes.
- Add or update contract tests for API changes, and verify that the generated
  `/openapi/json` specification and `/openapi` documentation reflect the final
  behavior. Do not defer documentation updates to a later task. For changes that
  do not affect the API, confirm that no OpenAPI update is needed.
- ObjectSchema supports only optional string fields. Do not imply it implements
  arbitrary JSON Schema; extend validation and schema together when required. Its
  MarshalJSON methods fix object/string types and unknown-property rejection to
  match the decoder; do not expose unsupported schema options.
- Response schemas are documentation, not runtime validation. Test their contracts.
- Preserve strict JSON decoding, duplicate/unknown-key rejection, media-type
  validation, body limits (including unknown-length bodies), and error envelopes.
- Use http.ServeMux for routing and PathValue for path parameters. Preserve JSON
  404/405 responses, HEAD semantics, and standard path canonicalization.
- Use httpapi.WriteError for expected errors, including wrapped errors. Error
  details are public; never put internal error objects or secrets in them.
- ResponseWriter wrappers must preserve informational responses and commitment
  tracking through http.ResponseController.Flush. Panic recovery must discard stale
  entity headers before writing JSON. HEAD must retain representation headers.

## Authentication and authorization

- `internal/auth` owns provider-independent bearer authentication and exact
  permission checks; `internal/features/account` is the protected example. Read
  `docs/authentication.md` before implementing provider integration or new policies.
- Inject a concurrency-safe `auth.Verifier` through `server.WithVerifier` into
  New/Run. The shipped executable has no provider and fails closed. Never add a
  hard-coded credential, accept-all verifier, or development authentication bypass.
- Verifiers return trusted Principal values only after validating credentials.
  Invalid/expired/revoked tokens return or wrap auth.ErrInvalidToken (401);
  outages and other verification failures return another error (generic 503).
  Preserve cancellation and use explicit outbound deadlines. Validate signature,
  issuer, audience, expiry, and permitted algorithms for JWTs with a maintained
  library; decoding alone is never verification. Keep secrets out of errors/logs.
- Prefer auth.Protect for route registration: authentication runs before all
  required permission checks and OpenAPI metadata is added together. Metadata
  fields alone do not enforce access. Permissions match exactly, with no wildcard
  or implicit admin bypass. No permission arguments means authenticated access.
- Use auth.FromContext to retrieve verified identity. Independently enforce
  resource ownership and tenant boundaries in services before accessing data.
- Preserve header-only credentials, no-store responses, Bearer challenges, and
  400/401/403/503 distinctions. Keep health public and CORS preflights unauthenticated.
- Document protected operations using HTTP bearer security and
  x-required-permissions; do not describe application permissions as OAuth scopes.
  Keep the documentation UI's permission display and ephemeral masked token input;
  never persist tokens or put them into cURL previews, URLs, or logs.
- Follow TDD for provider adapters and policies. Cover invalid/expired tokens,
  outages, cancellation, permission denial, successful access, concurrent use,
  and resource/tenant isolation. Fake verifiers belong only in tests.
- Keep README, this file, the integration guide, and generated OpenAPI aligned.
  Update validated configuration and .env.example when adding provider settings.

## Security and operations

- Preserve exact-origin CORS, security headers, generated/validated request IDs,
  structured logs, body limits, network timeouts, and bounded concurrent rate limits.
- Never log secrets, request bodies, Authorization/Cookie headers, query strings,
  raw paths, or arbitrary panic values. Correlate failures using request IDs.
- Log 5xx and aborted requests at error level, including requests aborted after
  headers were committed. Preserve the actual committed status and aborted flag.
- TRUST_PROXY defaults false. It requires ingress that overwrites X-Forwarded-For
  and prevents direct application access. Do not trust other forwarded headers.
- Documentation defaults disabled in production. Keep its interactive page and
  embedded JavaScript/CSS self-contained, generated from OpenAPI, with same-origin
  requests and no inline/third-party script dependency. Preserve searchable
  navigation, readable schema tables, response examples, keyboard accessibility,
  and responsive layout. Render API content as text, never untrusted HTML.
- Only GET/HEAD liveness bypasses quota; diagnostic POST remains limited.
- The rate limiter is process-local. Document distributed requirements before
  changing deployment topology. Keep memory bounded and test capacity behavior.
- Do not add credentials, local environment files, build output, or coverage data.

## Verification and documentation

- Prefer table-driven request tests with httptest and injected configuration.
  Use loopback listeners only for lifecycle/integration behavior.
- Cover successes, invalid input, error envelopes, headers, production defaults,
  and concurrency-sensitive changes. Never weaken tests or coverage to pass.
- Run `make check` before handoff: formatting, lint, vet, race tests, at least 80%
  aggregate statement coverage across all packages, and a production binary build.
  Keep `-count=1` on coverage runs to avoid stale cross-package coverage profiles.
- Use writable GOCACHE and GOLANGCI_LINT_CACHE directories when default caches
  are restricted. Do not disable the race detector to hide a race. Report
  unavailable platform-dependent checks.
- Run `make lint` for the configured golangci-lint checks, including test files.
  The Makefile pins v2.15.0 and installs the official checksum-verified binary
  under ignored bin/tools on demand; CI uses `make install-lint` and `make check`.
  Keep the version pin, configuration, and README aligned when upgrading.
- Keep gofmt as the formatter. Fix lint findings without weakening the explicit
  linter list or excluding tests. A necessary nolint directive must name its
  linter and explain the reason; prefer handling errors explicitly.
- Do not commit the installed linter or add it to the application's go.mod.
- If this directory is a Git checkout, also run `git diff --check`.
- Update `.env.example` and the README configuration table for every environment
  change. Keep CI, Dockerfile, go.mod, and documented Go requirements compatible.
  CI resolves the latest 1.25 patch; go.mod declares the language minimum.
- Update README setup, architecture, API, and deployment guidance when behavior
  changes. Describe the Go implementation directly without comparisons to other
  templates or assumptions about sibling repositories.
