# KubeVirt Service Provider E2E Testing

This document describes the E2E test suite for the KubeVirt Service Provider (FLPATH-2897).

## Branches

| Branch | Contents |
|--------|----------|
| `kubevirt-provider-tests-titan90` | **This branch** — default green suite (ready for CI / day-to-day runs) |
| `kubevirt-provider-tests-deferred` | Disruptive + intentionally skipped cases (`DCM_DISRUPTIVE=1`, SP/fixture gaps) |

Deferred coverage (TC-02/03/05/08/17/20/28a/b/d/e, TC-30, bad storage-class error path) lives only on `kubevirt-provider-tests-deferred`. See that branch’s `KUBEVIRT_TESTING.md` for the full deferred table.

## Overview

The KubeVirt SP test suite validates VM lifecycle management through the DCM platform, including:
- Direct KubeVirt SP API tests (health, CRUD, validation, labels, concurrency)
- Full platform integration tests (catalog → policy → placement → VM provisioning → delete)
- VM status monitoring and NATS event verification

## Test Files

| File | Purpose | Labels |
|------|---------|--------|
| `sp_kubevirt_helpers_test.go` | Helpers, VM builders, cluster/label utilities | — |
| `sp_kubevirt_api_test.go` | Direct SP API (health, CRUD, validation, concurrency, storage) | `sp`, `kubevirt` |
| `sp_kubevirt_status_test.go` | NATS status events + GET status | `sp`, `kubevirt`, `nats` |
| `core_platform_kubevirt_test.go` | Catalog → Running → delete lifecycle | `core`, `platform`, `kubevirt` |

## Prerequisites

- OpenShift/K8s with KubeVirt/CNV (green suite uses containerDisk; StorageClass not required)
- DCM stack + KubeVirt SP (`./scripts/deploy-dcm.sh --kubevirt-service-provider ...`)
- NATS for status tests
- `oc`/`kubectl` access for cluster assertions

## Running Tests

```bash
export KUBECONFIG=/path/to/kubeconfig
export DCM_GATEWAY_URL=http://localhost:8080/api/v1alpha1
export DCM_KUBEVIRT_SP_URL=http://localhost:8081/api/v1alpha1
export DCM_NATS_URL=nats://localhost:4222
# Same NS as deploy --kubevirt-vm-namespace (SP compose uses KUBERNETES_NAMESPACE)
export KUBERNETES_NAMESPACE=vms
# Optional; helpers also accept KUBEVIRT_VM_NAMESPACE

make test-kubevirt-sp
```

## Coverage (FLPATH-2897) — green suite

| TC | Status | Notes |
|----|--------|-------|
| TC-01 | Implemented | Select vm provider by `/api/v1alpha1/vms` endpoint; assert registration fields |
| TC-04 | Implemented | Health healthy |
| TC-06 | Implemented | Create + cluster labels |
| TC-07 | Implemented | Custom `?id=` |
| TC-09 | Implemented | Empty/missing/invalid memory |
| TC-10 | Implemented | Duplicate id → 409 |
| TC-11 | Implemented | Domain resources present on cluster VM |
| TC-12 | Implemented | GET existing |
| TC-13 | Implemented | GET missing → 404 problem+json (Skip on 500: FLPATH-4752) |
| TC-14 | Implemented | GET `spec.status` phase (Skip on empty/zeroed spec: FLPATH-4754) |
| TC-15 | Implemented | List contains 3 created VMs; per-create cleanup |
| TC-16 | Implemented | Unlabeled cluster VM excluded; label get must succeed |
| TC-18 | Implemented | DELETE 204; cluster NotFound; GET 404 (Skip API assert on 500: FLPATH-4752) |
| TC-19 | Implemented | DELETE missing → 404 (Skip on 500: FLPATH-4752) |
| TC-21 | Implemented | NATS id-filtered transitions; Fail if Pending/started order incomplete |
| TC-22 | Implemented | CloudEvent schema; filter by VM id |
| TC-23 | Implemented | Catalog → Running + cluster labels |
| TC-24 | Implemented | Delete instance → STI gone + VM gone |
| TC-25 | Implemented | Malformed JSON / content-type |
| TC-26 | Implemented | DCM labels on VM + template |
| TC-27 | Implemented | Instance id label round-trip |
| TC-28c | Implemented | OpenAPI 1MB accepted; maps to cluster 1M |
| TC-29 | Implemented | Invalid memory unit |
| TC-31 | Implemented | 5 parallel creates |
| TC-32 | Implemented | External halt → Stopped/Succeeded/Stopping + runStrategy Halted |
| TC-33 | Implemented | Boot disk maps to containerDisk (PVC/SC deferred) |

## Notes

- Cluster VMs use `GenerateName: dcm-`; look up by label `dcm.project/dcm-instance-id=<id>`.
- Catalog fields must use `storage.disks` as an array (control-plane nested_map bug with `disks[0]`).
- STI status casing is `Running` (not `RUNNING`).
- Specs clean up via Ginkgo `DeferCleanup` / `deleteTestVM` (no shared leftover-cleaner in-repo).
- Disruptive / Skip-stub cases: checkout `kubevirt-provider-tests-deferred`
- Contract source of truth: SP OpenAPI (`api/v1alpha1/openapi.yaml`); ADR examples that predate the schema (flat create body, `/api/v1/vm`) are superseded by OpenAPI.
- Known SP gaps tracked in Jira: [FLPATH-4751](https://redhat.atlassian.net/browse/FLPATH-4751) (OpenAPI 400 content-type), [FLPATH-4752](https://redhat.atlassian.net/browse/FLPATH-4752) (missing VM → 500 not 404), [FLPATH-4754](https://redhat.atlassian.net/browse/FLPATH-4754) (GET/CREATE returns empty/zeroed `spec`, no `status`).
