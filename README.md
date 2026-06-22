# DCM Utilities

Common scripts and tooling shared across the [DCM](https://github.com/dcm-project) ecosystem. Provides the E2E deploy script for bringing up the full DCM stack locally and a Ginkgo/Gomega E2E test suite that validates the stack through the control-plane API and DCM CLI.

## Contents

| Path | Description |
|------|-------------|
| `scripts/deploy-dcm.sh` | Deploy, health-check, and tear down the full DCM stack via podman-compose |
| `providers/` | Service provider registry — one `.conf` file per provider |
| `dcm-versions.json` | Example output of container version resolution (gitignored) |
| `tests/run-e2e.sh` | Test harness: deploy, run tests, teardown |
| `tests/e2e/` | Ginkgo/Gomega E2E test suite |
| `test-plans/` | E2E test plans and results for DCM service providers |
| `Makefile` | Convenience targets (`make help` to list all) |

### `dcm-versions.json`

Both deploy mode and `--running-versions` produce a `dcm-versions.json` mapping each container image to its digest and the git commit SHA that built it (resolved via the Quay.io tag API). Third-party images show `null` for `git_sha`.

```json
{
  "quay.io/dcm-project/control-plane:latest": {
    "image_digest": "sha256:1cdf5482f586ce513724074c0a132b718672d2be5cbae600a47e94324078b01e",
    "git_sha": "2388248"
  },
  "docker.io/library/postgres:16-alpine": {
    "image_digest": "sha256:b7587f3cb74f4f4b2a4f9d67f052edbf95eb93f4fec7c5ada3792546caaf7383",
    "git_sha": null
  }
}
```

## E2E Deploy Script

`scripts/deploy-dcm.sh` automates the full DCM stack lifecycle for E2E testing:

1. Clones the [control-plane](https://github.com/dcm-project/control-plane) repo (`deploy/compose.yaml`)
2. Starts all services with `podman-compose up`
3. Polls health endpoints until every service responds 2xx
4. Resolves running container images to git commit SHAs via the Quay.io API

### Prerequisites

- `git`, `podman`, `podman-compose`, `curl`, `jq`
- `oc` (for KubeVirt/ACM providers; also used for `oc login` auth)
- `oc` or `kubectl` (for k8s container provider — either works)
- `oc` + `jq` (for `--deploy-acm` / `--deploy-mce`)

### Quick Start

```bash
# 1. Deploy the full DCM stack (no providers)
./scripts/deploy-dcm.sh

# 2. Deploy a specific release version (auto-derives release branch)
./scripts/deploy-dcm.sh --version v0.1.0-rc.1

# 3. Deploy the latest release (resolves from Quay.io)
./scripts/deploy-dcm.sh --version release

# 4. Deploy with the k8s container service provider (auto-detects cluster)
./scripts/deploy-dcm.sh --k8s-container-service-provider

# 5. Deploy with KubeVirt + explicit kubeconfig
./scripts/deploy-dcm.sh --kubevirt-service-provider --kubeconfig ~/.kube/config

# 6. Deploy all providers, logging in via oc
./scripts/deploy-dcm.sh --all-service-providers \
    --cluster-api https://api.cluster.example.com --cluster-password secret

# 7. Deploy ACM cluster provider (install ACM first if needed)
./scripts/deploy-dcm.sh --acm-cluster-service-provider --deploy-acm --kubeconfig ~/.kube/config

# 8. Tear down when done
./scripts/deploy-dcm.sh --tear-down
```

Run `./scripts/deploy-dcm.sh --help` for all flags and environment variable overrides.

## E2E Tests

The test suite uses [Ginkgo](https://onsi.github.io/ginkgo/) and [Gomega](https://onsi.github.io/gomega/) to validate the full DCM stack through the control-plane API and the DCM CLI.

### Quick Start

```bash
# One command: deploy the stack, run all tests, tear down
make test-e2e-full
```

This runs the full lifecycle via `tests/run-e2e.sh`: deploys the DCM stack with `podman-compose`, auto-downloads the CLI binary from GitHub releases, executes all E2E tests (health checks, API CRUD, SP tests, CLI commands), and tears down afterward.

### Step-by-Step

```bash
make e2e-up        # Deploy the stack
make test-e2e      # Run all tests (stack must be running)
make test-smoke    # Run health checks + CLI version only
make test-cli      # Run CLI tests only
make test-sp       # Run container SP tests (SP must be deployed)
make test-acm-sp   # Run ACM cluster SP tests (ACM SP must be deployed)
make test-core     # Run core platform tests (full provisioning flow)
make e2e-down      # Tear down
make download-cli  # Download latest DCM CLI without running tests

# See all targets
make help
```

### Prerequisites

- Go 1.23+
- `podman`, `podman-compose`, `curl`, `jq`, `git`
- `gh` CLI ([cli.github.com](https://cli.github.com)) — for auto-downloading the DCM CLI binary
- **DCM CLI binary** (for CLI tests) — auto-downloaded from [GitHub releases](https://github.com/dcm-project/cli/releases), or set `DCM_CLI_PATH`

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DCM_GATEWAY_URL` | `http://localhost:8080/api/v1alpha1` | Control plane API base URL |
| `DCM_CONTAINER_SP_URL` | `http://localhost:8082/api/v1alpha1` | Container SP direct URL (requires published port) |
| `DCM_ACM_CLUSTER_SP_URL` | `http://localhost:8083/api/v1alpha1` | ACM Cluster SP direct URL (requires published port) |
| `DCM_NATS_URL` | `nats://localhost:4222` | NATS server URL for status event tests |
| `DCM_CLI_PATH` | (auto-resolved) | Path to `dcm` CLI binary |
| `JUNIT_REPORT` | (none) | JUnit XML report filename (e.g. `make test-e2e JUNIT_REPORT=results.xml`) |

### Test Harness Flags

The test harness (`tests/run-e2e.sh`) supports additional flags for fine-grained control:

```bash
./tests/run-e2e.sh --skip-deploy              # Stack is already running
./tests/run-e2e.sh --skip-teardown            # Leave stack running after tests
./tests/run-e2e.sh --skip-cli                 # Skip CLI binary resolution
./tests/run-e2e.sh --dcm-cli-path ~/bin/dcm   # Use a specific CLI binary
./tests/run-e2e.sh --label-filter smoke        # Run only smoke tests
./tests/run-e2e.sh --gateway-url http://...    # Override control plane API URL
./tests/run-e2e.sh --junit-report results.xml  # Write JUnit XML report

# Service provider tests
./tests/run-e2e.sh --k8s-container-service-provider --cluster-api https://api.example.com:6443
./tests/run-e2e.sh --skip-deploy --label-filter "sp && container"

# ACM cluster SP tests
./tests/run-e2e.sh --acm-cluster-service-provider --kubeconfig ~/.kube/config
./tests/run-e2e.sh --skip-deploy --label-filter "sp && acm-cluster"
```

## Cursor Integration

This repo includes configuration for [Cursor](https://cursor.sh) and [Claude Code](https://claude.ai/code):

| Path | Purpose |
|------|---------|
| `CLAUDE.md` | Consolidated project context (works in any AI tool) |
| `.cursor/rules/` | Auto-loaded context rules for Cursor |
| `.cursor/prompts/` | Task-specific prompt templates (use `@<name>` in Cursor) |
| `.cursor/agents/` | Specialized agent definitions |

Available prompts: `@deploy-dcm`, `@tear-down`, `@check-versions`, `@troubleshoot-deploy`, `@maintain-pr-summary`.

## Development

### Linting

Shell scripts are linted with [ShellCheck](https://www.shellcheck.net/). CI runs ShellCheck automatically on PRs against changed `*.sh` files.

```bash
shellcheck scripts/*.sh tests/*.sh
```
## License

Apache 2.0 — see [LICENSE](LICENSE).
