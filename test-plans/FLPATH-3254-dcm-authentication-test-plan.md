# Test Plan: DCM IDM/IAM Authentication

| Field | Value |
|---|---|
| **Epic** | [FLPATH-3254](https://redhat.atlassian.net/browse/FLPATH-3254) |
| **Author** | Chad Crum |
| **Version** | 1.7 |
| **Last Updated** | 2026-07-22 |
| **Target Release** | DCM 1.0 |
| **Status** | Ready |

## Description

DCM control-plane authentication adds Keycloak-backed identity to the DCM API. The control-plane validates JWT bearer tokens directly against Keycloak's JWKS endpoint via OIDC discovery (`go-oidc/v3`), with a proxy-header fallback (`X-Auth-Proxy-Secret` + `X-Forwarded-User`) for callers behind an auth proxy. New actors are JIT-provisioned on first login. An admin actor is seeded on startup when `DCM_ADMIN_SUBJECT` is configured.

This plan validates all authentication capabilities across three deployment configurations: auth disabled (default), JWT-enabled, and proxy-only mode. It also includes Helm chart smoke tests confirming baseline deployment with auth disabled (auth-enabled Helm testing deferred to [FLPATH-4476](https://redhat.atlassian.net/browse/FLPATH-4476)).

### References

- [FLPATH-3254](https://redhat.atlassian.net/browse/FLPATH-3254) - Implement IDM/IAM and Authorization Layer (epic)
- [FLPATH-4432](https://redhat.atlassian.net/browse/FLPATH-4432) - Implement IDM/IAM user authentication layer (story)
- [control-plane#24](https://github.com/dcm-project/control-plane/pull/24) - feat(auth): add IDM/IAM authentication with in-app JWT validation
- [FLPATH-4478](https://redhat.atlassian.net/browse/FLPATH-4478) - Add DCM authentication E2E tests in utilities repo
- [enhancements#52](https://github.com/dcm-project/enhancements/pull/52) - Authentication enhancement design

### Acceptance Criteria

- All P1 (critical) test cases must PASS
- All P2 (important) test cases must PASS or have documented workarounds
- P3 (nice-to-have) failures may be deferred with a tracking Jira issue

---

## Environment and Global Setup

### Environment Requirements

- RHEL 9.x host with Podman 5.x and podman-compose
- Root or sudo access on the host
- `git`, `curl`, `jq` available
- `psql` (postgresql-client) on the host, OR use `podman exec <postgres-container> psql` for DB queries
- Ports 8080, 8180, 5432, 4222, 7007 free
- Network access to `quay.io` for pulling container images

### Deployment Configurations

Three configurations are used across test cases:

| Config | AUTH_DISABLED | AUTH_ISSUER_URL | AUTH_JWT_AUDIENCE | AUTH_PROXY_SECRET | Use Case |
|--------|-------------|-----------------|-------------------|-------------------|----------|
| **A** | `true` | _(empty)_ | _(empty)_ | `dcm-dev-proxy-secret` | Default, no auth |
| **B** | `false` | `http://keycloak:8080/realms/dcm` | `dcm-api` | `dcm-dev-proxy-secret` | Full auth (JWT + proxy) |
| **C** | `false` | _(empty)_ | _(empty)_ | `dcm-dev-proxy-secret` | Proxy-only, no JWT |

### Keycloak Preconfigured Data

From `deploy/keycloak/realm-export.json`:

| Item | Value |
|------|-------|
| Realm | `dcm` |
| Client (confidential) | `dcm-proxy` (secret: `<DCM_PROXY_SECRET>`, direct access grants + service accounts, audience mapper: `dcm-api`) |
| Client (public) | `dcm-cli` (device auth grant, audience mapper: `dcm-api`) |
| User | `dcm-admin` (UUID: `56deb662-4820-5d83-b828-f4beb11a5fa7`, password: `<DCM_DEV_USER_PASSWORD>`) |
| Access token lifespan | 300s (5 minutes) |

### Global Setup

These steps apply to all test cases. Complete them once before starting.

**Step 1: Clone and checkout the control-plane repository**

```bash
cd /root
git clone https://github.com/dcm-project/control-plane.git dcm-control-plane
cd dcm-control-plane
git checkout flpath-4432-implement-authentication
```

**Expected:** Repository cloned and on the `flpath-4432-implement-authentication` branch.

**Step 2: Start the stack in auth-disabled mode (Config A)**

```bash
make compose-up
```

**Expected:** All services start and become healthy (Keycloak takes 30-60s to import the realm).

```bash
podman compose -f deploy/compose.yaml ps
```

**Step 3: Verify the health endpoint**

```bash
curl -s http://localhost:8080/api/v1alpha1/health | jq .
```

**Expected:** `{"status":"ok","path":"/api/v1alpha1/health"}`

**Step 4: Verify Keycloak OIDC discovery**

```bash
curl -sf http://localhost:8180/realms/dcm/.well-known/openid-configuration | jq .issuer
```

**Expected:** `"http://keycloak:8080/realms/dcm"`

> **Issuer alignment:** Keycloak uses `KC_HOSTNAME=http://keycloak:8080` with `KC_HOSTNAME_STRICT=false` (set in compose.yaml) to stamp `iss=http://keycloak:8080/realms/dcm` in all tokens - even when the token endpoint is accessed externally via `localhost:8180`. The control-plane's `AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm` matches this. If you see `iss=http://localhost:8180/realms/dcm` in tokens, verify `KC_HOSTNAME` (not `KC_HOSTNAME_URL`) is set in compose.yaml.

### Helper: Switch to Config B (Auth Enabled)

```bash
make compose-down
AUTH_DISABLED=false AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm AUTH_JWT_AUDIENCE=dcm-api make compose-up
```

### Helper: Switch to Config C (Proxy-Only)

```bash
make compose-down
AUTH_DISABLED=false make compose-up
```

### Helper: Obtain JWT Tokens

```bash
# Password grant (dcm-admin user)
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)

# Client credentials (service account)
SA_TOKEN=$(curl -s -d 'grant_type=client_credentials&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)

# Inspect token claims
echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq '{sub, preferred_username, aud, iss}'
```

### Helper: Database Access

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane
```

---

## Test Cases

> **Happy Path Flow:** TC-01 through TC-08 form the smoke test sequence. Run these first to validate the core authentication path end-to-end. If any of these fail, subsequent test cases are likely to fail as well.

### TC-01: Health endpoint bypasses auth when auth is enabled

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `health_test.go`
**Requires:** Global setup, Config B

#### Description

The health endpoint at `/api/v1alpha1/health` must be accessible without authentication headers even when auth is enabled. The middleware checks the URL path before any auth logic.

#### Prerequisites

- Stack running in Config B (auth enabled)

#### Steps

**Step 1: Hit health endpoint with no auth headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' http://localhost:8080/api/v1alpha1/health
```

**Expected:** HTTP 200 with `{"status":"ok","path":"/api/v1alpha1/health"}`.

**Step 2: Confirm a non-health endpoint is rejected without auth**

```bash
curl -s -w '\nHTTP %{http_code}\n' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `{"type":"UNAUTHENTICATED","status":401,"title":"Unauthorized","detail":"missing authentication"}`.

#### Cleanup

None - Config B needed for subsequent test cases.

---

### TC-02: Auth disabled mode - API requests succeed without auth

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** Global setup, Config A

#### Description

With `AUTH_DISABLED=true` (default compose), all API requests succeed without authentication. `DisabledMiddleware` injects a synthetic `auth-disabled` system actor into every request context.

#### Prerequisites

- Stack running in Config A (`AUTH_DISABLED=true`)

#### Steps

**Step 1: List providers without auth headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200 with `{"providers":[]}` or `{"providers":[...]}`. No 401 or 403.

**Step 2: Create a provider without auth headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -X POST http://localhost:8080/api/v1alpha1/providers -H 'Content-Type: application/json' -d '{"name":"auth-disabled-test","service_type":"compute","endpoint":"http://example.com","schema_version":"v1alpha1","operations":{"provision":{"path":"/provision"},"deprovision":{"path":"/deprovision"}}}'
```

**Expected:** HTTP 201 with provider JSON containing a generated `id`.

**Step 3: Delete the test provider**

```bash
curl -s -X DELETE -o /dev/null -w 'HTTP %{http_code}\n' http://localhost:8080/api/v1alpha1/providers/<id-from-step-2>
```

**Expected:** HTTP 204.

#### Cleanup

Switch to Config B for remaining test cases (see Helper: Switch to Config B).

---

### TC-03: Admin actor and identity seeded on startup

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `admin_seed_test.go`
**Requires:** Config B with fresh database

#### Description

When `DCM_ADMIN_SUBJECT` is set, the control-plane creates an admin actor and Keycloak identity binding during startup. The `Seed()` method runs before the HTTP server starts.

#### Prerequisites

- Config B running with fresh database (volumes removed by `make compose-down`)

#### Steps

**Step 1: Verify admin actor in database**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT id, username, type, status FROM actors WHERE username = 'admin';"
```

**Expected:** One row with `username=admin`, `type=human`, `status=active`.

**Step 2: Verify admin identity binding**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT actor_id, auth_provider, external_id FROM actor_identities WHERE external_id = '56deb662-4820-5d83-b828-f4beb11a5fa7';"
```

**Expected:** One row with `auth_provider=keycloak`. The `actor_id` matches the admin actor's `id` from step 1.

#### Cleanup

None - admin actor needed for subsequent test cases.

---

### TC-04: JWT authentication via password grant

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `oidc_test.go`
**Requires:** TC-03

#### Description

Obtain a JWT access token from Keycloak using the Resource Owner Password Credentials grant, then send it as a Bearer token to the control-plane. The control-plane validates the JWT signature via OIDC discovery/JWKS, extracts `sub` and `preferred_username`, and resolves the admin actor.

#### Prerequisites

- Config B running
- Admin actor seeded (TC-03)

#### Steps

**Step 1: Obtain an access token**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
echo "Token length: ${#TOKEN}"
```

**Expected:** Token length > 100 (a valid JWT string).

**Step 2: Inspect token claims**

```bash
echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq '{sub, preferred_username, aud, iss}'
```

**Expected:** `sub` = `56deb662-4820-5d83-b828-f4beb11a5fa7`, `preferred_username` = `dcm-admin`, `aud` contains `dcm-api`, `iss` = `http://keycloak:8080/realms/dcm`.

**Step 3: Call API with Bearer token**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200 with providers JSON array.

#### Cleanup

None - token expires naturally after 300s.

---

### TC-05: JWT authentication via client_credentials grant

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `oidc_test.go`
**Requires:** TC-03

#### Description

Validates service-to-service authentication using client_credentials grant. The control-plane JIT-provisions a service account actor on first use.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Obtain a service account token**

```bash
SA_TOKEN=$(curl -s -d 'grant_type=client_credentials&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
echo "Token length: ${#SA_TOKEN}"
```

**Expected:** Token length > 100.

**Step 2: Inspect service account claims**

```bash
echo "$SA_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq '{sub, preferred_username, aud}'
```

**Expected:** `sub` is a UUID, `preferred_username` starts with `service-account-dcm-proxy`, `aud` contains `dcm-api`.

**Step 3: Call API with service account token**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $SA_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

**Step 4: Verify JIT-provisioned service account in DB**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT username, type, status FROM actors WHERE username LIKE 'service-account%';"
```

**Expected:** One row with `username=service-account-dcm-proxy`, `type=human`, `status=active`.

> **Known limitation:** Service accounts are typed as `human` - JIT provisioning always sets `type=human`. Tracked as [FLPATH-4483](https://redhat.atlassian.net/browse/FLPATH-4483).

#### Cleanup

None.

---

### TC-06: JIT provisioning of new Keycloak user

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `jit_test.go`, `oidc_test.go`
**Requires:** TC-04

#### Description

A brand-new Keycloak user is automatically provisioned as a DCM actor on their first API call. JIT provisioning creates an actor row and identity binding in a single transaction.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Get Keycloak admin token**

```bash
KC_ADMIN_TOKEN=$(curl -s -d 'grant_type=password&client_id=admin-cli&username=admin&password=<KEYCLOAK_ADMIN_PASSWORD>' http://localhost:8180/realms/master/protocol/openid-connect/token | jq -r .access_token)
```

**Expected:** Token obtained.

**Step 2: Create a new user in the dcm realm**

```bash
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -X POST http://localhost:8180/admin/realms/dcm/users -H "Authorization: Bearer $KC_ADMIN_TOKEN" -H 'Content-Type: application/json' -d '{"username":"jit-test-user","enabled":true,"firstName":"JIT","lastName":"Test","email":"jit-test@example.com","emailVerified":true,"credentials":[{"type":"password","value":"<TEST_USER_PASSWORD>","temporary":false}]}'
```

**Expected:** HTTP 201.

> **Note:** Keycloak 26 Declarative User Profile requires `firstName`, `lastName`, and `email` with `emailVerified: true`. Without `email` and `emailVerified`, the password grant returns `"Account is not fully set up"` (invalid_grant).

**Step 3: Obtain a token for the new user**

```bash
JIT_TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=jit-test-user&password=<TEST_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
```

**Expected:** Token obtained.

**Step 4: First API call triggers JIT provisioning**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $JIT_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

**Step 5: Verify actor created in database**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT username, type, status FROM actors WHERE username = 'jit-test-user';"
```

**Expected:** One row: `username=jit-test-user`, `type=human`, `status=active`.

**Step 6: Verify identity binding**

```bash
JIT_SUB=$(echo "$JIT_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq -r .sub)
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT auth_provider, external_id FROM actor_identities WHERE external_id = '$JIT_SUB';"
```

**Expected:** One row with `auth_provider=keycloak`, `external_id` matching the user's `sub` UUID.

#### Cleanup

None - jit-test-user needed for TC-19 and TC-20.

---

### TC-07: Proxy header authentication

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** TC-03

#### Description

Validates the proxy-header fallback auth path. A valid `X-Auth-Proxy-Secret` causes the middleware to accept `X-Forwarded-User` as the subject identifier.

#### Prerequisites

- Config B running (both JWT and proxy paths active)

#### Steps

**Step 1: Send request with valid proxy headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: 56deb662-4820-5d83-b828-f4beb11a5fa7' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Admin actor resolved from `X-Forwarded-User`.

**Step 2: Verify preferred username header is propagated**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: 56deb662-4820-5d83-b828-f4beb11a5fa7' -H 'X-Forwarded-Preferred-Username: dcm-admin' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

#### Cleanup

None.

---

### TC-08: Full CRUD lifecycle with JWT auth

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** TC-04

#### Description

Validates the complete provider CRUD lifecycle under JWT authentication: create, list, get by ID, delete, and verify deletion.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Obtain a token**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
```

**Expected:** Token obtained.

**Step 2: Create a provider**

```bash
curl -s -w '\nHTTP %{http_code}\n' -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"name":"crud-test-provider","service_type":"compute","endpoint":"http://example.com","schema_version":"v1alpha1","operations":{"provision":{"path":"/provision"},"deprovision":{"path":"/deprovision"}}}' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 201 with provider JSON. Note the `id`.

**Step 3: List providers**

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers | jq '.providers[].name'
```

**Expected:** Output includes `"crud-test-provider"`.

**Step 4: Get provider by ID**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers/<id-from-step-2>
```

**Expected:** HTTP 200 with matching provider.

**Step 5: Delete provider**

```bash
curl -s -X DELETE -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers/<id-from-step-2>
```

**Expected:** HTTP 204.

**Step 6: Verify deletion**

```bash
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers/<id-from-step-2>
```

**Expected:** HTTP 404.

#### Cleanup

None - provider deleted in step 5.

---

### TC-09: JIT provisioning uses preferred_username for actor username

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (subsystem) | `jit_test.go`
**Requires:** TC-06

#### Description

When `preferred_username` is present in JWT claims or the `X-Forwarded-Preferred-Username` header, the JIT-provisioned actor's username is set from it. Without it, the username falls back to the external ID (UUID).

#### Prerequisites

- Config B running

#### Steps

**Step 1: Verify jit-test-user from TC-06 used preferred_username**

```bash
JIT_SUB=$(echo "$JIT_TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null | jq -r .sub)
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT username FROM actors a JOIN actor_identities ai ON a.id = ai.actor_id WHERE ai.external_id = '$JIT_SUB';"
```

**Expected:** `username = jit-test-user` (from `preferred_username` claim), NOT the raw UUID.

#### Cleanup

None.

---

### TC-10: JIT provisioning falls back to externalID when preferred_username is absent

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (subsystem) | `jit_test.go`
**Requires:** TC-03, Config B

#### Description

When no `preferred_username` is available (proxy path without the preferred username header), the actor's username is set to the external ID.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send request via proxy headers without preferred_username**

```bash
NEW_UUID=$(uuidgen)
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H "X-Forwarded-User: $NEW_UUID" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Actor JIT-provisioned.

**Step 2: Verify username equals the UUID**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT username FROM actors a JOIN actor_identities ai ON a.id = ai.actor_id WHERE ai.external_id = '$NEW_UUID';"
```

**Expected:** `username` equals `$NEW_UUID`.

#### Cleanup

None.

---

### TC-11: Second request for same user skips re-provisioning

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `jit_test.go`
**Requires:** TC-06

#### Description

After JIT provisioning on first login, subsequent requests for the same user resolve from cache or DB lookup without creating duplicate records.

#### Prerequisites

- Config B running, jit-test-user already provisioned (TC-06)

#### Steps

**Step 1: Send a second request for jit-test-user**

```bash
JIT_TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=jit-test-user&password=<TEST_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $JIT_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

**Step 2: Verify still exactly one actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actors WHERE username = 'jit-test-user';"
```

**Expected:** Count = 1.

#### Cleanup

None.

---

### TC-11b: Username collision during JIT provisioning returns 409

**Priority:** P2 (important)
**Type:** Negative
**Method:** Automated (subsystem) | `race_test.go`
**Requires:** TC-07

#### Description

When two different external identities attempt to JIT-provision with the same `preferred_username`, the first succeeds and the second receives HTTP 409 Conflict. This validates the unique username constraint and the `ErrUsernameConflict` error path in `service.go`.

The proxy-header path is used here because it allows independent control of subject UUID and preferred_username without Keycloak user manipulation.

#### Prerequisites

- Config B running

#### Steps

**Step 1: JIT-provision user A with a shared username via proxy headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: aaaaaaaa-1111-1111-1111-aaaaaaaaaaaa' -H 'X-Forwarded-Preferred-Username: collision-test-user' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Actor created with `username=collision-test-user`, `external_id=aaaaaaaa-1111-...`.

**Step 2: Verify user A was provisioned**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT id, username, type FROM actors WHERE username = 'collision-test-user';"
```

**Expected:** Exactly one row.

**Step 3: JIT-provision user B with the same username but different external ID**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: bbbbbbbb-2222-2222-2222-bbbbbbbbbbbb' -H 'X-Forwarded-Preferred-Username: collision-test-user' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 409 with `"detail":"username already in use by another account"`.

**Step 4: Verify no duplicate actors**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actors WHERE username = 'collision-test-user';"
```

**Expected:** Count = 1 (only user A).

#### Cleanup

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "DELETE FROM actor_identities WHERE external_id = 'aaaaaaaa-1111-1111-1111-aaaaaaaaaaaa'; DELETE FROM actors WHERE username = 'collision-test-user';"
```

---

### TC-12: Bearer token takes precedence over proxy headers

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** TC-04

#### Description

When both `Authorization: Bearer` and `X-Auth-Proxy-Secret` + `X-Forwarded-User` headers are present, the JWT path takes precedence. The proxy headers are not evaluated.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send request with both Bearer and proxy headers pointing to different identities**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: different-subject' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Identity comes from JWT claims (`sub = 56deb662-...`), not proxy headers.

#### Cleanup

None.

---

### TC-12b: Invalid JWT with valid proxy headers returns 401 (no fallback)

**Priority:** P1 (critical)
**Type:** Security
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** TC-04

#### Description

When a JWTValidator is configured (Config B) and a request sends an invalid Bearer token alongside valid proxy headers, the middleware must return 401 - not fall through to the proxy path. This prevents a bad JWT from being silently ignored when proxy headers are present.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send invalid JWT with valid proxy headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'Authorization: Bearer <INVALID_JWT>' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: 56deb662-4820-5d83-b828-f4beb11a5fa7' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"invalid bearer token"`. The proxy headers are NOT evaluated.

#### Cleanup

None.

---

### TC-13: No JWTValidator falls through to proxy path

**Priority:** P2 (important)
**Type:** Functional
**Method:** Manual
**Requires:** Config C

#### Description

When `AUTH_ISSUER_URL` is not set (no JWTValidator configured), Bearer tokens are ignored and the proxy-header path is used instead.

#### Prerequisites

- Stack running in Config C (proxy-only, no JWT)

#### Steps

**Step 1: Send request with Bearer token and valid proxy headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'Authorization: Bearer <INVALID_JWT>' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: 56deb662-4820-5d83-b828-f4beb11a5fa7' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Bearer token ignored (no validator). Identity from proxy headers.

#### Cleanup

Switch back to Config B if continuing with other test cases.

---

### TC-14: Expired JWT rejected

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Manual
**Requires:** TC-04, Config B

#### Description

An expired JWT token is rejected with 401. Keycloak access tokens have a 300s (5 minute) lifespan by default.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Obtain a token and wait for expiration**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
echo "Waiting 310s for token to expire..."
sleep 310
```

**Step 2: Send request with expired token**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"invalid bearer token"`.

#### Cleanup

None.

---

### TC-15: Forged JWT rejected

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `oidc_test.go`
**Requires:** Config B

#### Description

A JWT with an invalid signature is rejected. The control-plane verifies signatures against Keycloak's JWKS endpoint.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send request with forged Bearer token**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'Authorization: Bearer <FORGED_JWT>' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"invalid bearer token"`.

#### Cleanup

None.

---

### TC-16: Missing auth headers returns 401

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** Config B

#### Description

A request with no auth headers (no Bearer, no proxy secret) is rejected with 401.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send request with no auth headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `{"type":"UNAUTHENTICATED","status":401,"title":"Unauthorized","detail":"missing authentication"}`.

**Step 2: Verify WWW-Authenticate and Content-Type headers**

```bash
curl -s -D- -o /dev/null http://localhost:8080/api/v1alpha1/providers 2>/dev/null | grep -iE 'content-type|www-authenticate'
```

**Expected:** `Content-Type: application/problem+json` and `WWW-Authenticate: Bearer`.

#### Cleanup

None.

---

### TC-17: Wrong proxy secret returns 401

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** Config B or Config C

#### Description

A request with an incorrect `X-Auth-Proxy-Secret` is rejected. The comparison uses `crypto/subtle.ConstantTimeCompare` to prevent timing attacks.

#### Prerequisites

- Config B or Config C running

#### Steps

**Step 1: Send request with wrong proxy secret**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: wrong-secret' -H 'X-Forwarded-User: 56deb662-4820-5d83-b828-f4beb11a5fa7' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"invalid proxy secret"`.

#### Cleanup

None.

---

### TC-18: Missing subject identifier returns 401

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `proxy_secret_test.go`
**Requires:** Config C

#### Description

Valid proxy secret with missing or empty `X-Forwarded-User` is rejected.

#### Prerequisites

- Config C running (or Config B)

#### Steps

**Step 1: Valid proxy secret, no user header**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"missing subject identifier"`.

**Step 2: Valid proxy secret, empty user header**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H 'X-Forwarded-User: ' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 401 with `"detail":"missing subject identifier"`.

#### Cleanup

None.

---

### TC-19: Suspended actor receives 403

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `status_test.go`
**Requires:** TC-06

#### Description

An actor with status `suspended` receives 403 Forbidden with an RFC 7807 error body.

#### Prerequisites

- Config B running
- `jit-test-user` actor exists (TC-06)

#### Steps

**Step 1: Suspend the actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "UPDATE actors SET status = 'suspended' WHERE username = 'jit-test-user';"
```

**Expected:** `UPDATE 1`.

**Step 2: Clear cache by restarting control-plane**

```bash
podman restart $(podman ps --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}')
sleep 5
```

**Step 3: Obtain token and attempt API call**

```bash
JIT_TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=jit-test-user&password=<TEST_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $JIT_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 403 with `{"type":"PERMISSION_DENIED","status":403,"title":"Forbidden","detail":"account suspended"}`.

**Step 4: Reactivate the actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "UPDATE actors SET status = 'active' WHERE username = 'jit-test-user';"
```

**Expected:** `UPDATE 1`.

#### Cleanup

Restart control-plane to clear stale cache:

```bash
podman restart $(podman ps --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}')
sleep 5
```

---

### TC-20: Deactivated actor receives 403

**Priority:** P1 (critical)
**Type:** Negative
**Method:** Automated (subsystem) | `status_test.go`
**Requires:** TC-06

#### Description

An actor with status `deactivated` receives 403 Forbidden.

#### Prerequisites

- Config B running
- `jit-test-user` actor exists and is active (TC-19 cleanup)

#### Steps

**Step 1: Deactivate the actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "UPDATE actors SET status = 'deactivated' WHERE username = 'jit-test-user';"
```

**Expected:** `UPDATE 1`.

**Step 2: Clear cache and attempt API call**

```bash
podman restart $(podman ps --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}')
sleep 5
JIT_TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=jit-test-user&password=<TEST_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $JIT_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 403 with `"detail":"account deactivated"`.

**Step 3: Reactivate the actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "UPDATE actors SET status = 'active' WHERE username = 'jit-test-user';"
podman restart $(podman ps --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}')
sleep 5
```

#### Cleanup

Actor reactivated, cache cleared.

---

### TC-21: Reactivated actor succeeds after cache expires

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (subsystem) | `status_test.go`
**Requires:** TC-19

#### Description

An actor that was suspended and then reactivated can access the API again after the cache is cleared.

#### Prerequisites

- Config B running
- Actor was suspended in TC-19, reactivated in TC-19 cleanup

#### Steps

**Step 1: Verify reactivated actor can access API**

```bash
JIT_TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=jit-test-user&password=<TEST_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -w '\nHTTP %{http_code}\n' -H "Authorization: Bearer $JIT_TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

#### Cleanup

None.

---

### TC-22: Cached actor bypasses DB lookup

**Priority:** P2 (important)
**Type:** Functional
**Method:** Manual
**Requires:** TC-04

#### Description

After the first request resolves an actor from the database, subsequent requests serve from the in-memory cache without a DB query.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Send first request (populates cache)**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

**Step 2: Send second request immediately (should hit cache)**

```bash
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Control-plane logs should not show a DB query for actor resolution on this request.

#### Cleanup

None.

---

### TC-23: Cache entry expires after TTL

**Priority:** P2 (important)
**Type:** Functional
**Method:** Manual
**Requires:** TC-04

#### Description

After the cache TTL expires, the next request re-queries the database. If the actor's status changed, the new status is enforced.

#### Prerequisites

- Config B running with `AUTH_CACHE_TTL=5s` for faster testing (requires stack restart)

#### Steps

**Step 1: Restart with short cache TTL**

```bash
make compose-down
AUTH_DISABLED=false AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm AUTH_JWT_AUDIENCE=dcm-api AUTH_CACHE_TTL=5s make compose-up
```

**Step 2: Send first request to cache the actor**

```bash
TOKEN=$(curl -s -d 'grant_type=password&client_id=dcm-proxy&client_secret=<DCM_PROXY_SECRET>&username=dcm-admin&password=<DCM_DEV_USER_PASSWORD>' http://localhost:8180/realms/dcm/protocol/openid-connect/token | jq -r .access_token)
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200.

**Step 3: Wait for cache expiry and re-request**

```bash
sleep 6
curl -s -o /dev/null -w 'HTTP %{http_code}\n' -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Cache miss triggers fresh DB lookup.

#### Cleanup

Restart with default TTL:

```bash
make compose-down
AUTH_DISABLED=false AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm AUTH_JWT_AUDIENCE=dcm-api make compose-up
```

---

### TC-24: Concurrent first login - same user

**Priority:** P1 (critical)
**Type:** Edge Case
**Method:** Automated (subsystem) | `race_test.go`
**Requires:** TC-03, Config B

#### Description

When multiple concurrent requests arrive for a new user, only one actor and one identity row are created. The race-condition retry path in `provisionActor` handles unique constraint violations.

#### Prerequisites

- Config B running
- A new UUID not yet in the database

#### Steps

**Step 1: Fire 10 concurrent requests for a new user**

```bash
NEW_UUID=$(uuidgen)
seq 10 | xargs -P10 -I{} curl -s -o /dev/null -w '%{http_code}\n' -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H "X-Forwarded-User: $NEW_UUID" -H 'X-Forwarded-Preferred-Username: race-test-user' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** All 10 requests return HTTP 200.

**Step 2: Verify exactly one actor and identity**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actors WHERE username = 'race-test-user';"
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actor_identities WHERE external_id = '$NEW_UUID';"
```

**Expected:** Both counts = 1.

#### Cleanup

None.

---

### TC-25: Concurrent first login - different users

**Priority:** P2 (important)
**Type:** Edge Case
**Method:** Manual
**Requires:** TC-03, Config B

#### Description

Multiple new users arriving concurrently each get their own actor with no cross-contamination.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Fire 5 concurrent requests for 5 different users**

```bash
for i in $(seq 5); do
  UUID=$(uuidgen)
  curl -s -o /dev/null -w "User $i: %{http_code}\n" -H 'X-Auth-Proxy-Secret: <AUTH_PROXY_SECRET>' -H "X-Forwarded-User: $UUID" -H "X-Forwarded-Preferred-Username: concurrent-user-$i" http://localhost:8080/api/v1alpha1/providers &
done
wait
```

**Expected:** All 5 return HTTP 200.

**Step 2: Verify 5 distinct actors**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT username FROM actors WHERE username LIKE 'concurrent-user-%' ORDER BY username;"
```

**Expected:** 5 rows: `concurrent-user-1` through `concurrent-user-5`.

#### Cleanup

None.

---

### TC-26: 401 response format is RFC 7807 compliant

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (subsystem) | `error_format_test.go`
**Requires:** Config B

#### Description

All 401 responses use `application/problem+json` content type and contain the required RFC 7807 fields: `type`, `status`, `title`, `detail`. 401 responses also include `WWW-Authenticate: Bearer` per RFC 7235.

#### Prerequisites

- Config B running

#### Steps

**Step 1: Trigger a 401 and validate response structure**

```bash
curl -s -D- http://localhost:8080/api/v1alpha1/providers
```

**Expected:**
- Status line: `HTTP/1.1 401 Unauthorized`
- `Content-Type: application/problem+json`
- `WWW-Authenticate: Bearer`
- Body contains: `type` (string), `status` (integer = 401), `title` (= "Unauthorized"), `detail` (string)

#### Cleanup

None.

---

### TC-27: 403 response format for suspended and deactivated actors

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (subsystem) | `error_format_test.go`
**Requires:** TC-19, TC-20

#### Description

403 responses for suspended and deactivated actors use `application/problem+json` with `type=PERMISSION_DENIED` and appropriate `detail` messages.

#### Prerequisites

- Verified by TC-19 (suspended) and TC-20 (deactivated) - this TC documents the format expectation.

#### Steps

**Step 1: Verify suspended actor 403 format (from TC-19 step 3 output)**

**Expected:** `{"type":"PERMISSION_DENIED","status":403,"title":"Forbidden","detail":"account suspended"}`

**Step 2: Verify deactivated actor 403 format (from TC-20 step 2 output)**

**Expected:** `{"type":"PERMISSION_DENIED","status":403,"title":"Forbidden","detail":"account deactivated"}`

#### Cleanup

None.

---

### TC-28: Admin seeding is idempotent on restart

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** TC-03

#### Description

Restarting the control-plane does not create duplicate admin actors. The `Seed()` method checks for existing admin actor by username before creating.

#### Prerequisites

- Config B running, admin actor exists (TC-03)

#### Steps

**Step 1: Restart control-plane**

```bash
podman restart $(podman ps --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}')
sleep 5
```

**Step 2: Verify no duplicate admin actors**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actors WHERE username = 'admin';"
```

**Expected:** Count = 1.

#### Cleanup

None.

---

### TC-29: Admin seeding skipped when DCM_ADMIN_SUBJECT is empty

**Priority:** P2 (important)
**Type:** Functional
**Method:** Manual
**Requires:** Global setup

#### Description

When `DCM_ADMIN_SUBJECT` is empty, no admin actor is created. The control-plane logs an info message and continues.

> **Note:** The config validator requires `DCM_ADMIN_SUBJECT` when `AUTH_DISABLED=false`. This test case must use `AUTH_DISABLED=true` (Config A) to allow an empty admin subject.

#### Prerequisites

- Stack started with `DCM_ADMIN_SUBJECT=""` and `AUTH_DISABLED=true`

#### Steps

**Step 1: Create compose override to clear admin subject**

The main compose.yaml uses `${DCM_ADMIN_SUBJECT:-56deb662-...}` which substitutes the default when the env var is empty. A compose override file bypasses this by setting the value directly.

```bash
cat > /tmp/no-admin-override.yaml <<'EOF'
services:
  control-plane:
    environment:
      DCM_ADMIN_SUBJECT: ""
EOF
```

**Step 2: Start stack without admin subject**

```bash
make compose-down
COMPOSE_PROJECT_NAME=control-plane podman compose -f deploy/compose.yaml -f /tmp/no-admin-override.yaml up -d
```

> **Note:** Use `COMPOSE_PROJECT_NAME=control-plane` to match the Makefile's naming. The default `AUTH_DISABLED=true` applies since no override is set.

**Step 3: Verify no admin actor**

```bash
PGPASSWORD=<POSTGRES_PASSWORD> psql -h localhost -U <POSTGRES_USER> -d control-plane -c "SELECT count(*) FROM actors WHERE username = 'admin';"
```

**Expected:** Count = 0.

**Step 4: Check control-plane logs for skip message**

```bash
podman logs $(podman ps -a --filter 'label=com.docker.compose.service=control-plane' --format '{{.Names}}') 2>&1 | grep -i "admin"
```

**Expected:** Log contains `DCM_ADMIN_SUBJECT not set, skipping admin actor creation`.

#### Cleanup

Remove override and restart with default settings:

```bash
rm /tmp/no-admin-override.yaml
make compose-down
AUTH_DISABLED=false AUTH_ISSUER_URL=http://keycloak:8080/realms/dcm AUTH_JWT_AUDIENCE=dcm-api make compose-up
```

---

### TC-30: Auth disabled mode ignores garbage auth headers

**Priority:** P2 (important)
**Type:** Negative
**Method:** Manual
**Requires:** Config A

#### Description

When `AUTH_DISABLED=true`, the middleware does not inspect auth headers at all. Even garbage values succeed.

#### Prerequisites

- Config A running

#### Steps

**Step 1: Send request with garbage headers**

```bash
curl -s -w '\nHTTP %{http_code}\n' -H 'X-Auth-Proxy-Secret: garbage' -H 'X-Forwarded-User: garbage' -H 'Authorization: Bearer garbage' http://localhost:8080/api/v1alpha1/providers
```

**Expected:** HTTP 200. Headers irrelevant when `AUTH_DISABLED=true`.

#### Cleanup

None.

---

### TC-31: Compose stack teardown

**Priority:** P1 (critical)
**Type:** Cleanup
**Method:** Manual
**Requires:** All compose-based TCs (TC-01 through TC-30) complete

#### Description

Remove all compose stack resources and verify clean shutdown.

#### Steps

**Step 1: Stop the stack and remove volumes**

```bash
cd /root/dcm-control-plane
make compose-down
```

**Expected:** All containers stopped, volumes removed.

**Step 2: Verify no containers remain**

```bash
podman ps --filter 'name=control-plane' --format '{{.Names}}'
```

**Expected:** No output.

#### Cleanup

Optional - remove the clone:

```bash
rm -rf /root/dcm-control-plane
```

---

### Helm Chart Smoke Tests (Auth Disabled)

> **Note:** The Helm chart does not set any `AUTH_` environment variables, so the control-plane
> defaults to `AUTH_DISABLED=true`. Auth-enabled Helm deployment is tracked in
> [FLPATH-4476](https://redhat.atlassian.net/browse/FLPATH-4476). These TCs verify baseline
> Helm deployment and API functionality with authentication disabled.

### TC-32: Helm install and pod readiness

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** OpenShift/Kubernetes cluster access

#### Description

Install the DCM Helm chart into a test namespace and verify all pods reach a healthy state.

#### Prerequisites

- `oc` or `kubectl` CLI configured with cluster access
- Helm 3.x installed
- DCM control-plane repo cloned (same branch as compose tests)

#### Steps

**Step 1: Create namespace and install chart**

```bash
oc new-project dcm-helm-test || kubectl create namespace dcm-helm-test
helm install dcm-test deploy/helm/dcm/ --namespace dcm-helm-test
```

**Expected:** Helm release `dcm-test` created with no errors.

**Step 2: Wait for control-plane pod to be ready**

```bash
oc wait --for=condition=ready pod -l app.kubernetes.io/name=control-plane,app.kubernetes.io/instance=dcm-test -n dcm-helm-test --timeout=120s
```

**Expected:** Pod reaches `Ready` within 120 seconds.

**Step 3: Verify all pods are running**

```bash
kubectl get pods -n dcm-helm-test
```

**Expected:** At minimum `control-plane`, `postgres`, and `nats` pods in `Running` state with no restarts.

#### Cleanup

None - chart remains for TC-33.

---

### TC-33: Health endpoint accessible via Helm deployment

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** TC-32

#### Description

The health endpoint is reachable through the Kubernetes service. Confirms the control-plane started successfully with default (auth-disabled) config.

#### Prerequisites

- TC-32 passed, all pods running

#### Steps

**Step 1: Determine the control-plane URL**

For OpenShift with route enabled (default):

```bash
CP_URL=$(oc get route dcm-test-control-plane -n dcm-helm-test -o jsonpath='{.spec.host}')
echo "Control-plane URL: https://$CP_URL"
```

For clusters without routes, use port-forward:

```bash
kubectl port-forward svc/dcm-test-control-plane 8080:8080 -n dcm-helm-test &
PF_PID=$!
CP_URL="localhost:8080"
```

**Step 2: Hit the health endpoint**

For route (TLS):

```bash
curl -sk "https://$CP_URL/api/v1alpha1/health"
```

For port-forward:

```bash
curl -s "http://$CP_URL/api/v1alpha1/health"
```

**Expected:** HTTP 200 with `{"status":"ok","path":"/api/v1alpha1/health"}`.

**Step 3: Verify API responds without auth headers**

```bash
curl -sk "https://$CP_URL/api/v1alpha1/providers"
```

**Expected:** HTTP 200 with `{"providers":[]}`. No authentication required because `AUTH_DISABLED` defaults to `true`.

#### Cleanup

If using port-forward, stop it:

```bash
kill $PF_PID 2>/dev/null
```

---

### TC-34: Multi-subsystem CRUD with auth disabled (Helm)

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Manual
**Requires:** TC-33

#### Description

Exercise multiple control-plane subsystems (service providers, catalog service-types, catalog items) through the Helm-deployed API with auth disabled. Verifies more than one component is functional end-to-end, not just health.

#### Prerequisites

- TC-33 passed, `CP_URL` determined

#### Steps

**Step 1: Resolve the control-plane URL**

```bash
CP_URL=$(oc get route dcm-test-control-plane -n dcm-helm-test -o jsonpath='{.spec.host}')
SCHEME="https"
```

Or for port-forward:

```bash
kubectl port-forward svc/dcm-test-control-plane 8080:8080 -n dcm-helm-test &
PF_PID=$!
CP_URL="localhost:8080"
SCHEME="http"
```

**Step 2: Create a service provider**

```bash
curl -sk -X POST "$SCHEME://$CP_URL/api/v1alpha1/providers" -H 'Content-Type: application/json' -d '{"name":"helm-smoke-sp","service_type":"vm","endpoint":"http://example.com/api/v1alpha1","schema_version":"v1alpha1"}' -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 201 with the created provider JSON including an `id` field.

**Step 3: List service providers**

```bash
curl -sk "$SCHEME://$CP_URL/api/v1alpha1/providers" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 200 with `{"providers":[...]}` containing `helm-smoke-sp`.

**Step 4: List catalog service-types**

```bash
curl -sk "$SCHEME://$CP_URL/api/v1alpha1/service-types" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 200 with an empty list or existing service types. Confirms the catalog subsystem is reachable.

**Step 5: List catalog items**

```bash
curl -sk "$SCHEME://$CP_URL/api/v1alpha1/catalog-items" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 200. Confirms the catalog item subsystem responds independently of providers.

**Step 6: List policies**

```bash
curl -sk "$SCHEME://$CP_URL/api/v1alpha1/policies" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 200. Confirms the policy subsystem is serving requests.

**Step 7: Delete the service provider**

```bash
SP_ID=$(curl -sk "$SCHEME://$CP_URL/api/v1alpha1/providers" | jq -r '.providers[] | select(.name=="helm-smoke-sp") | .id // empty')
curl -sk -X DELETE "$SCHEME://$CP_URL/api/v1alpha1/providers/$SP_ID" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 204. Provider deleted.

**Step 8: Verify deletion**

```bash
curl -sk "$SCHEME://$CP_URL/api/v1alpha1/providers" -w '\nHTTP %{http_code}\n'
```

**Expected:** HTTP 200 with `{"providers":[]}` - `helm-smoke-sp` no longer present.

#### Cleanup

If using port-forward:

```bash
kill $PF_PID 2>/dev/null
```

---

### TC-35: Helm uninstall cleans up resources

**Priority:** P1 (critical)
**Type:** Cleanup
**Method:** Manual
**Requires:** TC-34

#### Description

Uninstall the Helm release and verify all Kubernetes resources are removed.

#### Steps

**Step 1: Uninstall the Helm release**

```bash
helm uninstall dcm-test --namespace dcm-helm-test
```

**Expected:** Release `dcm-test` uninstalled.

**Step 2: Wait for pods to terminate**

```bash
kubectl wait --for=delete pod --all -n dcm-helm-test --timeout=60s 2>/dev/null || true
kubectl get pods -n dcm-helm-test
```

**Expected:** No pods remain in the namespace.

**Step 3: Verify PVCs are cleaned up**

```bash
kubectl get pvc -n dcm-helm-test
```

**Expected:** No PVCs remain (or PVCs remain if the chart uses `Retain` - document which).

**Step 4: Delete the test namespace**

```bash
oc delete project dcm-helm-test || kubectl delete namespace dcm-helm-test
```

**Expected:** Namespace deleted.

#### Cleanup

None - this is the Helm cleanup test case.

---

## Global Teardown

If any test case left resources behind, run the relevant cleanup:

**Compose stack:**

```bash
cd /root/dcm-control-plane
make compose-down
```

This runs `podman compose down -v` which removes all containers and volumes.

**Helm deployment:**

```bash
helm uninstall dcm-test --namespace dcm-helm-test 2>/dev/null
oc delete project dcm-helm-test 2>/dev/null || kubectl delete namespace dcm-helm-test 2>/dev/null
```

---

## Test Case Dependency Graph

```
Global Setup (clone, Config A start)
    |
    +-- TC-02 (Auth disabled CRUD) --- TC-30 (Disabled ignores garbage)
    |
    +-- [Switch to Config B] --+
    |                          |
    +-- TC-01 (Health bypass)  +-- TC-03 (Admin seeding)
    |                          |     |
    +-- TC-16 (No auth 401)    |     +-- TC-04 (JWT password) ----+-- TC-08 (CRUD w/ JWT)
    |                          |     |                             |
    +-- TC-17 (Wrong secret)   |     +-- TC-05 (JWT client_creds) +-- TC-12 (JWT precedence)
    |                          |     |                             |
    +-- TC-15 (Forged JWT)     |     +-- TC-07 (Proxy headers)    +-- TC-12b (Invalid JWT no fallback)
                               |     |                             |
                               |     |                             +-- TC-22 (Cache hit)
                               |     |                             |
                               |     +-- TC-28 (Idempotent seed)   +-- TC-23 (Cache TTL)
                               |     |
                               |     +-- TC-06 (JIT user) ---+-- TC-09 (preferred_username)
                               |                              |
                               +-- TC-10 (UUID fallback)      +-- TC-11 (No re-provision)
                               |                              |
                               +-- TC-24 (Concurrent same)    +-- TC-19 (Suspended 403) --- TC-21 (Reactivated)
                               |                              |
                               +-- TC-25 (Concurrent diff)    +-- TC-20 (Deactivated 403)
                               |
                               +-- [Switch to Config C] --+
                               |                          |
                               +-- TC-13 (No validator)   +-- TC-18 (Empty subject)
                               |
                               +-- TC-14 (Expired JWT)
                               |
                               +-- TC-26 (401 RFC 7807)
                               +-- TC-27 (403 format)
                               +-- TC-29 (No admin subject)
                               |
                               +-- TC-31 (Compose teardown)


Helm Chart (independent track - requires K8s/OCP cluster):

    TC-32 (Helm install) --- TC-33 (Health check) --- TC-34 (CRUD round-trip) --- TC-35 (Helm uninstall)
```

---

## Risk Observations

1. **Cache serves stale status for up to TTL (60s default)**: A suspended actor can continue making requests for up to 60 seconds after suspension. Documented trade-off, not a defect.
2. **Proxy-header fallback trusts headers if secret matches**: Anyone with `AUTH_PROXY_SECRET` can set arbitrary `X-Forwarded-User`. Network policy responsibility in production. JWT is the primary mechanism.
3. **`AUTH_DISABLED=true` is the compose default**: Must be explicitly overridden for staging/production.
4. **Service accounts typed as `human`**: JIT provisioning always sets `type=human`, even for service accounts. Tracked as [FLPATH-4483](https://redhat.atlassian.net/browse/FLPATH-4483).
5. **JWKS availability at startup**: If Keycloak is unreachable, `NewOIDCValidator` fails and the control-plane exits.
6. **Helm chart auth not templatized**: `AUTH_DISABLED` hardcoded to `true` in chart. Auth-enabled Helm testing deferred to [FLPATH-4476](https://redhat.atlassian.net/browse/FLPATH-4476).
7. **Username collision during JIT provisioning**: If two Keycloak users share the same `preferred_username` and both JIT-provision, the second user receives HTTP 409 (`username already in use by another account`). This is by design but may surprise operators who expect provisioning to always succeed.

---

## Automation Coverage

The following test cases are automated in the auth subsystem test suite
([control-plane#32](https://github.com/dcm-project/control-plane/pull/32),
`test/subsystem/auth/`). Automated tests run in CI on every PR via
`.github/workflows/subsystem.yaml`.

| TC | Description | Subsystem Test File | Notes |
|---|---|---|---|
| TC-01 | Health bypass | `health_test.go` | 3 sub-cases: no auth, invalid bearer, wrong proxy |
| TC-03 | Admin seeding | `admin_seed_test.go` | Actor + identity verified via DB |
| TC-04 | JWT password grant | `oidc_test.go` | ROPC flow + JIT provision + DB verify |
| TC-05 | Client credentials | `oidc_test.go` | Service account JIT provision + DB verify |
| TC-06 | JIT provisioning | `jit_test.go`, `oidc_test.go` | Proxy and JWT paths both covered |
| TC-07 | Proxy header auth | `proxy_secret_test.go` | |
| TC-09 | preferred_username | `jit_test.go` | |
| TC-10 | UUID fallback | `jit_test.go` | |
| TC-11 | No re-provisioning | `jit_test.go` | Cache hit path |
| TC-11b | Username collision | `race_test.go` | 409 Conflict verified |
| TC-12 | JWT precedence (valid) | `proxy_secret_test.go` | Valid JWT + valid proxy, JWT identity wins |
| TC-12b | JWT precedence (invalid) | `proxy_secret_test.go` | Invalid JWT + valid proxy = hard 401 |
| TC-15 | Forged JWT | `oidc_test.go` | Tampered signature |
| TC-16 | Missing auth 401 | `proxy_secret_test.go` | |
| TC-17 | Wrong proxy secret | `proxy_secret_test.go` | |
| TC-18 | Missing subject | `proxy_secret_test.go` | |
| TC-19 | Suspended 403 | `status_test.go` | DB update + cache TTL wait |
| TC-20 | Deactivated 403 | `status_test.go` | |
| TC-21 | Reactivated actor | `status_test.go` | Suspend -> 403 -> reactivate -> 200 |
| TC-24 | Concurrent same user | `race_test.go` | 10 goroutines, unique constraint handling |
| TC-26 | 401 RFC 7807 format | `error_format_test.go` | Content-Type + body fields + WWW-Authenticate |
| TC-27 | 403 RFC 7807 format | `error_format_test.go` | |

**22 of 35 test cases automated** (27 Ginkgo specs total - some TCs map to
multiple specs).

Remaining manual-only TCs:

| TC | Reason not automated |
|---|---|
| TC-02 | Auth disabled mode - covered by catalog/policy/sp subsystem suites |
| TC-08 | CRUD lifecycle with JWT - CRUD covered by catalog suite, auth by auth suite |
| TC-13 | Config C (proxy-only) - requires separate compose configuration |
| TC-14 | Expired JWT - requires 5-minute wait |
| TC-22 | Cache bypass verification - requires log inspection |
| TC-23 | Cache TTL expiry - partially covered by status tests (AUTH_CACHE_TTL=2s) |
| TC-25 | Concurrent different users - low risk vs TC-24 |
| TC-28 | Admin seeding idempotent - requires container restart |
| TC-29 | Empty DCM_ADMIN_SUBJECT - requires separate compose configuration |
| TC-30 | Auth disabled ignores garbage - covered by other suites |
| TC-31 | Compose teardown - infrastructure, not functional |
| TC-32-35 | Helm chart tests - separate infrastructure (FLPATH-4476) |

---

## Version History

| Version | Date | Changes |
|---|---|---|
| 1.0 | 2026-07-17 | Initial test plan: 31 test cases covering health bypass, auth disabled, admin seeding, JWT auth, JIT provisioning, proxy headers, status enforcement, caching, concurrency, error format, and configuration |
| 1.1 | 2026-07-17 | Added Helm chart smoke tests (TC-32 through TC-35): install, health, CRUD round-trip, uninstall. Total: 35 test cases |
| 1.2 | 2026-07-17 | Commands validated against live lab deployment |
| 1.3 | 2026-07-17 | Senior QE review fixes: corrected KC_HOSTNAME_STRICT doc (false not true), fixed TC-29 compose override for empty DCM_ADMIN_SUBJECT, added TC-12b (invalid JWT + valid proxy = 401), added username conflict risk observation |
| 1.4 | 2026-07-20 | Full validation on lab host: all 32 compose TCs passed, 4 Helm TCs skipped (no cluster). Fixes: TC-06 KC user needs email+emailVerified, TC-11b cleanup table name actor_identities not identities, TC-29 must use AUTH_DISABLED=true, TC-19/20 restart commands use podman restart directly, psql via podman exec |
| 1.5 | 2026-07-20 | Phase 6 review fixes: promoted TC-14/TC-15 to P1 (security-critical), fixed TC-31 container filter from deploy_ to control-plane, replaced container name placeholders with discoverable podman commands |
| 1.6 | 2026-07-20 | TC-32 Step 2: corrected label selector from `app.kubernetes.io/component=control-plane` to `app.kubernetes.io/name=control-plane` based on actual chart template execution on a lab SNO cluster |
| 1.7 | 2026-07-22 | Added automation coverage section mapping 22 of 35 TCs to subsystem test suite ([control-plane#32](https://github.com/dcm-project/control-plane/pull/32)). 27 Ginkgo specs cover TC-01, TC-03-07, TC-09-12b, TC-15-21, TC-24, TC-26-27. Remaining 13 TCs documented as manual-only with rationale |

---

## Sanitization Notice

This document is intended for sharing. The following rules apply:

- No credentials, tokens, API keys, or passwords - use placeholders (`<ADMIN_PASSWORD>`, `<API_TOKEN>`)
- No internal hostnames or IPs - use placeholders (`<CLUSTER_HOST>`, `<API_ENDPOINT>`)
- No internal URLs (Kerberos, RHSSO, internal wikis, proprietary portals)
- No PII (employee names, emails, account IDs) - use `<USER>`, `<ADMIN>`
- Secrets required by steps should reference where to obtain them (e.g., "retrieve from vault at `<VAULT_PATH>`"), never inline
- Open-source tool and project names are fine as-is
