# Deploy the DCM Stack

Deploy the full DCM stack for E2E testing using `scripts/deploy-dcm.sh`.

## Prerequisites

1. **Required tools**: `git`, `podman`, `podman-compose`, `curl`, `jq`
2. **For KubeVirt provider**: `oc` (OCP cluster with CNV installed)
3. **For k8s container provider**: `oc` or `kubectl` (any Kubernetes cluster)

## Commands

### Default Deploy
```bash
./scripts/deploy-dcm.sh
```

### Deploy a Specific Version
```bash
# Pin all images to an explicit tag (auto-derives release branch)
./scripts/deploy-dcm.sh --version v0.1.0-rc.1

# Auto-resolve the latest semver tag from Quay.io
./scripts/deploy-dcm.sh --version release

# Explicit version with a custom control-plane branch
./scripts/deploy-dcm.sh --version v0.1.0-rc.1 --control-plane-branch my-branch
```

### Deploy from a Different Branch
```bash
./scripts/deploy-dcm.sh --control-plane-branch feature-x
```

### Deploy from a Fork
```bash
./scripts/deploy-dcm.sh --control-plane-repo https://github.com/myfork/control-plane.git
```

### Deploy to a Custom Directory
```bash
./scripts/deploy-dcm.sh --control-plane-dir /tmp/my-dcm-deploy
```

### Deploy with Auto-Cleanup on Failure
```bash
./scripts/deploy-dcm.sh --cleanup-on-failure
```

### Deploy with k8s Container Service Provider
```bash
# Auto-detects cluster from existing oc/kubectl session
./scripts/deploy-dcm.sh --k8s-container-service-provider

# With explicit kubeconfig
./scripts/deploy-dcm.sh --k8s-container-service-provider --kubeconfig ~/.kube/config
```

### Deploy with KubeVirt Service Provider
```bash
./scripts/deploy-dcm.sh --kubevirt-service-provider --kubeconfig ~/.kube/config
```

### Deploy with All Service Providers
```bash
./scripts/deploy-dcm.sh --all-service-providers --kubeconfig ~/.kube/config
```

### Deploy with oc login Credentials
```bash
./scripts/deploy-dcm.sh --all-service-providers \
    --cluster-api https://api.cluster.example.com \
    --cluster-password mypassword

# Or via environment variables
OPENSHIFT_API=https://api.cluster.example.com \
OPENSHIFT_PASSWORD=mypassword \
./scripts/deploy-dcm.sh --kubevirt-service-provider
```

## Cluster Authentication

When any service provider is enabled, the script resolves cluster access in this order:
1. Explicit `--kubeconfig PATH` (or `KUBECONFIG` env var)
2. Existing `oc`/`kubectl` session (auto-detected)
3. `oc login` with `--cluster-api` + `--cluster-password`

## Environment Variable Overrides

| Variable | Flag equivalent |
|----------|----------------|
| `DCM_VERSION` | `--version` |
| `CONTROL_PLANE_REPO` | `--control-plane-repo` |
| `CONTROL_PLANE_BRANCH` | `--control-plane-branch` |
| `CONTROL_PLANE_TMP_DIR` | `--control-plane-dir` |
| `KUBECONFIG` | `--kubeconfig` |
| `KUBEVIRT_VM_NAMESPACE` | `--kubevirt-vm-namespace` |
| `K8S_CONTAINER_SP_NAMESPACE` | `--k8s-container-namespace` |
| `OPENSHIFT_API` | `--cluster-api` |
| `OPENSHIFT_USERNAME` | `--cluster-username` |
| `OPENSHIFT_PASSWORD` | `--cluster-password` |

Flags take precedence over environment variables.

## What Happens

1. Clones control-plane (`deploy/compose.yaml`)
2. Runs `podman-compose up -d`
3. Verifies all containers are running
4. Polls `/api/v1alpha1/health` (90s timeout)
5. Resolves container images to git commit SHAs via Quay.io API
6. Writes `dcm-versions.json`

## Output

- Stack available at `http://localhost:8080`
- Version info written to `dcm-versions.json`
