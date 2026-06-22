# Check Running Versions

Query running DCM containers and resolve their image digests to git commit SHAs.

## Command

### Default deploy directory
```bash
./scripts/deploy-dcm.sh --running-versions
```

### Custom deploy directory
```bash
./scripts/deploy-dcm.sh --control-plane-dir /path/to/deploy --running-versions
```

## Prerequisites

- DCM stack must already be deployed and running
- Requires: `podman`, `podman-compose`, `curl`, `jq`

## Output

Writes `dcm-versions.json` to the current directory. Example:

```json
{
  "quay.io/dcm-project/control-plane:latest": {
    "image_digest": "sha256:1cdf5482f586...",
    "git_sha": "2388248"
  },
  "docker.io/library/postgres:16-alpine": {
    "image_digest": "sha256:b7587f3cb74f...",
    "git_sha": null
  }
}
```

- DCM images: digest + git commit SHA (resolved via Quay.io tag API)
- Third-party images (postgres, etc.): digest only, `git_sha` is `null`

## Manual Inspection

```bash
# List running containers with images
podman ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'

# Inspect a specific container's image
podman inspect --format '{{.ImageName}} {{.ImageDigest}}' <container-id>
```
