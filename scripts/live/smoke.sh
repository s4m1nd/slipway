#!/bin/sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)
cd "$ROOT_DIR"

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/smoke.sh [user@host] [--print-commands]

Runs a live nginx smoke test:
  validate -> provision -> deploy v1 -> status -> deploy v2 -> rollback -> status

Required environment:
  SLIPWAY_LIVE_IMAGE
  SLIPWAY_REGISTRY_SERVER
  SLIPWAY_REGISTRY_USERNAME

Also required:
  REGISTRY_PASSWORD, unless SLIPWAY_LIVE_SECRETS_FETCH is set

Optional environment:
  SLIPWAY_LIVE_HOST=203.0.113.10
  SLIPWAY_LIVE_SSH_USER=root
  SLIPWAY_LIVE_ROUTE_HOST=$SLIPWAY_LIVE_HOST
  SLIPWAY_LIVE_SECRETS_FETCH=
  SLIPWAY_LIVE_CONFIG=.tmp/live-nginx/slipway.yml
EOF
}

print_commands=0
target_arg=

while [ $# -gt 0 ]; do
	case "$1" in
	--print-commands)
		print_commands=1
		;;
	-h | --help)
		usage
		exit 0
		;;
	-*)
		echo "unknown option: $1" >&2
		usage
		exit 2
		;;
	*)
		if [ -n "$target_arg" ]; then
			echo "unexpected argument: $1" >&2
			usage
			exit 2
		fi
		target_arg=$1
		;;
	esac
	shift
done

shell_quote() {
	printf "'"
	printf '%s' "$1" | sed "s/'/'\\\\''/g"
	printf "'"
}

print_required_env() {
	name=$1
	hint=$2
	eval "value=\${$name:-}"
	if [ -n "$value" ]; then
		printf 'export %s=%s\n' "$name" "$(shell_quote "$value")"
	else
		printf ': "${%s:?%s}"\n' "$name" "$hint"
	fi
}

print_manual_commands() {
	cat <<'EOF'
# Run from the Slipway repository root.
# Choose one secret mode before deploying:
#   export REGISTRY_PASSWORD=<ghcr token>
#   export OP_SERVICE_ACCOUNT_TOKEN=<token>
#   export SLIPWAY_LIVE_SECRETS_FETCH='SLIPWAY_OP_ACCOUNT=<1password-account> SLIPWAY_OP_VAULT=<vault-id-or-name> SLIPWAY_OP_ITEM=<item-id-or-name> scripts/live/onepassword-fetch.sh'
EOF
	printf 'export SLIPWAY_LIVE_TARGET=%s\n' "$(shell_quote "$target")"
	printf 'export SLIPWAY_LIVE_HOST=%s\n' "$(shell_quote "$SLIPWAY_LIVE_HOST")"
	printf 'export SLIPWAY_LIVE_SSH_USER=%s\n' "$(shell_quote "$SLIPWAY_LIVE_SSH_USER")"
	printf 'export SLIPWAY_LIVE_ROUTE_HOST=%s\n' "$(shell_quote "$SLIPWAY_LIVE_ROUTE_HOST")"
	printf 'export SLIPWAY_LIVE_CONFIG=%s\n' "$(shell_quote "$SLIPWAY_LIVE_CONFIG")"
	print_required_env SLIPWAY_LIVE_IMAGE "set ghcr.io/owner/slipway-live-nginx"
	print_required_env SLIPWAY_REGISTRY_SERVER "set ghcr.io"
	print_required_env SLIPWAY_REGISTRY_USERNAME "set registry username"
	cat <<'EOF'
case "$SLIPWAY_LIVE_ROUTE_HOST" in
  http://*|https://*) SLIPWAY_LIVE_ROUTE_URL=$SLIPWAY_LIVE_ROUTE_HOST ;;
  *) SLIPWAY_LIVE_ROUTE_URL="http://$SLIPWAY_LIVE_ROUTE_HOST" ;;
esac
scripts/live/check-secrets.sh
scripts/live/check-local-docker.sh
MOCK_VERSION=v1 scripts/live/render-config.sh
go run ./cmd/slipway validate -c "$SLIPWAY_LIVE_CONFIG" --env production
go run ./cmd/slipway provision -c "$SLIPWAY_LIVE_CONFIG" --env production
SLIPWAY_GIT_SHA=111111111111 go run ./cmd/slipway deploy -c "$SLIPWAY_LIVE_CONFIG" --env production
curl -fsS "$SLIPWAY_LIVE_ROUTE_URL/"
curl -fsS "$SLIPWAY_LIVE_ROUTE_URL/healthz"
go run ./cmd/slipway status -c "$SLIPWAY_LIVE_CONFIG" --env production
MOCK_VERSION=v2 scripts/live/render-config.sh
go run ./cmd/slipway validate -c "$SLIPWAY_LIVE_CONFIG" --env production
SLIPWAY_GIT_SHA=222222222222 go run ./cmd/slipway deploy -c "$SLIPWAY_LIVE_CONFIG" --env production
curl -fsS "$SLIPWAY_LIVE_ROUTE_URL/"
go run ./cmd/slipway rollback -c "$SLIPWAY_LIVE_CONFIG" --env production
curl -fsS "$SLIPWAY_LIVE_ROUTE_URL/"
go run ./cmd/slipway status -c "$SLIPWAY_LIVE_CONFIG" --env production
EOF
}

target=${target_arg:-${SLIPWAY_LIVE_TARGET:-root@203.0.113.10}}
case "$target" in
*@*)
	target_user=${target%@*}
	target_host=${target#*@}
	;;
*)
	target_user=${SLIPWAY_LIVE_SSH_USER:-root}
	target_host=$target
	;;
esac

export SLIPWAY_LIVE_HOST=${SLIPWAY_LIVE_HOST:-$target_host}
export SLIPWAY_LIVE_SSH_USER=${SLIPWAY_LIVE_SSH_USER:-$target_user}
export SLIPWAY_LIVE_ROUTE_HOST=${SLIPWAY_LIVE_ROUTE_HOST:-$SLIPWAY_LIVE_HOST}
export SLIPWAY_LIVE_CONFIG=${SLIPWAY_LIVE_CONFIG:-.tmp/live-nginx/slipway.yml}

if [ "$print_commands" = "1" ]; then
	print_manual_commands
	exit 0
fi

missing=
require_env() {
	name=$1
	eval "value=\${$name:-}"
	if [ -z "$value" ]; then
		missing="$missing $name"
	fi
}

for name in \
	SLIPWAY_LIVE_IMAGE \
	SLIPWAY_REGISTRY_SERVER \
	SLIPWAY_REGISTRY_USERNAME
do
	require_env "$name"
done

if [ -n "$missing" ]; then
	echo "missing required environment:$missing" >&2
	usage
	exit 2
fi

route_url=$SLIPWAY_LIVE_ROUTE_HOST
case "$route_url" in
http://* | https://*) ;;
*) route_url="http://$route_url" ;;
esac

run_slipway() {
	go run ./cmd/slipway "$@"
}

render_version() {
	version=$1
	MOCK_VERSION=$version scripts/live/render-config.sh >/dev/null
}

require_body_contains() {
	expected=$1
	path=$2
	attempt=1
	while [ "$attempt" -le 20 ]; do
		body=$(curl -fsS --max-time 5 "$route_url$path" 2>/dev/null || true)
		case "$body" in
		*"$expected"*) return 0 ;;
		esac
		sleep 2
		attempt=$((attempt + 1))
	done
	echo "expected $route_url$path to contain $expected" >&2
	exit 1
}

require_healthz() {
	attempt=1
	while [ "$attempt" -le 20 ]; do
		code=$(curl -fsS -o /dev/null -w "%{http_code}" --max-time 5 "$route_url/healthz" 2>/dev/null || true)
		if [ "$code" = "200" ]; then
			return 0
		fi
		sleep 2
		attempt=$((attempt + 1))
	done
	echo "expected $route_url/healthz to return 200" >&2
	exit 1
}

scripts/live/check-secrets.sh
scripts/live/check-local-docker.sh

echo "Rendering v1 config"
render_version v1
run_slipway validate -c "$SLIPWAY_LIVE_CONFIG" --env production
run_slipway provision -c "$SLIPWAY_LIVE_CONFIG" --env production
SLIPWAY_GIT_SHA=111111111111 go run ./cmd/slipway deploy -c "$SLIPWAY_LIVE_CONFIG" --env production
require_body_contains "Slipway mock v1" /
require_healthz
run_slipway status -c "$SLIPWAY_LIVE_CONFIG" --env production

sleep 1

echo "Rendering v2 config"
render_version v2
run_slipway validate -c "$SLIPWAY_LIVE_CONFIG" --env production
SLIPWAY_GIT_SHA=222222222222 go run ./cmd/slipway deploy -c "$SLIPWAY_LIVE_CONFIG" --env production
require_body_contains "Slipway mock v2" /

run_slipway rollback -c "$SLIPWAY_LIVE_CONFIG" --env production
require_body_contains "Slipway mock v1" /
run_slipway status -c "$SLIPWAY_LIVE_CONFIG" --env production

echo "Slipway live nginx smoke test passed"
