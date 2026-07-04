#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)
DIST_DIR=${DIST_DIR:-"$ROOT_DIR/dist"}
VERSION=${VERSION:-}
COMMIT=${COMMIT:-}
DATE=${DATE:-}

usage() {
	cat <<'USAGE'
Build Slipway release artifacts.

Usage:
  scripts/release/build.sh [version]

Environment:
  VERSION   Version string to embed. A positional version overrides this.
  COMMIT    Commit string to embed. Defaults to current git short SHA.
  DATE      Build date to embed. Defaults to current UTC time.
  DIST_DIR  Output directory. Defaults to ./dist.
USAGE
}

if [ "$#" -gt 1 ]; then
	printf 'scripts/release/build.sh: expected at most one version argument\n' >&2
	exit 2
fi
if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
	usage
	exit 0
fi
if [ "$#" -eq 1 ]; then
	VERSION=$1
fi

if [ -z "$VERSION" ]; then
	if VERSION=$(git -C "$ROOT_DIR" describe --tags --exact-match 2>/dev/null); then
		:
	else
		VERSION=$(git -C "$ROOT_DIR" describe --tags --always --dirty 2>/dev/null || printf 'dev')
	fi
fi

if [ -z "$COMMIT" ]; then
	COMMIT=$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || printf 'unknown')
fi

if [ -z "$DATE" ]; then
	DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
fi

LDFLAGS="-s -w"
LDFLAGS="$LDFLAGS -X github.com/s4m1nd/slipway/internal/cli.Version=$VERSION"
LDFLAGS="$LDFLAGS -X github.com/s4m1nd/slipway/internal/cli.Commit=$COMMIT"
LDFLAGS="$LDFLAGS -X github.com/s4m1nd/slipway/internal/cli.Date=$DATE"

for command in go git; do
	command -v "$command" >/dev/null 2>&1 || {
		printf 'scripts/release/build.sh: %s is required\n' "$command" >&2
		exit 1
	}
done

checksum_files() {
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$@"
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$@"
	else
		printf 'scripts/release/build.sh: shasum or sha256sum is required\n' >&2
		exit 1
	fi
}

mkdir -p "$DIST_DIR"
rm -f "$DIST_DIR"/slipway_* "$DIST_DIR"/checksums.txt

for target in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64; do
	goos=${target%/*}
	goarch=${target#*/}
	output="$DIST_DIR/slipway_${goos}_${goarch}"
	printf 'building %s %s -> %s\n' "$goos" "$goarch" "$output"
	(
		cd "$ROOT_DIR"
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" -o "$output" ./cmd/slipway
	)
done

(
	cd "$DIST_DIR"
	checksum_files slipway_* > checksums.txt
)

printf 'wrote release artifacts to %s\n' "$DIST_DIR"
printf 'version=%s commit=%s date=%s\n' "$VERSION" "$COMMIT" "$DATE"
