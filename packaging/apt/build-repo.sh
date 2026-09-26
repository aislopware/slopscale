#!/usr/bin/env bash
# Builds the index of the flat apt repository every release carries next to
# its .deb packages. GitHub redirects releases/latest/download/<asset> to the
# latest release's asset, so the source
#
#   deb [signed-by=/usr/share/keyrings/slopscale-archive-keyring.gpg] https://github.com/aislopware/slopscale/releases/latest/download ./
#
# follows new releases without a host of its own, and lists only the latest
# version. Prereleases are skipped by that redirect.
#
# The signing key is the armored private key in APT_SIGNING_KEY; the index is
# checked against slopscale-archive-keyring.gpg next to this script, so a key
# that does not match the published keyring fails the release instead of
# every apt update.
#
# Usage: build-repo.sh <out dir> <package.deb>...
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: $0 <out dir> <package.deb>..." >&2
  exit 2
fi

if [ -z "${APT_SIGNING_KEY:-}" ]; then
  echo "APT_SIGNING_KEY is not set" >&2
  exit 2
fi

here=$(cd "$(dirname "$0")" && pwd)
keyring="$here/slopscale-archive-keyring.gpg"
out=$(mkdir -p "$1" && cd "$1" && pwd)
shift

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

export GNUPGHOME="$work/gnupg"
mkdir -m 700 "$GNUPGHOME"
gpg --batch --quiet --import <<<"$APT_SIGNING_KEY"

# apt-ftparchive recurses and writes paths relative to where it runs, so it
# runs in a directory holding only the packages: Filename is then ./<deb>,
# which apt resolves against the source URL, the release's asset list.
mkdir "$work/debs"
cp "$@" "$work/debs/"
(cd "$work/debs" && apt-ftparchive packages .) >"$out/Packages"
gzip -9nk "$out/Packages"

archs=$(sed -n 's/^Architecture: //p' "$out/Packages" | sort -u | tr '\n' ' ')
(
  cd "$out"
  apt-ftparchive \
    -o APT::FTPArchive::Release::Origin=slopscale \
    -o APT::FTPArchive::Release::Label=slopscale \
    -o APT::FTPArchive::Release::Architectures="${archs% }" \
    -o APT::FTPArchive::Release::Description="slopscale and slopscale-flowd, latest release" \
    release . >"$work/Release"
)
mv "$work/Release" "$out/Release"

gpg --batch --yes --digest-algo SHA512 --clearsign --output "$out/InRelease" "$out/Release"
gpg --batch --yes --digest-algo SHA512 --armor --detach-sign --output "$out/Release.gpg" "$out/Release"

gpgv --keyring "$keyring" "$out/InRelease"
gpgv --keyring "$keyring" "$out/Release.gpg" "$out/Release"

cp "$keyring" "$out/"
ls -l "$out"
