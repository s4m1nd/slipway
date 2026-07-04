#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/check-local-docker.sh

Required environment:
  SLIPWAY_LIVE_IMAGE=registry/owner/name

This preflight only checks local Docker availability and image shape.
Slipway performs registry login during deploy.
EOF
}

image=${SLIPWAY_LIVE_IMAGE:-}
case "$image" in
"" | *" "* | *"	"* | *"
"*)
	echo "SLIPWAY_LIVE_IMAGE must be a non-empty single-line image name" >&2
	usage
	exit 2
	;;
esac

registry=${image%%/*}
rest=${image#*/}
owner=${rest%%/*}
name=${rest#*/}
if [ "$registry" = "$image" ] || [ "$owner" = "$rest" ] || [ -z "$registry" ] || [ -z "$owner" ] || [ -z "$name" ]; then
	echo "SLIPWAY_LIVE_IMAGE must look like registry/owner/name" >&2
	exit 2
fi

command -v docker >/dev/null 2>&1 || {
	echo "docker CLI is required for the live deploy build/push" >&2
	exit 2
}

docker info >/dev/null 2>&1 || {
	echo "docker daemon is not reachable; start Docker before running the live deploy" >&2
	exit 2
}

echo "docker CLI and daemon are reachable"
echo "Docker login will be attempted by Slipway during deploy"
