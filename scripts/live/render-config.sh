#!/bin/sh
set -eu

usage() {
	cat >&2 <<'EOF'
usage: scripts/live/render-config.sh

Required environment:
  SLIPWAY_LIVE_HOST
  SLIPWAY_LIVE_SSH_USER
  SLIPWAY_LIVE_ROUTE_HOST
  SLIPWAY_LIVE_IMAGE
  SLIPWAY_REGISTRY_SERVER
  SLIPWAY_REGISTRY_USERNAME

Optional environment:
  MOCK_VERSION=unknown
  SLIPWAY_LIVE_PLATFORM=linux/amd64
  SLIPWAY_LIVE_SSH_PORT=22
  SLIPWAY_LIVE_SECRETS_FETCH=
  SLIPWAY_LIVE_CONFIG=.tmp/live-nginx/slipway.yml
EOF
}

missing=
require_env() {
	name=$1
	eval "value=\${$name:-}"
	if [ -z "$value" ]; then
		missing="$missing $name"
	fi
}

for name in \
	SLIPWAY_LIVE_HOST \
	SLIPWAY_LIVE_SSH_USER \
	SLIPWAY_LIVE_ROUTE_HOST \
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

reject_multiline() {
	name=$1
	eval "value=\${$name:-}"
	case "$value" in
	*'
'*)
		echo "$name must be a single-line value" >&2
		exit 2
		;;
	esac
}

for name in \
	SLIPWAY_LIVE_HOST \
	SLIPWAY_LIVE_SSH_USER \
	SLIPWAY_LIVE_ROUTE_HOST \
	SLIPWAY_LIVE_IMAGE \
	SLIPWAY_REGISTRY_SERVER \
	SLIPWAY_REGISTRY_USERNAME \
	MOCK_VERSION \
	SLIPWAY_LIVE_PLATFORM \
	SLIPWAY_LIVE_SSH_PORT \
	SLIPWAY_LIVE_SECRETS_FETCH
do
	reject_multiline "$name"
done

yaml_single_quote() {
	printf "'"
	printf '%s' "$1" | sed "s/'/''/g"
	printf "'"
}

route_host=$SLIPWAY_LIVE_ROUTE_HOST
case "$route_host" in
http://* | https://*) ;;
*) route_host="http://$route_host" ;;
esac

mock_version=${MOCK_VERSION:-unknown}
platform=${SLIPWAY_LIVE_PLATFORM:-linux/amd64}
ssh_port=${SLIPWAY_LIVE_SSH_PORT:-22}
secrets_fetch=${SLIPWAY_LIVE_SECRETS_FETCH:-}
config_path=${SLIPWAY_LIVE_CONFIG:-.tmp/live-nginx/slipway.yml}
config_dir=$(dirname "$config_path")
mkdir -p "$config_dir"

secrets_fetch_line=
if [ -n "$secrets_fetch" ]; then
	secrets_fetch_line="  fetch: $(yaml_single_quote "$secrets_fetch")
"
fi

cat > "$config_path" <<EOF
project: slipway_live

retention:
  releases: 5

registry:
  server: $SLIPWAY_REGISTRY_SERVER
  username: $SLIPWAY_REGISTRY_USERNAME
  password:
    - REGISTRY_PASSWORD

secrets:
${secrets_fetch_line}  names:
    - REGISTRY_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: $SLIPWAY_LIVE_HOST
        ssh_user: $SLIPWAY_LIVE_SSH_USER
        host_ssh_port: $ssh_port
    proxy:
      listen_http: :80
      listen_https: :443
      routes:
        - host: $route_host
          service: web
          tls: false
    services:
      web:
        image: $SLIPWAY_LIVE_IMAGE
        build:
          context: examples/live-nginx
          dockerfile: Dockerfile
          platform: $platform
          args:
            - MOCK_VERSION=$mock_version
        hosts: [app-1]
        internal_port: 80
        health_check:
          path: /healthz
          interval: 5s
          timeout: 2s
          retries: 12
EOF

printf '%s\n' "$config_path"
