# Docker Guide

This document covers Docker requirements, setup, and usage for benchmarkoor.

## Table of Contents

- [Requirements](#requirements)
- [Running Benchmarkoor in Docker](#running-benchmarkoor-in-docker)
- [How Docker is Used](#how-docker-is-used)
- [Container Lifecycle](#container-lifecycle)
- [Network Configuration](#network-configuration)
- [Volume Management](#volume-management)
- [Resource Limits](#resource-limits)
- [Container Labels](#container-labels)
- [Cleanup](#cleanup)
- [Troubleshooting](#troubleshooting)

## Requirements

### Docker Engine

Benchmarkoor requires Docker Engine to run Ethereum execution clients. The minimum supported version is Docker 20.10+.

```bash
# Check Docker version
docker --version

# Verify Docker daemon is running
docker info
```

### Permissions

The user running benchmarkoor must have permission to interact with the Docker daemon:

```bash
# Option 1: Add user to docker group (recommended)
sudo usermod -aG docker $USER
# Log out and back in for changes to take effect

# Option 2: Run benchmarkoor with sudo (not recommended for production)
sudo benchmarkoor run --config config.yaml
```

### Docker Socket

Benchmarkoor connects to Docker via the standard socket at `/var/run/docker.sock`. If using a custom socket location, set the `DOCKER_HOST` environment variable:

```bash
export DOCKER_HOST=unix:///path/to/docker.sock
benchmarkoor run --config config.yaml
```

## Running Benchmarkoor in Docker

Benchmarkoor itself can run inside a Docker container. This is useful for CI/CD pipelines or isolated environments.

### Building the Image

```bash
# Build with default settings
docker build -t benchmarkoor .

# Build with version info
docker build \
  --build-arg VERSION=1.0.0 \
  --build-arg COMMIT=$(git rev-parse HEAD) \
  --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -t benchmarkoor .
```

### Running Benchmarkoor in Docker

When running benchmarkoor in a container, it needs access to the Docker socket to manage client containers (Docker-in-Docker pattern using sibling containers):

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v $(pwd)/results:/app/results \
  benchmarkoor run --config /app/config.yaml
```

#### With Pre-populated Data Directories

If using pre-populated data directories, mount them into the container:

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v $(pwd)/results:/app/results \
  -v /data/snapshots:/data/snapshots:ro \
  benchmarkoor run --config /app/config.yaml
```

#### With Privileged Mode (for overlayfs,zfs)

If using the `overlayfs` or `zfs` datadir method, the container needs privileged mode:

```bash
docker run --rm --privileged \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v $(pwd)/results:/app/results \
  -v /data/snapshots:/data/snapshots:ro \
  benchmarkoor run --config /app/config.yaml
```

#### With Memory Cache Dropping

If using `drop_memory_caches`, mount the proc filesystem and run with appropriate permissions:

```bash
docker run --rm --privileged \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v /proc/sys/vm/drop_caches:/host/drop_caches \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v $(pwd)/results:/app/results \
  benchmarkoor run --config /app/config.yaml
```

Then configure in your config.yaml:
```yaml
global:
  drop_caches_path: /host/drop_caches
```

## How Docker is Used

Benchmarkoor uses Docker to:

1. **Run Ethereum clients** - Each client instance runs in its own container
2. **Isolate test environments** - Each benchmark run gets fresh containers and volumes
3. **Manage networking** - Containers communicate via a dedicated Docker network
4. **Collect metrics** - Resource usage is collected via Docker Stats API or cgroups

### Container Types

| Type | Purpose | Naming Pattern |
|------|---------|----------------|
| Init container | Initialize client data directory (e.g., geth init) | `benchmarkoor-{runID}-{instanceID}-init` |
| Client container | Run the Ethereum client | `benchmarkoor-{runID}-{instanceID}` |

## Container Lifecycle

For each benchmark run, benchmarkoor follows this lifecycle:

```
1. Pull image (based on pull_policy)
      ↓
2. Create Docker volume (if not using datadir)
      ↓
3. Run init container (if required and not using datadir)
      ↓
4. Create and start client container
      ↓
5. Wait for RPC endpoint to be ready
      ↓
6. Execute benchmark tests
      ↓
7. Stop and remove container
      ↓
8. Remove volume
```

### Init Containers

Some clients (like Erigon) require an initialization step before starting. The init container:

- Uses the same image as the main container
- Runs the client's init command (e.g., `erigon init`)
- Has access to the same volumes
- Must complete successfully before the main container starts

Init containers are skipped when:
- Using pre-populated data directories (`datadirs` config)
- No genesis file is configured

### Image Pull Policies

| Policy | Behavior |
|--------|----------|
| `always` (default) | Always pull the latest image |
| `if-not-present` | Only pull if image doesn't exist locally |
| `never` | Never pull, fail if image doesn't exist |

## Network Configuration

Benchmarkoor creates a dedicated Docker bridge network for container communication.

```yaml
global:
  docker_network: benchmarkoor  # default
```

### Network Behavior

- Network is created automatically on first run (if it doesn't exist)
- All containers in a benchmark run join this network
- Containers can communicate via container name or IP
- Network persists between runs for efficiency
- Use `cleanup_on_start: true` to remove the network on startup

### Port Exposure

Client containers expose the following ports (internal to the Docker network):

| Client | RPC Port | Engine Port | Metrics Port |
|--------|----------|-------------|--------------|
| Geth | 8545 | 8551 | 6060 |
| Nethermind | 8545 | 8551 | 9091 |
| Besu | 8545 | 8551 | 9545 |
| Erigon | 8545 | 8551 | 6060 |
| Nimbus | 8545 | 8551 | 9091 |
| Reth | 8545 | 8551 | 9001 |

Ports are not mapped to the host by default. Benchmarkoor communicates with containers via their internal Docker network IP.

## Volume Management

### Docker Volumes

When not using pre-populated data directories, benchmarkoor creates a Docker volume for each run:

- **Name pattern**: `benchmarkoor-{runID}-{instanceID}`
- **Labels**: Include instance ID, client type, run ID for identification
- **Lifecycle**: Created before init container, removed after benchmark completes

### Pre-populated Data Directories

For stateful benchmarks, you can provide pre-populated data directories instead of Docker volumes. See [Data Directories](configuration.md#data-directories) in the configuration reference.

## Resource Limits

Docker resource limits control CPU and memory allocation for client containers.

### CPU Pinning

```yaml
client:
  config:
    resource_limits:
      # Option 1: Random CPU selection (different CPUs each run)
      cpuset_count: 4

      # Option 2: Specific CPUs
      cpuset: [0, 1, 2, 3]
```

The `cpuset_count` option uses Fisher-Yates shuffle to randomly select CPUs, providing variation between runs while maintaining consistent CPU count.

### Memory Limits

```yaml
client:
  config:
    resource_limits:
      memory: "16g"
      swap_disabled: true
```

When `swap_disabled: true`:
- `memory-swap` is set equal to `memory` (no swap available)
- `memory-swappiness` is set to 0

### Per-Instance Overrides

Resource limits can be overridden per instance:

```yaml
client:
  config:
    resource_limits:
      cpuset_count: 4
      memory: "16g"

  instances:
    - id: besu-latest
      client: besu
      resource_limits:
        cpuset_count: 8  # Besu gets more CPUs
        memory: "32g"    # and more memory
```

## Container Labels

All benchmarkoor-managed containers and volumes are labeled for identification:

| Label | Description |
|-------|-------------|
| `benchmarkoor.managed-by` | Always `benchmarkoor` |
| `benchmarkoor.instance` | Instance ID from config |
| `benchmarkoor.client` | Client type (geth, nethermind, etc.) |
| `benchmarkoor.run-id` | Unique run identifier |
| `benchmarkoor.type` | Container type (`init` for init containers) |

These labels are used by the cleanup command to identify orphaned resources.

## Cleanup

### Automatic Cleanup

Benchmarkoor automatically cleans up containers and volumes after each run. If a run is interrupted, resources may be left behind.

### Manual Cleanup

Use the cleanup command to remove orphaned resources:

```bash
# List resources that would be removed
benchmarkoor cleanup

# Remove resources (with confirmation)
benchmarkoor cleanup

# Remove resources without confirmation
benchmarkoor cleanup --force
```

The cleanup command removes:
- Docker containers with `benchmarkoor.managed-by=benchmarkoor` label
- Docker volumes with `benchmarkoor.managed-by=benchmarkoor` label
- Orphaned ZFS clones and snapshots (if using ZFS datadir method)
- Orphaned overlayfs mounts (if using overlayfs datadir method)

### Cleanup on Start

Enable automatic cleanup at startup:

```yaml
global:
  cleanup_on_start: true
```

This removes any leftover resources before starting a new benchmark run.

## Troubleshooting

### Cannot Connect to Docker Daemon

```
Error: creating docker manager: Cannot connect to the Docker daemon
```

**Solutions:**
1. Ensure Docker daemon is running: `sudo systemctl start docker`
2. Check socket permissions: `ls -la /var/run/docker.sock`
3. Add user to docker group: `sudo usermod -aG docker $USER`

### Image Pull Failures

```
Error: pulling image: pull access denied
```

**Solutions:**
1. Check image name is correct
2. Login to registry if private: `docker login`
3. Check network connectivity
4. Try `pull_policy: never` if image exists locally

### Container Startup Failures

```
Error: starting container: OCI runtime error
```

**Solutions:**
1. Check container logs: `docker logs benchmarkoor-{runID}-{instanceID}`
2. Verify resource limits are within system capacity
3. Check if ports are already in use
4. Ensure sufficient disk space

### Network Issues

```
Error: container not connected to network
```

**Solutions:**
1. Remove and recreate network: `docker network rm benchmarkoor`
2. Check for network conflicts: `docker network ls`
3. Enable `cleanup_on_start: true` in config

### Resource Limit Errors

```
Error: cpuset_count (16) exceeds available CPUs (8)
```

**Solutions:**
1. Reduce `cpuset_count` to available CPU count
2. Use `cpuset` with specific valid CPU IDs
3. Check available CPUs: `nproc` or `lscpu`

### Volume Mount Failures

```
Error: failed to create shim: mount failed
```

**Solutions:**
1. Ensure source directory exists and is readable
2. Check SELinux/AppArmor policies
3. For overlayfs: ensure running as root or with proper permissions
4. For bind mounts: verify absolute paths are used
