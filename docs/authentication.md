# Authentication and authorization integration guide

This is the handoff guide for the developer adopting this template. The reusable
middleware and protected example are implemented. Choosing and connecting a real
identity provider is still required. There is no login endpoint, token issuer,
password database, session store, refresh flow, or built-in JWT validator.
No token, environment flag, or development password bypasses verification.

## Existing architecture

- `internal/auth`: `Verifier`, `Principal`, bearer authentication, permission
  middleware, context access, and `Protect` for enforcement plus OpenAPI metadata.
- `internal/features/account`: `GET /account/me`, requiring `profile:read`, returns the
  verified subject in the standard success envelope.
- `internal/server`: inject a verifier with `WithVerifier` into `New` (tests or
  embedding) or `Run` (the executable).
- `internal/server/routes.go`: central route registry; passes dependencies to
  feature Routes functions and registers enabled documentation routes. Shared
  auth stays outside internal/features.
- `cmd/server/main.go`: construct the production verifier here and pass it to Run.

Shared auth and HTTP packages do not import features or know their URLs or
permission names. Features choose policies; the server composition layer wires
those policies and dependencies. For example, health owns `IsLivenessRequest`,
which `server.New` injects via `httpapi.WithRateLimitExemption`; the shared limiter
has no built-in endpoint exemptions. Feature unit tests live beside each feature;
server integration tests verify the assembled middleware and OpenAPI contracts.

Global request logging, recovery, CORS, rate limiting, and body limits run before
route authentication. Approved CORS preflights do not require a token. Health and
available documentation routes remain public. CORS is not authorization.

`Authenticate(verifier)(RequirePermissions("items:write")(handler))` authenticates
before checking permissions. Prefer `auth.Protect(route, verifier, "items:write")`
for registered application routes: it applies that order and declares the same
policy in OpenAPI. `Protect(route, verifier)` requires authentication only.
All listed permissions must match exactly and case-sensitively. There are no
wildcards or implicit administrator privileges. Empty permission names deny.
The permission list and identity slices are copied to prevent accidental mutation.

## 1. Implement and test a verifier

Add a provider adapter such as `internal/identity`. Implement:

```go
Verify(ctx context.Context, token string) (auth.Principal, error)
```

The adapter must be safe for concurrent requests. It receives the original
request context; use it for outbound calls and set a bounded verification timeout.
Use a reused HTTP client with explicit timeouts if verification needs a network.

Return a nonempty, stable subject and trusted application permissions **only after
successful verification**. Return `auth.ErrInvalidToken` (or wrap it with `%w`)
for invalid, expired, or revoked credentials. Return another error for provider
outages, key-fetch failures, or configuration failures; the middleware returns a
generic 503. Never return a partial identity as a successful fallback.

For JWT access tokens, use a maintained provider SDK or JWT library rather than
implementing cryptography. Explicitly validate the allowed algorithm, signature,
trusted issuer, intended audience, expiration, and applicable not-before rules.
Reject ID tokens or tokens for another API. Configure trusted key locations;
do not follow arbitrary token-controlled key URLs. Define key rotation, caching,
clock-skew bounds, and revocation requirements for your deployment. Merely decoding
a JWT does not authenticate it.

For opaque tokens, validate through the provider's supported introspection or
lookup mechanism, including active state, expiry, issuer/audience as applicable,
and the intended API. Translate verified claims or server-side role assignments
into the application's permissions. Never accept permissions from request JSON,
unverified token claims, or arbitrary headers.

Write failing adapter tests before implementation: valid token, expired/revoked
or invalid token, wrong issuer/audience/signature/algorithm where applicable,
provider outage, cancellation, timeouts, claim mapping, and concurrent use.
Add dependencies with `go get` and `go mod tidy` and commit go.sum if introduced.

## 2. Wire the provider at startup

Validate provider configuration in `internal/config`, and update its tests,
`.env.example`, and README's configuration table. No provider-specific variables
exist today. Obtain secrets through your deployment's secret management.
Fail startup for invalid required provider configuration.

After constructing your concrete verifier in `cmd/server/main.go`, change the
existing Run call to:

```go
return server.Run(ctx, c, logger, server.WithVerifier(verifier))
```

Tests and embedders use:

```go
handler := server.New(c, logger, server.WithVerifier(verifier))
```

`verifier` in these snippets is the real adapter you constructed, not an included
factory. Without injection, missing credentials receive 401 and a syntactically
valid bearer token receives 503. The shipped executable deliberately has this
behavior. Never ship an accept-all verifier or a hard-coded token.

## 3. Protect new routes

For a complete feature, route registration, and test example, follow the
[new feature guide](adding-a-feature.md).

Create or extend `internal/features/<feature>` (such as services, matching, or
payment). Keep its handlers, schemas, access policies, and tests together.
Build your normal feature `httpapi.Route` with schemas, stable operation ID, tags,
and error statuses. Then wrap it before returning/registering it:

```go
protected := auth.Protect(route, verifier, "items:write")
```

Register the feature's `Routes` in `applicationRoutes` in
`internal/server/routes.go`. Pass the verifier explicitly there, as account does;
keep shared middleware composition in `internal/server/app.go`.
Use `auth.FromContext(r.Context())` in the protected handler to access the identity.
Check the returned boolean; never trust a user ID supplied by the caller as the
identity. Do not set `Route.Authenticated` or `Route.Permissions` manually as a
substitute for enforcement. Those fields describe the middleware policy.

Permission checks are only the route-level gate. Services must still check
resource ownership, tenant boundaries, and operation-specific rules before data
access. A caller with `items:write` cannot automatically modify every user's item.
Introduce tenant identity and tests when multi-tenancy is implemented. Use 403,
or a consistently applied 404 policy when hiding resource existence.

## HTTP and OpenAPI contract

Send one `Authorization: Bearer <access-token>` header. Cookies and query strings
are ignored as credentials. Tokens must use RFC 6750 bearer characters and be at
most 8192 bytes. Tokens are never echoed or logged by the middleware.

| Outcome | HTTP status | Error code / challenge |
| --- | --- | --- |
| Missing credentials or another authentication scheme | 401 | `UNAUTHORIZED`; `WWW-Authenticate: Bearer` |
| Malformed bearer header or multiple Authorization headers | 400 | `INVALID_AUTH_REQUEST`; Bearer `invalid_request` |
| Invalid, expired, or revoked token | 401 | `INVALID_TOKEN`; Bearer `invalid_token` |
| Verified identity missing any permission | 403 | `FORBIDDEN`; Bearer `insufficient_scope` |
| Missing verifier, provider failure, or empty verified subject | 503 | `AUTH_UNAVAILABLE` |
| Valid identity and permissions on `/account/me` | 200 | `{"success":true,"data":{"subject":"..."}}` |

Auth responses set `Cache-Control: no-store`. Earlier global middleware can still
reject requests with 413, 429, or 503. All failures retain the JSON error envelope.
Authentication does not mint tokens or automatically refresh them.

`Protect` adds 400/401/403/503 error responses and operation-level `bearerAuth`
security. The generated scheme is HTTP bearer, without claiming JWT format.
`x-required-permissions` lists application permissions; HTTP bearer security
requirements have an empty scopes array because these are not OAuth scopes.
Public routes do not acquire a global security requirement.

The local `/openapi` UI displays required permissions and a masked bearer-token
field on protected endpoints. It sends tokens only to the same origin, disallows
redirects, and requires HTTPS except on loopback development hosts. The field
lives only in the selected form, is reset when selecting another endpoint, and is
not saved to browser storage. cURL previews use `$TOKEN` instead of the entered
secret. Use test credentials; browser developer tools can still inspect requests.
The UI cannot obtain a token for you. Documentation is disabled by default in
production. Terminate TLS at a trusted ingress for all deployed bearer requests;
the Go middleware relies on deployment TLS, not forwarding-header guesses.

For a real token issued by your configured provider, use a shell variable supplied
securely by your environment:

```sh
curl -H "Authorization: Bearer $TOKEN" https://api.example.com/account/me
```

## Acceptance checklist for the integrating developer

1. Follow red-green-refactor for the provider, route policy, and resource checks.
2. Test missing/malformed/invalid/expired credentials, outages, denied permissions,
   successful access, cross-user/cross-tenant denial, HEAD, and CORS preflight.
3. Use deterministic fake verifiers only in tests. Keep a separate integration
   test against the chosen provider's test environment; do not depend on it for
   unit tests or commit live credentials.
4. Update route schemas, status codes, permission metadata, README, and AGENTS in
   the same change. Inspect `/openapi/json` and exercise `/openapi` with a test token.
5. Run `make check` including race tests. Deploy with HTTPS and validated provider
   configuration, then verify both permitted and denied calls.

References: [Bearer token usage (RFC 6750)](https://www.rfc-editor.org/rfc/rfc6750)
and [OpenAPI security schemes](https://spec.openapis.org/oas/v3.1.0.html#security-scheme-object).
