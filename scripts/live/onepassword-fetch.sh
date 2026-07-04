#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/onepassword-fetch.sh

Required environment:
  SLIPWAY_SECRET_NAMES
  SLIPWAY_OP_ACCOUNT
  SLIPWAY_OP_VAULT
  SLIPWAY_OP_ITEM

Optional environment:
  SLIPWAY_OP_FIELD_PREFIX

The script prints KEY=VALUE lines for Slipway's command secret provider.
EOF
}

command -v op >/dev/null 2>&1 || {
	echo "1Password CLI 'op' is required" >&2
	exit 2
}

missing=
require_env() {
	name=$1
	eval "value=\${$name:-}"
	if [ -z "$value" ]; then
		missing="$missing $name"
	fi
}

for name in SLIPWAY_SECRET_NAMES SLIPWAY_OP_ACCOUNT SLIPWAY_OP_VAULT SLIPWAY_OP_ITEM; do
	require_env "$name"
done

if [ -n "$missing" ]; then
	echo "missing required environment:$missing" >&2
	usage
	exit 2
fi

field_prefix=${SLIPWAY_OP_FIELD_PREFIX:-}
case "$field_prefix" in
*'
'* | */*)
	echo "SLIPWAY_OP_FIELD_PREFIX must not contain newlines or slash characters" >&2
	exit 2
	;;
esac

if [ -z "${OP_SERVICE_ACCOUNT_TOKEN:-}" ]; then
	if ! auth_output=$(op whoami --account "$SLIPWAY_OP_ACCOUNT" 2>&1); then
		case "$auth_output" in
		*"desktop app"* | *"Desktop app"* | *"1Password desktop app"*)
			echo "1Password CLI is not authenticated. For headless runs, set OP_SERVICE_ACCOUNT_TOKEN, or export REGISTRY_PASSWORD directly." >&2
			;;
		*)
			echo "1Password CLI is not authenticated. For headless runs, set OP_SERVICE_ACCOUNT_TOKEN, or export REGISTRY_PASSWORD directly." >&2
			;;
		esac
		exit 2
	fi
fi

old_ifs=$IFS
IFS=,
set -- $SLIPWAY_SECRET_NAMES
IFS=$old_ifs

if [ $# -eq 0 ]; then
	echo "SLIPWAY_SECRET_NAMES must contain at least one name" >&2
	exit 2
fi

for name in "$@"; do
	case "$name" in
	[A-Z_]*)
		case "$name" in
		*[!A-Z0-9_]*)
			echo "invalid secret name requested" >&2
			exit 2
			;;
		esac
		;;
	*)
		echo "invalid secret name requested" >&2
		exit 2
		;;
	esac

	field=$field_prefix$name
	if ! value=$(op read "op://$SLIPWAY_OP_VAULT/$SLIPWAY_OP_ITEM/$field" --account "$SLIPWAY_OP_ACCOUNT"); then
		echo "failed to read 1Password field for $name" >&2
		exit 1
	fi
	case "$value" in
	*'
'*)
		echo "secret value contains unsupported newline" >&2
		exit 1
		;;
	esac
	printf '%s=%s\n' "$name" "$value"
done
