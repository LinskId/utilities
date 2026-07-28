# Test Plan: FLPATH-4629 — Control-Plane Health Checks Postgres and NATS

| Field | Value |
|---|---|
| **Ticket** | [FLPATH-4629](https://redhat.atlassian.net/browse/FLPATH-4629) |
| **Author** | Thomas Stetson |
| **Version** | 1.0 |
| **Last Updated** | 2026-07-28 |
| **Status** | Draft |

## Description

The monolith control-plane health endpoint (`GET /api/v1alpha1/health`) now performs real dependency checks against Postgres and NATS (when messaging is enabled). If any required dependency is unreachable or unresponsive, the endpoint returns **503 Service Unavailable** with per-check details. This enables Kubernetes readiness probes and load balancers to drop unhealthy replicas.

### References

- [FLPATH-4629](https://redhat.atlassian.net/browse/FLPATH-4629) — Control-plane health checks Postgres and NATS (story)
- [control-plane#30](https://github.com/dcm-project/control-plane/pull/30) — Fail health when the control plane cannot reach its deps
- Reporter/Assignee: Gloria Ciavarrini

### Acceptance Criteria

1. Health succeeds only when database connectivity succeeds
2. When NATS is enabled, health also verifies NATS connectivity
3. When NATS is disabled, health does not require NATS
4. Any unreachable required dependency → 503 (unhealthy body for humans/ops)
5. Unit tests cover healthy and unhealthy cases (including DB-only and DB+NATS)

---

## Environment and Global Setup

### Environment Requirements

- DCM control-plane deployed and healthy (`./scripts/deploy-dcm.sh`)
- `podman` available for stopping/starting containers
- `curl`, `jq` available
- NATS enabled in the deployment (default compose config)

### Helper: Container Discovery

```bash
# Find compose container names dynamically
podman ps --filter 'label=com.docker.compose.service=postgres' --format '{{.Names}}'
podman ps --filter 'label=com.docker.compose.service=nats' --format '{{.Names}}'
```

### Helper: Health Check

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

---

## Implementation Details (from PR #30)

### Response Shape

```json
{
  "status": "ok",
  "path": "/api/v1alpha1/health",
  "checks": {
    "database": "ok",
    "nats": "ok"
  }
}
```

- HTTP 200 + `status: "ok"` when all checkers pass
- HTTP 503 + `status: "unhealthy"` when any checker fails
- `checks` map is omitted when empty (no checkers registered)
- `nats` key only present when NATS/messaging is enabled

### Checker Architecture

| Checker | What it verifies | Timeout |
|---------|-----------------|---------|
| `postgresChecker` | `sql.DB.PingContext()` via GORM | 1s per check |
| `natsChecker` | `conn.FlushTimeout()` on NATS connection | 1s per check (or ctx remaining) |

Overall health handler timeout: 2s. Each individual checker gets a 1s sub-context.

### Key Behaviors

- If no checkers are registered, `healthy` defaults to `false` (→ 503). Prevents silent misconfiguration.
- NATS checker returns error if `conn == nil` or `conn.IsConnected() == false`.
- Postgres checker returns error if `db == nil` ("database not configured").
- Context cancellation short-circuits remaining checkers with "unavailable".

---

## Test Tiers

| Tier | Requires | Test Cases | Notes |
|------|----------|------------|-------|
| **Unit** (in repo) | Go test environment | Mocked DB/NATS | Already in PR #30 (`health_test.go`, +170 lines) |
| **E2E — happy path** | DCM stack running | TC-01 | Already exists (`api_health_test.go`) |
| **E2E — disruptive** | DCM stack + podman | TC-02 through TC-07 | **NOT YET IMPLEMENTED** |

### Upstream Test Coverage (in repo)

`internal/app/health_test.go` (+170 lines in PR #30):
- Healthy with DB only (no NATS checker)
- Healthy with DB + NATS
- Unhealthy when DB unreachable
- Unhealthy when NATS unreachable (DB healthy)
- Response format validation (JSON fields, status codes)
- Timeout behavior

---

## Test Cases

### TC-01: Healthy control-plane (happy path) — EXISTS

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `smoke`
**Requires:** DCM stack running

#### Description

Verifies that `/health` returns 200 with `status: "ok"` when all dependencies are reachable. This test already exists but does not assert individual checker results.

#### Prerequisites

- DCM stack deployed and healthy

#### Steps

**Step 1: Hit the health endpoint**

```bash
curl -s http://localhost:9080/api/v1alpha1/health
```

**Expected:** HTTP 200 with `{"status":"ok","path":"/api/v1alpha1/health"}`.

#### Cleanup

None.

**Gap:** Does not assert the `checks` map (`database: ok`, `nats: ok`). Should be updated to verify individual checker results (see TC-02).

---

### TC-02: Health reports individual check status — NEW

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `smoke`
**Requires:** DCM stack running

#### Description

Verifies the `checks` map in the health response reports per-dependency status. This validates that the new structured response format is exposed through the gateway.

#### Prerequisites

- DCM stack deployed and healthy

#### Steps

**Step 1: Hit health endpoint while stack is healthy**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 200 with body containing:
- `"status": "ok"`
- `"checks"` object with `"database": "ok"`
- If NATS enabled: `"checks"` contains `"nats": "ok"`

#### Cleanup

None.

---

### TC-03: Health returns 503 when Postgres is stopped — NEW

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `disruptive`
**Requires:** TC-01 passing, `podman` available

#### Description

Stopping the Postgres container causes the health endpoint to return 503 with `checks.database: "unavailable"`. Restarting Postgres restores health to 200.

#### Prerequisites

- DCM stack deployed and healthy
- `requirePodman()` passes

#### Steps

**Step 1: Confirm health is 200 (precondition)**

```bash
curl -s -o /dev/null -w '%{http_code}' http://localhost:9080/api/v1alpha1/health
```

**Expected:** `200`.

**Step 2: Stop the Postgres container**

```bash
POSTGRES=$(podman ps --filter 'label=com.docker.compose.service=postgres' --format '{{.Names}}')
podman stop "$POSTGRES"
```

**Expected:** Container stopped, exit 0.

**Step 3: Verify health returns 503 (Eventually, timeout 15s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 503 with `{"status":"unhealthy","path":"/api/v1alpha1/health","checks":{"database":"unavailable","nats":"ok"}}`.

**Step 4: Restart Postgres**

```bash
podman start "$POSTGRES"
```

**Expected:** Container started.

**Step 5: Verify health recovers (Eventually, timeout 30s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 200 with `"status": "ok"`, `"checks": {"database": "ok", "nats": "ok"}`.

#### Cleanup

Postgres restarted in step 4. If test fails mid-way, restart Postgres manually.

---

### TC-04: Health returns 503 when NATS is stopped — NEW

**Priority:** P1 (critical)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `disruptive`
**Requires:** TC-01 passing, `podman` available

#### Description

Stopping the NATS container causes the health endpoint to return 503 with `checks.nats: "unavailable"` while `checks.database` remains `"ok"`. This validates independent checker isolation.

#### Prerequisites

- DCM stack deployed and healthy
- `requirePodman()` passes

#### Steps

**Step 1: Confirm health is 200 (precondition)**

```bash
curl -s -o /dev/null -w '%{http_code}' http://localhost:9080/api/v1alpha1/health
```

**Expected:** `200`.

**Step 2: Stop the NATS container**

```bash
NATS=$(podman ps --filter 'label=com.docker.compose.service=nats' --format '{{.Names}}')
podman stop "$NATS"
```

**Expected:** Container stopped.

**Step 3: Verify health returns 503 (Eventually, timeout 15s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 503 with `{"status":"unhealthy","checks":{"database":"ok","nats":"unavailable"}}`.

**Step 4: Restart NATS**

```bash
podman start "$NATS"
```

**Expected:** Container started.

**Step 5: Verify health recovers (Eventually, timeout 30s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 200 with `"status": "ok"`.

#### Cleanup

NATS restarted in step 4.

---

### TC-05: Health returns 503 when both Postgres and NATS are stopped — NEW

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `disruptive`
**Requires:** TC-03 and TC-04 passing, `podman` available

#### Description

Stopping both dependencies simultaneously results in both checks reporting "unavailable". Validates that failures are reported independently, not short-circuited after the first.

#### Prerequisites

- DCM stack deployed and healthy
- `requirePodman()` passes

#### Steps

**Step 1: Stop both Postgres and NATS containers**

```bash
POSTGRES=$(podman ps --filter 'label=com.docker.compose.service=postgres' --format '{{.Names}}')
NATS=$(podman ps --filter 'label=com.docker.compose.service=nats' --format '{{.Names}}')
podman stop "$POSTGRES" "$NATS"
```

**Expected:** Both containers stopped.

**Step 2: Verify health returns 503 with both checks unavailable (Eventually, timeout 15s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 503 with `"checks": {"database": "unavailable", "nats": "unavailable"}`.

**Step 3: Restart both containers**

```bash
podman start "$POSTGRES" "$NATS"
```

**Expected:** Both containers running.

**Step 4: Verify health recovers (Eventually, timeout 30s)**

```bash
curl -s http://localhost:9080/api/v1alpha1/health | jq .
```

**Expected:** HTTP 200 with `"status": "ok"`.

#### Cleanup

Containers restarted in step 3.

---

### TC-06: Health recovery time is bounded — NEW

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `disruptive`
**Requires:** `podman` available

#### Description

After a dependency is restarted, health should recover within a bounded time window. This validates that connection pools and reconnect logic work efficiently.

#### Prerequisites

- DCM stack deployed and healthy
- `requirePodman()` passes

#### Steps

**Step 1: Stop NATS container**

```bash
NATS=$(podman ps --filter 'label=com.docker.compose.service=nats' --format '{{.Names}}')
podman stop "$NATS"
```

**Expected:** Stopped.

**Step 2: Restart NATS and measure time to health 200**

```bash
podman start "$NATS"
time curl --retry 20 --retry-delay 1 --retry-all-errors -sf http://localhost:9080/api/v1alpha1/health > /dev/null
```

**Expected:** Health returns 200 within 10 seconds (NATS reconnect + flush).

**Step 3: Stop Postgres container**

```bash
POSTGRES=$(podman ps --filter 'label=com.docker.compose.service=postgres' --format '{{.Names}}')
podman stop "$POSTGRES"
```

**Expected:** Stopped.

**Step 4: Restart Postgres and measure time to health 200**

```bash
podman start "$POSTGRES"
time curl --retry 20 --retry-delay 1 --retry-all-errors -sf http://localhost:9080/api/v1alpha1/health > /dev/null
```

**Expected:** Health returns 200 within 15 seconds (connection pool re-establish).

#### Cleanup

Both containers running after steps 2 and 4.

---

### TC-07: Health response time is bounded (timeout enforcement) — NEW

**Priority:** P2 (important)
**Type:** Functional
**Method:** Automated (E2E)
**Labels:** `disruptive`
**Requires:** `podman` available

#### Description

When a dependency is unreachable, the health endpoint must respond within its configured timeout (2s overall) rather than hanging indefinitely. This validates the context timeout in `registerMonolithHealth`.

#### Prerequisites

- DCM stack deployed and healthy
- `requirePodman()` passes

#### Steps

**Step 1: Stop Postgres to simulate unreachable dependency**

```bash
POSTGRES=$(podman ps --filter 'label=com.docker.compose.service=postgres' --format '{{.Names}}')
podman stop "$POSTGRES"
```

**Expected:** Stopped.

**Step 2: Time the health request**

```bash
time curl -s -o /dev/null -w '%{http_code} %{time_total}s\n' http://localhost:9080/api/v1alpha1/health
```

**Expected:** HTTP 503 returned within 3 seconds (2s handler timeout + network overhead). Must NOT hang indefinitely.

**Step 3: Restart Postgres**

```bash
podman start "$POSTGRES"
```

**Expected:** Container running, health recovers.

#### Cleanup

Postgres restarted in step 3.

---

## Not Testable at E2E Level

| Scenario | Why |
|----------|-----|
| NATS disabled mode (AC #3) | Requires restarting control-plane with different config; unit tests cover this |
| No checkers registered | Internal misconfiguration; unit tests verify 503 fallback |
| Individual checker timeout (1s sub-context) | Requires precise network partitioning; unit tests mock this |
| Zero-checker 503 fallback | Cannot trigger externally; verified in `health_test.go` |

---

## Implementation Notes

### Container Names

The Postgres and NATS container names depend on the compose project. Use the existing `findComposeContainer` helper pattern:

```go
postgresContainer := findComposeContainer("postgres")
natsContainer := findComposeContainer("nats")
```

### Existing Infrastructure

- `sp_container_status_test.go` already has a NATS restart test (`"delivers status events after NATS restart"`) — the health disruption tests can share the same container discovery pattern
- `rehydration_persistence_test.go` has `restartSPRM()` — similar pattern for Postgres
- `requirePodman()` skip guard already exists for disruptive tests

### Suggested File

`tests/e2e/api_health_disruptive_test.go` — separate from the existing smoke test to keep `Label("disruptive")` isolated.

---

## Coverage Summary

| AC | Unit (repo) | E2E (existing) | E2E (needed) |
|----|-------------|----------------|--------------|
| 1. Health requires DB | ✅ | ❌ | TC-03 |
| 2. Health requires NATS (when enabled) | ✅ | ❌ | TC-04 |
| 3. Health skips NATS (when disabled) | ✅ | N/A (config change) | N/A |
| 4. Unreachable dep → 503 | ✅ | ❌ | TC-03, TC-04, TC-05 |
| 5. Unit tests | ✅ (PR #30) | — | — |
| — Response format (checks map) | ✅ | Partial (TC-01 exists) | TC-02 |
| — Recovery after restart | ❌ | ❌ | TC-06 |
| — Timeout enforcement | ✅ | ❌ | TC-07 |

---

## Risk Observations

1. **NATS reconnect timing is non-deterministic**: The NATS client library has automatic reconnect with backoff. Recovery time in TC-06 may vary under load. Use generous `Eventually` timeouts.
2. **Postgres connection pool caching**: GORM maintains a connection pool. `PingContext` may succeed briefly after Postgres stops if existing connections haven't timed out. The Eventually polling pattern handles this.
3. **Gateway vs. control-plane distinction**: The health endpoint is served by the control-plane monolith. If the gateway itself is unhealthy, tests won't reach the health endpoint at all. TC-01 serves as the precondition guard.
4. **Container restart ordering**: Stopping Postgres may cause the control-plane to crash-loop if health probes drive liveness (not just readiness). Verify the compose config uses readiness probes only for health.

---

## Version History

| Version | Date | Changes |
|---|---|---|
| 1.0 | 2026-07-28 | Initial test plan: 7 test cases covering happy path, dependency isolation (DB-only, NATS-only, both), recovery timing, and timeout enforcement |

---

## Sanitization Notice

This document is intended for sharing. The following rules apply:

- No credentials, tokens, API keys, or passwords — use placeholders (`<PASSWORD>`, `<TOKEN>`)
- No internal hostnames or IPs — use `localhost` for local deployments
- No PII (employee names, emails, account IDs)
- Open-source tool and project names are fine as-is
