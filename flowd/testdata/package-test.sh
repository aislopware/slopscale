#!/bin/bash
# Installs a slopscale-flowd deb in a Debian container booted with systemd
# and checks that the hardened unit counts and names a tailnet node's HTTPS
# request (package-check.sh). Needs Docker and the internet.
#
#   goreleaser release --snapshot --clean --skip=before,ko,sign,publish,validate
#   flowd/testdata/package-test.sh dist/slopscale-flowd_<version>_linux_<docker arch>.deb
set -euo pipefail

deb=$(realpath "$1")
here=$(cd "$(dirname "$0")" && pwd)
name=flowd-package-test

docker rm -f "$name" >/dev/null 2>&1 || true
trap 'docker rm -f "$name" >/dev/null' EXIT

docker run -d --name "$name" --privileged --cgroupns=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
  -v "$deb":/opt/slopscale-flowd.deb:ro \
  -v "$here/package-check.sh":/package-check.sh:ro \
  debian:trixie bash -c 'apt-get update -qq &&
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq systemd systemd-sysv iproute2 nftables curl jq zstd >/dev/null &&
    exec /sbin/init' >/dev/null

until docker exec "$name" test -x /usr/bin/zstd 2>/dev/null && docker exec "$name" test -e /run/systemd/system; do
  sleep 2
done

docker exec "$name" /package-check.sh
