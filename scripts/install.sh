#!/bin/sh
set -eu

REPO=${SLIPWAY_REPO:-s4m1nd/slipway}
VERSION=${SLIPWAY_VERSION:-latest}
BIN_DIR=${SLIPWAY_INSTALL_DIR:-/usr/local/bin}
DRY_RUN=0

usage() {
	cat <<'USAGE'
Install Slipway from GitHub Releases.

Usage:
  install.sh [--version <tag>|latest] [--repo <owner/repo>] [--bin-dir <dir>] [--dry-run]

Options:
  --version <tag>   Release tag to install. Defaults to latest.
  --repo <repo>     GitHub repository. Defaults to s4m1nd/slipway.
  --bin-dir <dir>   Installation directory. Defaults to /usr/local/bin.
  --dry-run         Print the resolved download and install paths without network or file writes.
  -h, --help        Show this help.

Environment:
  SLIPWAY_VERSION      Default release tag.
  SLIPWAY_REPO         Default GitHub repository.
  SLIPWAY_INSTALL_DIR  Default installation directory.
USAGE
}

die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

need_value() {
	flag=$1
	value=${2:-}
	[ -n "$value" ] || die "$flag requires a value"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			need_value "$1" "${2:-}"
			VERSION=$2
			shift 2
			;;
		--repo)
			need_value "$1" "${2:-}"
			REPO=$2
			shift 2
			;;
		--bin-dir)
			need_value "$1" "${2:-}"
			BIN_DIR=$2
			shift 2
			;;
		--dry-run)
			DRY_RUN=1
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			die "unknown argument: $1"
			;;
	esac
done

case "$(uname -s)" in
	Darwin)
		os=darwin
		;;
	Linux)
		os=linux
		;;
	*)
		die "unsupported OS: $(uname -s)"
		;;
esac

case "$(uname -m)" in
	x86_64 | amd64)
		arch=amd64
		;;
	arm64 | aarch64)
		arch=arm64
		;;
	*)
		die "unsupported architecture: $(uname -m)"
		;;
esac

asset="slipway_${os}_${arch}"
if [ "$VERSION" = "latest" ]; then
	base_url="https://github.com/${REPO}/releases/latest/download"
else
	base_url="https://github.com/${REPO}/releases/download/${VERSION}"
fi
asset_url="${base_url}/${asset}"
checksum_url="${base_url}/checksums.txt"
install_path="${BIN_DIR}/slipway"

if [ "$DRY_RUN" -eq 1 ]; then
	printf 'Slipway installer dry run\n'
	printf '  repo:      %s\n' "$REPO"
	printf '  version:   %s\n' "$VERSION"
	printf '  platform:  %s/%s\n' "$os" "$arch"
	printf '  binary:    %s\n' "$asset_url"
	printf '  checksums: %s\n' "$checksum_url"
	printf '  install:   %s\n' "$install_path"
	printf 'No network requests or file writes were made.\n'
	exit 0
fi

for command in curl install mktemp; do
	command -v "$command" >/dev/null 2>&1 || die "$command is required"
done

verify_checksum_file() {
	checksum_file=$1
	if command -v shasum >/dev/null 2>&1; then
		shasum -a 256 -c "$checksum_file"
	elif command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c "$checksum_file"
	else
		die "shasum or sha256sum is required"
	fi
}

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

printf 'Downloading %s\n' "$asset_url"
curl --fail --silent --show-error --location "$asset_url" --output "$tmp_dir/$asset"
curl --fail --silent --show-error --location "$checksum_url" --output "$tmp_dir/checksums.txt"

if ! grep -E "[[:space:]]${asset}$" "$tmp_dir/checksums.txt" > "$tmp_dir/checksum.txt"; then
	die "checksums.txt does not include $asset"
fi

(
	cd "$tmp_dir"
	verify_checksum_file checksum.txt
)

mkdir -p "$BIN_DIR"
install -m 0755 "$tmp_dir/$asset" "$install_path"
printf 'Installed Slipway to %s\n' "$install_path"
