# Live Lab

The optional live lab deploys `examples/live-nginx` to an SSH-reachable server.
It is useful for checking deploy, status, logs, rollback, proxy sync, cleanup,
and reboot behavior against a real Docker host. It is not part of CI.

Do not paste secret values into tracked files or shared command transcripts.
Keep real host, route, registry, and 1Password metadata in a local ignored
`slipway.live.yml`.

## Safe Config Template

Start from the public template:

```sh
cp slipway.live.example.yml slipway.live.yml
```

Then replace the placeholders in `slipway.live.yml` with private values. The
local file is ignored by git.

## Generated Smoke Config

The helper flow can also render a temporary config under `.tmp/live-nginx/`:

```sh
export SLIPWAY_LIVE_HOST=203.0.113.10
export SLIPWAY_LIVE_SSH_USER=root
export SLIPWAY_LIVE_ROUTE_HOST=203.0.113.10
export SLIPWAY_LIVE_IMAGE=ghcr.io/<owner>/slipway-live-nginx
export SLIPWAY_REGISTRY_SERVER=ghcr.io
export SLIPWAY_REGISTRY_USERNAME=<username>

scripts/live/render-config.sh
```

The generated `.tmp/` files are ignored.

## Inspect And Prepare

Back up and inspect the host first:

```sh
scripts/live/prepare-server.sh <user@host>
```

When you are ready for Slipway's Dockerized Caddy to bind ports 80 and 443,
stop the system Caddy service explicitly:

```sh
scripts/live/prepare-server.sh <user@host> --stop-system-caddy
```

`--purge-system-caddy` is destructive and requires `--stop-system-caddy`.

## Run The Smoke

```sh
scripts/live/check-secrets.sh
scripts/live/check-local-docker.sh
scripts/live/smoke.sh <user@host>
```

If script-launched SSH is blocked, print the exact commands and paste them into
a trusted terminal:

```sh
scripts/live/prepare-server.sh <user@host> --print-commands
scripts/live/prepare-server.sh <user@host> --stop-system-caddy --print-commands
scripts/live/smoke.sh <user@host> --print-commands
scripts/live/restore-caddy.sh <user@host> /root/slipway-backups/<timestamp> --print-commands
```

## Real-Run Sequence

For a local `slipway.live.yml`, use a temp copy so you can change the mock app
version during the test:

```sh
CFG=.tmp/live-nginx/real-run.yml
mkdir -p .tmp/live-nginx
cp slipway.live.yml "$CFG"

set_mock_version() {
  perl -0pi -e "s/MOCK_VERSION=[A-Za-z0-9._-]+/MOCK_VERSION=$1/g" "$CFG"
}

set_mock_version v1
go run ./cmd/slipway validate -c "$CFG" --env production
SLIPWAY_GIT_SHA=111111111111 go run ./cmd/slipway deploy -c "$CFG" --env production

set_mock_version v2
SLIPWAY_GIT_SHA=222222222222 go run ./cmd/slipway deploy -c "$CFG" --env production

go run ./cmd/slipway status -c "$CFG" --env production
go run ./cmd/slipway logs -c "$CFG" --env production --service web --tail 50
go run ./cmd/slipway rollback -c "$CFG" --env production
go run ./cmd/slipway sync-proxy -c "$CFG" --env production
go run ./cmd/slipway cleanup -c "$CFG" --env production
```

During deploys, the proxy switch should appear only after the health check
succeeds. Sensitive commands should stay redacted. After rollback, the route
should serve the previous mock version again.
