# Live nginx Smoke Test

This optional live lab deploys a tiny nginx app through Slipway to an SSH-reachable server. It is not part of CI, and it can affect host Caddy only when you pass explicit flags to the helper scripts.

The mock app serves `/healthz` with HTTP 200 and `/` with `Slipway mock <version>`. The smoke script deploys `v1`, deploys `v2`, rolls back, and verifies that `/` returns `v1` again.

## Safety Model

- `scripts/live/prepare-server.sh` inspects and backs up by default.
- It stops and disables system Caddy only with `--stop-system-caddy`.
- It removes apt Caddy only with `--purge-system-caddy`, and that flag requires `--stop-system-caddy`.
- It never deletes host Caddy config during prepare.
- `scripts/live/restore-caddy.sh` restores `/etc/caddy` from a backup path and restarts system Caddy when the service exists.
- The lab uses `root@203.0.113.10` as a documentation-safe fallback only when you run `smoke.sh` without passing a target.

## Required Environment

```sh
export SLIPWAY_LIVE_HOST=203.0.113.10
export SLIPWAY_LIVE_SSH_USER=root
export SLIPWAY_LIVE_ROUTE_HOST=203.0.113.10
export SLIPWAY_LIVE_IMAGE=ghcr.io/<owner>/slipway-live-nginx
export SLIPWAY_REGISTRY_SERVER=ghcr.io
export SLIPWAY_REGISTRY_USERNAME=<username>
```

For the real server run, copy `slipway.live.example.yml` to local ignored `slipway.live.yml`, then fill in your private host, route, registry, and 1Password values. It can use the native 1Password provider in config, including account, vault, and item, so no wrapper env vars are needed beyond whatever `op` itself needs for authentication.

## Run

Inspect and back up the host first:

```sh
scripts/live/prepare-server.sh <user@host>
```

When you are ready for Slipway's Dockerized Caddy to bind ports 80 and 443, stop the system Caddy service explicitly:

```sh
scripts/live/prepare-server.sh <user@host> --stop-system-caddy
```

Then run the live smoke:

```sh
scripts/live/smoke.sh
```

After a successful smoke run, you can inspect recent web logs:

```sh
go run ./cmd/slipway logs -c .tmp/live-nginx/slipway.yml --env production --service web --tail 50
```

You can also inspect or run cleanup for old release env files and image tags:

```sh
go run ./cmd/slipway cleanup -c .tmp/live-nginx/slipway.yml --env production --dry-run
go run ./cmd/slipway cleanup -c .tmp/live-nginx/slipway.yml --env production
```

When running from a headless or sandboxed environment, preflight secrets and local Docker before the live deploy:

```sh
scripts/live/check-secrets.sh
scripts/live/check-local-docker.sh
```

If script-launched SSH is blocked, print the exact commands and paste them manually:

```sh
scripts/live/prepare-server.sh <user@host> --print-commands
scripts/live/prepare-server.sh <user@host> --stop-system-caddy --print-commands
scripts/live/smoke.sh --print-commands
scripts/live/restore-caddy.sh <user@host> /root/slipway-backups/<timestamp> --print-commands
```

To restore the host Caddy config, use the backup path printed by `prepare-server.sh`:

```sh
scripts/live/restore-caddy.sh <user@host> /root/slipway-backups/<timestamp>
```

`--purge-system-caddy` is destructive and should only be used when you really intend to remove the apt package:

```sh
scripts/live/prepare-server.sh <user@host> --stop-system-caddy --purge-system-caddy
```
