#!/usr/bin/env bash
#
# Provision an OrbStack Linux machine to run benchmarkoor with a copy-on-write
# datadir method (overlayfs). macOS has no overlayfs/zfs/schelk, so the large
# stateful prestates (e.g. config.state-actor-eest.full.amsterdam.stateful.yaml,
# ~77 GB) can't use anything but `copy` on the host — which is infeasible at that
# size. An OrbStack Linux machine has a real kernel (overlayfs works) and we run
# a DEDICATED dockerd inside it so benchmarkoor, dockerd, and the overlay mounts
# all share one filesystem namespace (the merged dir must be bind-mountable into
# the client containers — the host's shared Docker engine can't see VM-local
# overlay mounts).
#
# Prereqs: OrbStack installed (https://orbstack.dev). The repo is auto-shared
# into the machine at the same path, so no copying is needed.
#
# Usage:
#   .hack/orbstack.sh [machine-name]          # default: bench
#
# After it finishes, run benchmarkoor INSIDE the machine as root (overlayfs mount
# needs CAP_SYS_ADMIN), e.g.:
#   orb -m bench sudo benchmarkoor build \
#     --config $(pwd)/examples/configuration/config.state-actor-eest.full.amsterdam.stateful.yaml --force
#   orb -m bench sudo benchmarkoor run \
#     --config $(pwd)/examples/configuration/config.state-actor-eest.full.amsterdam.stateful.yaml --limit-instance-id=geth
set -euo pipefail

MACHINE="${1:-bench}"
DISTRO="${ORBSTACK_DISTRO:-ubuntu}"
# Build tags mirror the Makefile (avoid btrfs/devicemapper graphdrivers + gpgme).
GO_BUILD_TAGS="exclude_graphdriver_btrfs,exclude_graphdriver_devicemapper,containers_image_openpgp"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if ! command -v orb >/dev/null 2>&1; then
  echo "error: 'orb' not found — install OrbStack first (https://orbstack.dev)" >&2
  exit 1
fi

echo "==> Repo root: ${REPO_ROOT}"
echo "==> OrbStack machine: ${MACHINE} (${DISTRO})"

# 1. Create the machine if it doesn't already exist (idempotent).
if orb list 2>/dev/null | awk '{print $1}' | grep -qx "${MACHINE}"; then
  echo "==> Machine '${MACHINE}' already exists, reusing it."
else
  echo "==> Creating machine '${MACHINE}'..."
  orb create "${DISTRO}" "${MACHINE}"
fi

# 2. Provision inside the machine: dedicated dockerd + Go toolchain, then build
#    benchmarkoor natively. Everything below runs as root in the machine.
orb -m "${MACHINE}" sudo bash -s -- "${REPO_ROOT}" "${GO_BUILD_TAGS}" <<'PROVISION'
set -euo pipefail
REPO_ROOT="$1"
GO_BUILD_TAGS="$2"

echo "==> apt: docker.io + golang-go + make + git"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
# make + git let you run the repo's Makefile targets (e.g. `make build-core`,
# `make test-core`) inside the machine; git also stamps the version/commit.
apt-get install -y -qq docker.io golang-go make git

echo "==> Starting the machine's own dockerd"
systemctl enable --now docker
systemctl is-active --quiet docker || { echo "dockerd failed to start" >&2; exit 1; }

# A dedicated, VM-local docker daemon is the whole point — confirm it.
docker version --format '   dockerd {{.Server.Version}}' >/dev/null

echo "==> Go: $(go version)"
case "$(go version)" in
  *go1.2[3-9]*|*go1.[3-9][0-9]*|*go[2-9].*) : ;;
  *) echo "warning: Go < 1.23 detected; benchmarkoor needs >= 1.23. Install a newer Go." >&2 ;;
esac

echo "==> Building benchmarkoor (native, tags: ${GO_BUILD_TAGS})"
cd "${REPO_ROOT}"
GOCACHE=/root/.cache/go-build GOPATH=/root/go GOFLAGS=-mod=mod \
  go build -tags "${GO_BUILD_TAGS}" -o /usr/local/bin/benchmarkoor ./cmd/benchmarkoor
/usr/local/bin/benchmarkoor --help >/dev/null && echo "==> benchmarkoor installed at /usr/local/bin/benchmarkoor"

echo "==> Verifying overlayfs works in this machine"
d="$(mktemp -d)"; mkdir -p "$d"/{low,up,work,merged}; echo ok > "$d/low/f"
mount -t overlay overlay -o "lowerdir=$d/low,upperdir=$d/up,workdir=$d/work" "$d/merged"
docker run --rm -v "$d/merged:/m" alpine cat /m/f >/dev/null && echo "==> overlayfs + dockerd bind-mount: OK"
umount "$d/merged"; rm -rf "$d"
PROVISION

cat <<EOF

==> Done. The '${MACHINE}' machine is ready (dedicated dockerd + overlayfs + benchmarkoor).

Run benchmarkoor inside it as root (the repo is shared at ${REPO_ROOT}):

  orb -m ${MACHINE} sudo benchmarkoor build \\
    --config ${REPO_ROOT}/examples/configuration/config.state-actor-eest.full.amsterdam.stateful.yaml --force

  orb -m ${MACHINE} sudo benchmarkoor run \\
    --config ${REPO_ROOT}/examples/configuration/config.state-actor-eest.full.amsterdam.stateful.yaml --limit-instance-id=geth

Tear down with:  orb delete ${MACHINE}
EOF
