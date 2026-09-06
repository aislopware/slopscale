# Build from source

!!! warning "Community documentation"

    This page is not actively maintained by the headscale authors and is
    written by community members. It is _not_ verified by headscale developers.

    **It might be outdated and it might miss necessary steps**.

Headscale can be built from source using the latest version of [Go](https://golang.org) and [Buf](https://buf.build)
(Protobuf generator). See the [Contributing section in the GitHub
README](https://github.com/juanfont/headscale#contributing) for more information.

## OpenBSD

### Install from source

```shell
# Install prerequisites: Go, git, GNU make and a C compiler (clang ships
# with the base system; the SQLite driver is C compiled through cgo)
pkg_add go git gmake

git clone https://github.com/juanfont/headscale.git

cd headscale

# optionally checkout a release
# option a. you can find official release at https://github.com/juanfont/headscale/releases/latest
# option b. get latest tag, this may be a beta release
latestTag=$(git describe --tags `git rev-list --tags --max-count=1`)

git checkout $latestTag

# gmake build passes the compiler flags from sqlite.cflags that the server expects
gmake build

# make it executable
chmod a+x headscale

# copy it to /usr/local/sbin
cp headscale /usr/local/sbin
```
