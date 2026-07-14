# Slipway

Slipway is a blue/green-first deployment CLI for Dockerized apps. It deploys from one `slipway.yml` to SSH-reachable servers, using Docker for the app runtime and Caddy for HTTP routing.

CI systems such as GitHub Actions are only runners. The same command should work from a developer machine when it has the config file, SSH access, registry access, and secret-provider access.

## Commands

```sh
slipway init [-c slipway.yml] [--force]
slipway validate -c slipway.yml [--env production]
slipway provision -c slipway.yml --env production [--dry-run] [--verbose]
slipway deploy -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
slipway rollback -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
slipway status -c slipway.yml --env production [--dry-run]
slipway sync-proxy -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
slipway cleanup -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
slipway logs -c slipway.yml --env production --service web [--host app-1] [--color active] [--tail 100] [--follow] [--dry-run]
slipway accessory apply -c slipway.yml --env production [--name <accessory>] [--dry-run] [--verbose] [--lock-timeout 30m]
slipway accessory status -c slipway.yml --env production [--name <accessory>] [--dry-run]
slipway accessory logs -c slipway.yml --env production --name <accessory> [--tail 100] [--follow] [--dry-run]
slipway accessory restart -c slipway.yml --env production --name <accessory> [--dry-run] [--verbose] [--lock-timeout 30m]
slipway accessory exec -c slipway.yml --env production --name <accessory> [--dry-run] -- command [args...]
slipway secrets exec -c slipway.yml --secret NAME [--secret NAME ...] [--dry-run] -- command [args...]
slipway version
```

Commands execute by default. Normal execution prints concise progress without dumping generated shell. Use `--verbose` on supported mutating commands to include non-sensitive generated shell, or `--dry-run` to print the detailed command plan without running Docker or SSH commands. Sensitive commands are always redacted.

Subprocess output is indented beneath its step. Routine successful login, image-pull, and Caddy messages are compacted, while warnings remain visible; failures replay the captured details. Successful deploys and rollbacks finish with the active color, release, and total elapsed time.

Color is enabled automatically for terminal output and disabled for redirected output, `TERM=dumb`, or when `NO_COLOR` is present. Set `SLIPWAY_COLOR=always`, `SLIPWAY_COLOR=never`, or `SLIPWAY_COLOR=auto` to override or restore automatic detection.

## Install

Install the current alpha release:

```sh
curl -fsSL https://raw.githubusercontent.com/s4m1nd/slipway/main/scripts/install.sh | SLIPWAY_VERSION=v0.1.0-alpha.1 bash
```

After the first stable release, the unpinned latest-release installer will work:

```sh
curl -fsSL https://raw.githubusercontent.com/s4m1nd/slipway/main/scripts/install.sh | bash
```

The installer downloads the matching `darwin` or `linux` binary for `amd64` or `arm64`, verifies it with `checksums.txt`, and installs it to `/usr/local/bin/slipway` by default. Use `--bin-dir` to choose another directory.

```sh
scripts/install.sh --help
scripts/install.sh --dry-run
```

## Requirements

Local machine:

- Slipway installed, or Go installed when running `go run ./cmd/slipway`.
- Docker installed locally for `deploy`, because Slipway builds and pushes
  service images before it connects to hosts.
- A registry account and registry credentials for the configured image
  repository.
- SSH access to each target server.
- Terraform only when using the optional
  `examples/terraform/hetzner-single-node` host example.

Target servers:

- Docker available to the configured SSH user without an interactive sudo
  prompt.
- The configured SSH user can write under `/opt/slipway`.
- Ports 80 and 443 are available when the environment uses Slipway-managed
  Caddy proxy routes.

Secrets:

- By default, Slipway reads secret names from the local environment, such as
  `REGISTRY_PASSWORD`.
- `secrets.fetch` can call any command that prints `KEY=VALUE` lines.
- Built-in secret providers are `1password` and `doppler`. Other providers can
  still be used through `secrets.fetch`.
- The matching provider CLI is required for a native adapter: `op` for
  1Password or `doppler` for Doppler.

Useful starting points:

- `slipway.example.yml` is the compact config example.
- `slipway.live.example.yml` is a local, ignored real-server template.
- `examples/terraform/hetzner-single-node` creates a single Hetzner host.
- `examples/live-nginx` is the optional real-host smoke test.

## Deployment Flow

1. Load the config with strict YAML field validation.
2. Resolve named secrets at deploy time from the local environment or an optional fetch command.
3. Acquire the deploy lock and verify declared accessory dependencies exist, are running, and are healthy.
4. Build and push each service image with a release tag.
5. SSH to the configured hosts.
6. Upload restrictive env files under `/opt/slipway/<project>/<env>/secrets`.
7. Start the inactive color beside the active container.
8. Run configured HTTP health checks for every service that defines one.
9. Reload Caddy once per proxy host, only after health checks pass, with host-local routes.
10. Record the active color and release under `/opt/slipway/<project>/<env>/state`.
11. For services not referenced by `proxy.routes`, stop the previous color after state is recorded. The previous container is kept for rollback, but is not left running.
12. Clean up old release env files and image tags after successful state recording.

Resolved secret values are passed through stdin and are not printed in plans, logs, errors, or tests.

## Remote State And Status

Each deployed service records state on the target host at:

```text
/opt/slipway/<project>/<env>/state/<service>.json
```

The state file preserves the current release metadata and the previous release metadata when available. `slipway status` reads these files and inspects blue/green Docker containers over SSH; it does not resolve secrets or require secret-provider access.

## Proxy Sync

`slipway sync-proxy -c slipway.yml --env production` reapplies the configured `proxy.routes` to Caddy without building images, pushing images, uploading env files, starting containers, running health checks, mutating release state, cleaning up artifacts, logging in to the registry, or resolving secrets. It reads the active color from each routed service state file and reloads Caddy once per proxy host.

Use it when route-only config changes need to go live through Slipway without a full deploy:

```sh
slipway sync-proxy -c slipway.yml --env production --dry-run
slipway sync-proxy -c slipway.yml --env production
```

## Retention And Cleanup

`retention.releases` controls how many release IDs Slipway keeps per service on each host. The default is `5` when omitted. Explicit values must be at least `2`, and an environment can override the top-level setting:

```yaml
retention:
  releases: 5

environments:
  production:
    retention:
      releases: 3
```

The active release and immediate previous rollback release are always kept, even when they exceed the numeric retention limit. Cleanup removes old service env files under `/opt/slipway/<project>/<env>/secrets` and old Docker image tags for the configured service image repository. It does not resolve secrets, log in to the registry, build, push, upload env files, switch Caddy, mutate state, stream logs, or delete the active or previous release artifacts. Cleanup never targets accessory containers, env files, or volumes.

Deploy runs cleanup after successful state recording and after stopping previous containers for non-routed services. Cleanup can also be inspected or run directly:

```sh
slipway cleanup -c slipway.yml --env production --dry-run
slipway cleanup -c slipway.yml --env production
```

## Logs

`slipway logs -c slipway.yml --env production --service web` streams Docker logs for a deployed service over SSH. By default it reads the active color from the service state file and tails the last 100 lines.

Options:

- `--service <name>` selects the configured service and is required.
- `--host <server-name>` restricts logs to one configured server key, such as `app-1`.
- `--color active|previous|blue|green` selects which blue/green container to read. `active` is the default.
- `--tail <n>` sets the Docker log tail count. The default is `100`.
- `--follow` passes `-f` to `docker logs`. When a service runs on multiple hosts, combine `--follow` with `--host`.
- `--dry-run` prints the generated SSH/Docker command plan without connecting to hosts.

`logs` does not build images, push images, upload env files, switch Caddy, mutate state, log in to the registry, or resolve secrets, so it does not need registry or secret-provider access. Slipway does not add secret values to the generated log command or dry-run output. Application logs are produced by the application itself, so operators should still treat log output as operational data that may contain app-provided sensitive values.

## Accessories

Accessories are stable persistent containers managed separately from blue/green application services. Redis and PostgreSQL are supported presets. PostgreSQL images must have a tag beginning with an explicit major version, such as `postgres:17-alpine`, so Slipway can guard major upgrades.

```yaml
secrets:
  names:
    - POSTGRES_PASSWORD
    - REDIS_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: deploy
        host_ssh_port: 22
    accessories:
      postgres:
        type: postgres
        image: postgres:17-alpine
        host: app-1
        env:
          POSTGRES_DB: demo
          POSTGRES_USER: demo
        secrets:
          - POSTGRES_PASSWORD
        storage:
          volume: demo-postgres-data
      redis:
        type: redis
        image: redis:7-alpine
        host: app-1
        secrets:
          - REDIS_PASSWORD
        storage:
          volume: demo-redis-data
    services:
      web:
        # existing image/build fields
        hosts: [app-1]
        depends_on: [postgres, redis]
        env:
          POSTGRES_HOST: postgres
          POSTGRES_PORT: "5432"
          POSTGRES_DB: demo
          POSTGRES_USER: demo
          REDIS_HOST: redis
          REDIS_PORT: "6379"
        secrets:
          - POSTGRES_PASSWORD
          - REDIS_PASSWORD
```

The accessory name becomes its stable Docker network alias, so `postgres` and `redis` are reachable only from services on the same configured host and project network. Slipway does not publish accessory ports. Validation rejects unpinned or `latest` images, undefined secrets, missing named volumes, unknown hosts, and dependencies that cross host-local Docker networks.

After provisioning the target hosts, apply accessories explicitly before the first application deploy:

```sh
slipway accessory apply -c slipway.yml --env production --dry-run
slipway accessory apply -c slipway.yml --env production
```

`accessory apply` resolves only the selected accessories' secrets, acquires the same host lock used by deploys, uploads a mode-`0600` env file, creates the project network and named volume when absent, and creates or explicitly reconciles one stable container. Redis enables append-only persistence and password authentication. PostgreSQL uses its official image entrypoint, password authentication, and a persistent data volume. Reapplying unchanged configuration is idempotent. A changed image, environment, or secret recreates the container after the image pull, while preserving the named volume. Changing the configured volume for an existing container is refused. `accessory restart` uses the same lock so it cannot race an application deploy.

For PostgreSQL, changing the configured image to another major version is refused before Slipway pulls an image or replaces the container. Perform a documented database migration separately, then reconcile the container deliberately. A same-major image change is allowed with a warning and preserves the volume.

Before an upgrade, take a logical backup and prove that it restores into a disposable database rather than production:

```sh
mkdir -p backups
slipway accessory exec -c slipway.yml --env production --name postgres -- \
  sh -lc 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' \
  > backups/postgres.dump

slipway accessory exec -c slipway.yml --env production --name postgres -- \
  sh -lc 'dropdb -U "$POSTGRES_USER" --if-exists slipway_restore_drill && createdb -U "$POSTGRES_USER" slipway_restore_drill'
slipway accessory exec -c slipway.yml --env production --name postgres -- \
  sh -lc 'pg_restore -U "$POSTGRES_USER" -d slipway_restore_drill --exit-on-error' \
  < backups/postgres.dump
slipway accessory exec -c slipway.yml --env production --name postgres -- \
  sh -lc 'psql -U "$POSTGRES_USER" -d slipway_restore_drill -c "SELECT current_database();"'
slipway accessory exec -c slipway.yml --env production --name postgres -- \
  sh -lc 'dropdb -U "$POSTGRES_USER" slipway_restore_drill'
```

Application deploys never pull, recreate, restart, or update accessories. A service's `depends_on` entries are checked after the deploy lock is acquired and before application images are built. The deploy stops if a dependency is absent, stopped, or unhealthy.

Operational commands do not resolve secrets:

```sh
slipway accessory status -c slipway.yml --env production
slipway accessory logs -c slipway.yml --env production --name redis --tail 100
slipway accessory restart -c slipway.yml --env production --name redis
slipway accessory exec -c slipway.yml --env production --name redis -- \
  sh -lc 'redis-cli -a "$REDIS_PASSWORD" --no-auth-warning PING'
```

There is deliberately no accessory `destroy` command. Slipway cleanup has no accessory or volume deletion path.

## Rollback

`slipway rollback -c slipway.yml --env production` rolls the whole environment back to the previous release recorded in each service state file. It inspects every configured service/host first and fails before changing anything if any target is missing previous release metadata or the previous color container.

Rollback starts previous containers, runs configured health checks, switches Caddy once per proxy host for routed services, swaps the active and previous metadata, and then stops the rolled-back-from color for non-routed services. It does not build images, push images, upload env files, log in to the registry, or resolve secrets, so it does not require registry or secret-provider access.

## Config

See [`slipway.example.yml`](./slipway.example.yml) for a full example.

```yaml
project: demo

retention:
  releases: 5

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
    - REDIS_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: root
        host_ssh_port: 22
    proxy:
      listen_http: :80
      listen_https: :443
      routes:
        - host: app.example.com
          service: web
          tls: true
    accessories:
      redis:
        type: redis
        image: redis:7-alpine
        host: app-1
        secrets:
          - REDIS_PASSWORD
        storage:
          volume: demo-redis-data
    services:
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
          dockerfile: Dockerfile
          platform: linux/amd64
        hosts: [app-1]
        depends_on: [redis]
        internal_port: 3000
        health_check:
          path: /healthz
        env:
          RACK_ENV: production
          REDIS_HOST: redis
          REDIS_PORT: "6379"
        secrets:
          - DATABASE_URL
          - REDIS_PASSWORD
```

### Required Fields

Top level:

- `project`
- `registry.server`
- `registry.username`
- `registry.password_secret`, or `registry.password` with exactly one secret name
- `secrets.names`
- `environments`

Environment:

- `servers`
- `services`
- `proxy.routes` when HTTP traffic should be routed through Caddy
- `accessories` when stable persistent containers are needed

Server:

- `host`
- `ssh_user`
- `host_ssh_port`

Service:

- `image`
- `build.context`
- `hosts`
- `internal_port` and `health_check.path` for services referenced by `proxy.routes`
- `depends_on` when the service requires declared healthy accessories before deploy

Accessory:

- `type` (`postgres` or `redis`)
- an explicitly tagged or digest-pinned `image`; `latest` is rejected. PostgreSQL tags must begin with the major version
- `host`
- `storage.volume`
- `REDIS_PASSWORD` in `secrets` for Redis
- `POSTGRES_DB` and `POSTGRES_USER` in `env`, plus `POSTGRES_PASSWORD` in `secrets`, for PostgreSQL

Set `build.platform` when the build machine architecture differs from the target server, for example `linux/amd64` when building on Apple Silicon for an x86_64 Ubuntu host.

## Secrets

`slipway.yml` stores secret names, not values. By default, Slipway reads each name from the local environment when `deploy` runs.

```yaml
secrets:
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
```

Set `secrets.fetch` to use a command-based provider instead. The command prints `KEY=VALUE` lines and receives `SLIPWAY_SECRET_NAMES` as a comma-separated list.

```yaml
secrets:
  fetch: op run --env-file=.env.production -- printenv
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
```

Every service or accessory secret and the registry password secret must be listed in `secrets.names`. `registry.password` is accepted as an additive form for configs that prefer a list of password secret names:

```yaml
registry:
  server: ghcr.io
  username: <registry-username>
  password:
    - REGISTRY_PASSWORD
```

For 1Password-backed deployments, configure the native provider:

```yaml
secrets:
  provider:
    type: 1password
    account: <1password-account>
    vault: <vault-id-or-name>
    item: <item-id-or-name>
  names:
    - REGISTRY_PASSWORD
```

For headless automation, set `OP_SERVICE_ACCOUNT_TOKEN` in the runner environment so `op read` can authenticate without the desktop app. For simple local runs without a configured provider, exporting `REGISTRY_PASSWORD=<ghcr token>` is enough.

For Doppler-backed deployments, select the Doppler project and config in the
same file:

```yaml
secrets:
  provider:
    type: doppler
    project: backend
    config: prd
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
```

Authenticate the Doppler CLI locally or set `DOPPLER_TOKEN` in automation. See
[`docs/doppler.md`](./docs/doppler.md) for setup, bootstrap-token guidance, and
Terraform examples.

`slipway secrets exec` resolves selected names from the same `secrets` provider
and injects them into a child command environment:

```sh
slipway secrets exec -c slipway.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node plan
```

This does not mutate your current shell environment, and `--dry-run` prints a
redacted command plan without resolving or printing secret values. Deploy only
resolves the registry password secret plus service secrets used by the selected
environment. `slipway accessory apply` separately resolves only the selected
accessories' secrets. Extra names in `secrets.names` can therefore be used by
other commands without being fetched on every deploy or accessory operation.

## Architecture

```text
cmd/slipway
  internal/cli        CLI parsing and command execution
  internal/accessory  Stable persistent Docker accessory lifecycle
  internal/console    TTY-aware styling and structured progress output
  internal/config     YAML schema, defaults, strict loading, validation
  internal/planner    Blue/green orchestration order
  internal/runtime    Docker runtime command generation
  internal/proxy      Caddy proxy command generation
  internal/remote     Command plans and execution output
  internal/secrets    Env/command secret resolution
  internal/ssh        System ssh runner
  internal/state      Remote status parsing and reporting
```

Blue/green Docker command generation stays behind `internal/runtime.Runtime`. Persistent accessory commands stay behind the focused `internal/accessory.Manager`; they are not added to the application runtime interface. Caddy-specific command generation stays behind `internal/proxy.Manager`.

## Development

```sh
make fmt
make check
```

`make check` enforces formatting, runs tests, parses every shell script, and exercises the important dry-run command paths:

```sh
go run ./cmd/slipway validate -c examples/slipway.yml --env production
go run ./cmd/slipway validate -c slipway.example.yml --env production
go run ./cmd/slipway validate -c slipway.live.example.yml --env production
go run ./cmd/slipway provision -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway deploy -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway status -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway rollback -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway sync-proxy -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway cleanup -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway logs -c slipway.example.yml --env production --service web --dry-run
go run ./cmd/slipway logs -c slipway.example.yml --env production --service web --color previous --tail 50 --dry-run
go run ./cmd/slipway accessory apply -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway accessory status -c slipway.example.yml --env production --dry-run
go run ./cmd/slipway accessory logs -c slipway.example.yml --env production --name redis --dry-run
go run ./cmd/slipway accessory restart -c slipway.example.yml --env production --name redis --dry-run
go run ./cmd/slipway accessory exec -c slipway.example.yml --env production --name redis --dry-run -- redis-cli PING
```

Release steps live in [`docs/releasing.md`](./docs/releasing.md).

## Infrastructure Examples

[`examples/terraform/hetzner-single-node`](./examples/terraform/hetzner-single-node)
creates one Hetzner Cloud Ubuntu host with Docker, automatic security updates,
`fail2ban`, and a provider firewall that opens SSH only to admin CIDRs plus
public HTTP/HTTPS for Slipway-managed Caddy. It outputs a ready-to-copy
`servers` block for `slipway.yml`.

Terraform examples are not part of CI. Keep provider tokens in `HCLOUD_TOKEN`
and keep registry credentials, app secrets, and 1Password metadata out of
Terraform variables, cloud-init, plans, and state.

Use `secrets exec` when `HCLOUD_TOKEN` lives in your configured secret provider:

```sh
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node init
slipway secrets exec -c slipway.live.yml --secret HCLOUD_TOKEN -- \
  terraform -chdir=examples/terraform/hetzner-single-node apply
```

## Alpha Notes

Provision starts an existing Slipway Caddy container when it is present, but it
does not yet reconcile changed Caddy ports, network, volume mounts, or image
version. Changing `proxy.listen_http` or `proxy.listen_https` may require
operator cleanup of the existing Caddy container before reprovisioning.

There is also a known deploy transaction gap: if Caddy reloads successfully but
the active state write fails, traffic and Slipway state can temporarily
disagree. Treat this as an alpha risk until a recovery command or atomic
proxy/state update lands.

## Optional Live Lab

[`examples/live-nginx`](./examples/live-nginx) contains a guarded, non-CI smoke test that deploys a tiny nginx app to a real SSH host, verifies deploy/status/rollback, and documents how to back up or restore host Caddy. It is inspect-and-backup by default; stopping system Caddy requires `--stop-system-caddy`, and purging apt Caddy additionally requires `--purge-system-caddy`.

The generated nginx smoke helpers render a temporary config under `.tmp/` and expect either `REGISTRY_PASSWORD` or `SLIPWAY_LIVE_SECRETS_FETCH`. For a local real-server config, copy [`slipway.live.example.yml`](./slipway.live.example.yml) to ignored `slipway.live.yml` and fill in private host, route, registry, and 1Password values. See [`docs/live-lab.md`](./docs/live-lab.md) for the full live-lab flow.

The helper targets are intentionally explicit:

```sh
make live-render
make live-prepare SLIPWAY_LIVE_TARGET=<user@host>
make live-smoke SLIPWAY_LIVE_TARGET=<user@host>
make live-restore SLIPWAY_LIVE_TARGET=<user@host> SLIPWAY_LIVE_BACKUP=/root/slipway-backups/<timestamp>
```

If a sandbox allows direct top-level SSH but blocks script-launched SSH, print the exact commands and paste them manually:

```sh
scripts/live/prepare-server.sh <user@host> --print-commands
scripts/live/prepare-server.sh <user@host> --stop-system-caddy --print-commands
scripts/live/smoke.sh --print-commands
scripts/live/restore-caddy.sh <user@host> /root/slipway-backups/<timestamp> --print-commands
```

## License

Apache License 2.0. See [LICENSE](./LICENSE).
