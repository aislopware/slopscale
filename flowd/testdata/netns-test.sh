#!/usr/bin/env bash
# Runs slopscale-flowd's tests on a real Linux kernel: connection tracking,
# the packet capture and the resolver against real traffic between network
# namespaces, in a throwaway privileged container.
#
# Usage: flowd/testdata/netns-test.sh [go test flags...]
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
go_version=$(awk '/^go / { print $2; exit }' "$root/go.mod")

docker run --rm --privileged \
  -v "$root:/src" \
  -v "$(go env GOMODCACHE):/go/pkg/mod" \
  -v slopscale-flowd-gocache:/root/.cache/go-build \
  -w /src \
  -e FLOWD_NETNS_TEST=1 \
  -e DEBIAN_FRONTEND=noninteractive \
  "golang:${go_version}" \
  bash -c 'apt-get update -qq && apt-get install -qq -y iproute2 nftables >/dev/null &&
    go test -count=1 "$@" ./flowd/...' _ "$@"
