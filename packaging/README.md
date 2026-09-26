# Packaging

We use [nFPM](https://nfpm.goreleaser.com/) for making `.deb` packages.

This folder contains files we need to package with these releases.

`apt/` builds the signed apt index each release carries, so the release
itself is the apt repository; see the comment in `apt/build-repo.sh`.
