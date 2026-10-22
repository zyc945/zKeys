#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
version=${1:?Usage: scripts/build-release.sh VERSION}
case "$version" in ''|*[!A-Za-z0-9._-]*) echo 'Invalid version' >&2; exit 1;; esac
out="dist/$version"
if [ -e "$out" ]; then echo "$out already exists; move it aside before rebuilding" >&2; exit 1; fi
mkdir -p "$out"
for platform in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    target_os=${platform%/*}
    target_arch=${platform#*/}
    stage=$(mktemp -d "${TMPDIR:-/tmp}/zkeys-build.XXXXXXXX")
    trap 'rm -rf "$stage"' EXIT HUP INT TERM
    for command in zkey zwork; do
        CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build -trimpath -ldflags='-s -w' -o "$stage/$command" "./cmd/$command"
    done
    cp README.md "$stage/README.md"
    COPYFILE_DISABLE=1 tar -czf "$out/zkeys-$version-$target_os-$target_arch.tar.gz" -C "$stage" zkey zwork README.md
    rm -rf "$stage"
    trap - EXIT HUP INT TERM
done
(cd "$out"; if command -v sha256sum >/dev/null 2>&1; then sha256sum ./*.tar.gz > SHA256SUMS; else shasum -a 256 ./*.tar.gz > SHA256SUMS; fi)
printf 'Release files: %s\n' "$out"
