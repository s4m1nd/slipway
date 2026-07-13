.PHONY: test fmt fmt-check validate-example build check live-render live-prepare live-smoke live-restore

test:
	go test ./...

fmt:
	gofmt -w cmd internal

fmt-check:
	@files="$$(gofmt -l cmd internal)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

validate-example:
	go run ./cmd/slipway validate -c examples/slipway.yml --env production
	go run ./cmd/slipway validate -c slipway.example.yml --env production
	go run ./cmd/slipway validate -c slipway.live.example.yml --env production

build:
	go build -o bin/slipway ./cmd/slipway

check: fmt-check
	go test ./...
	@for script in scripts/live/*.sh scripts/install.sh scripts/release/build.sh; do sh -n "$$script"; done
	go run ./cmd/slipway version
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
	go run ./cmd/slipway accessory apply -c slipway.example.yml --env production --dry-run
	go run ./cmd/slipway accessory status -c slipway.example.yml --env production --dry-run
	go run ./cmd/slipway accessory logs -c slipway.example.yml --env production --name redis --dry-run
	go run ./cmd/slipway accessory restart -c slipway.example.yml --env production --name redis --dry-run
	go run ./cmd/slipway accessory exec -c slipway.example.yml --env production --name redis --dry-run -- redis-cli PING
	scripts/install.sh --dry-run

live-render:
	@test -n "$$SLIPWAY_LIVE_HOST" || { echo "set SLIPWAY_LIVE_HOST"; exit 2; }
	@test -n "$$SLIPWAY_LIVE_SSH_USER" || { echo "set SLIPWAY_LIVE_SSH_USER"; exit 2; }
	@test -n "$$SLIPWAY_LIVE_ROUTE_HOST" || { echo "set SLIPWAY_LIVE_ROUTE_HOST"; exit 2; }
	@test -n "$$SLIPWAY_LIVE_IMAGE" || { echo "set SLIPWAY_LIVE_IMAGE"; exit 2; }
	@test -n "$$SLIPWAY_REGISTRY_SERVER" || { echo "set SLIPWAY_REGISTRY_SERVER"; exit 2; }
	@test -n "$$SLIPWAY_REGISTRY_USERNAME" || { echo "set SLIPWAY_REGISTRY_USERNAME"; exit 2; }
	scripts/live/render-config.sh

live-prepare:
	@test -n "$$SLIPWAY_LIVE_TARGET" || { echo "set SLIPWAY_LIVE_TARGET, for example root@203.0.113.10"; exit 2; }
	scripts/live/prepare-server.sh "$$SLIPWAY_LIVE_TARGET" $${SLIPWAY_LIVE_PREPARE_FLAGS:-}

live-smoke:
	@test -n "$$SLIPWAY_LIVE_TARGET" || { echo "set SLIPWAY_LIVE_TARGET, for example root@203.0.113.10"; exit 2; }
	@test -n "$$SLIPWAY_LIVE_IMAGE" || { echo "set SLIPWAY_LIVE_IMAGE"; exit 2; }
	@test -n "$$SLIPWAY_REGISTRY_SERVER" || { echo "set SLIPWAY_REGISTRY_SERVER"; exit 2; }
	@test -n "$$SLIPWAY_REGISTRY_USERNAME" || { echo "set SLIPWAY_REGISTRY_USERNAME"; exit 2; }
	scripts/live/smoke.sh "$$SLIPWAY_LIVE_TARGET"

live-restore:
	@test -n "$$SLIPWAY_LIVE_TARGET" || { echo "set SLIPWAY_LIVE_TARGET, for example root@203.0.113.10"; exit 2; }
	@test -n "$$SLIPWAY_LIVE_BACKUP" || { echo "set SLIPWAY_LIVE_BACKUP, for example /root/slipway-backups/20260701T120000Z"; exit 2; }
	scripts/live/restore-caddy.sh "$$SLIPWAY_LIVE_TARGET" "$$SLIPWAY_LIVE_BACKUP"
