#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
missing live registry secret mode

Use one of:
  export REGISTRY_PASSWORD=<ghcr token>

For headless 1Password CLI runs:
  export OP_SERVICE_ACCOUNT_TOKEN=<token>
  export SLIPWAY_LIVE_SECRETS_FETCH='SLIPWAY_OP_ACCOUNT=<1password-account> SLIPWAY_OP_VAULT=<vault-id-or-name> SLIPWAY_OP_ITEM=<item-id-or-name> scripts/live/onepassword-fetch.sh'

An already-authenticated 1Password CLI session may also use SLIPWAY_LIVE_SECRETS_FETCH.
EOF
}

secret_name=${SLIPWAY_LIVE_REQUIRED_SECRET:-REGISTRY_PASSWORD}

if [ -n "${REGISTRY_PASSWORD:-}" ]; then
	echo "using REGISTRY_PASSWORD from environment"
	exit 0
fi

if [ -n "${SLIPWAY_LIVE_SECRETS_FETCH:-}" ]; then
	output=$(
		SLIPWAY_SECRET_NAMES=$secret_name sh -c "$SLIPWAY_LIVE_SECRETS_FETCH"
	) || {
		echo "secret fetch command failed; verify authentication or export REGISTRY_PASSWORD directly" >&2
		exit 1
	}

	case "$output" in
	"$secret_name="* | *"
$secret_name="*)
		echo "using SLIPWAY_LIVE_SECRETS_FETCH"
		exit 0
		;;
	esac

	echo "secret fetch command did not return $secret_name" >&2
	exit 1
fi

usage
exit 2
