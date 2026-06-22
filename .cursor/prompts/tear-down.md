# Tear Down DCM Stack

Stop and clean up a running DCM deployment.

## Command

### Default deploy directory (`/tmp/dcm-e2e`)
```bash
./scripts/deploy-dcm.sh --tear-down
```

### Custom deploy directory
```bash
./scripts/deploy-dcm.sh --control-plane-dir /path/to/deploy --tear-down
```

## What Happens

1. Stops all containers via `podman-compose down -v`
2. Force-removes any orphaned containers
3. Cleans up pods and networks created by compose
4. Deletes the deploy directory

## Verifying Cleanup

```bash
# Check no DCM containers remain
podman ps -a --filter "name=dcm" --format 'table {{.Names}}\t{{.Status}}'

# Check no leftover networks
podman network ls --filter "name=dcm"

# Check deploy directory is gone
ls /tmp/dcm-e2e 2>/dev/null && echo "Still exists" || echo "Clean"
```
