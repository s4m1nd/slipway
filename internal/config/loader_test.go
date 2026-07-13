package config

import (
	"fmt"
	"strings"
	"testing"
)

const validConfigYAML = `project: demo_app

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
  fetch: op run --env-file=.env.production -- printenv
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
    - REDIS_URL

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: root
        host_ssh_port: 2201
      worker-1:
        host: 203.0.113.11
        ssh_user: root
    proxy:
      routes:
        - host: app.example.com
          service: web
          tls: true
    services:
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
          dockerfile: Dockerfile
        hosts: [app-1]
        internal_port: 3000
        health_check:
          path: /healthz
        env:
          RACK_ENV: production
        secrets:
          - DATABASE_URL
          - REDIS_URL
      worker:
        image: ghcr.io/example/demo-worker
        build:
          context: .
          dockerfile: Dockerfile.worker
        hosts: [worker-1]
        secrets:
          - DATABASE_URL
`

func TestLoadExampleConfigs(t *testing.T) {
	for _, path := range []string{"../../slipway.example.yml", "../../examples/slipway.yml"} {
		t.Run(path, func(t *testing.T) {
			cfg, err := LoadFile(path)
			if err != nil {
				t.Fatalf("LoadFile returned error: %v", err)
			}
			if err := ValidateEnv(cfg, "production"); err != nil {
				t.Fatalf("ValidateEnv returned error: %v", err)
			}
		})
	}
}

func TestLoadBytesAppliesDefaults(t *testing.T) {
	cfg, err := LoadBytes([]byte(validConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}

	if cfg.Retention.Releases != 5 {
		t.Fatalf("top-level retention.releases = %d", cfg.Retention.Releases)
	}

	if cfg.Defaults.Root != "/opt/slipway" {
		t.Fatalf("internal root default = %q", cfg.Defaults.Root)
	}

	prod := cfg.Environments["production"]
	if prod.Retention.Releases != 5 {
		t.Fatalf("environment retention.releases = %d", prod.Retention.Releases)
	}
	if prod.Proxy.ListenHTTP != ":80" {
		t.Fatalf("proxy.listen_http = %q", prod.Proxy.ListenHTTP)
	}
	if prod.Proxy.ListenHTTPS != ":443" {
		t.Fatalf("proxy.listen_https = %q", prod.Proxy.ListenHTTPS)
	}

	web := prod.Services["web"]
	if web.HealthCheck.Interval != "5s" {
		t.Fatalf("health_check.interval = %q", web.HealthCheck.Interval)
	}
	if web.HealthCheck.Timeout != "2s" {
		t.Fatalf("health_check.timeout = %q", web.HealthCheck.Timeout)
	}
	if web.HealthCheck.Retries != 12 {
		t.Fatalf("health_check.retries = %d", web.HealthCheck.Retries)
	}

	appServer := prod.Servers["app-1"]
	if appServer.SSHUser != "root" {
		t.Fatalf("app ssh_user = %q", appServer.SSHUser)
	}
	if appServer.SSHPort != 2201 {
		t.Fatalf("app host_ssh_port = %d", appServer.SSHPort)
	}
	workerServer := prod.Servers["worker-1"]
	if workerServer.SSHPort != 22 {
		t.Fatalf("worker host_ssh_port = %d", workerServer.SSHPort)
	}
}

func TestLoadBytesEnvironmentRetentionInheritsTopLevelRetention(t *testing.T) {
	input := strings.Replace(validConfigYAML, "registry:", "retention:\n  releases: 7\n\nregistry:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if got := cfg.Environments["production"].Retention.Releases; got != 7 {
		t.Fatalf("environment retention.releases = %d, want 7", got)
	}
}

func TestLoadBytesEnvironmentRetentionOverridesTopLevelRetention(t *testing.T) {
	input := strings.Replace(validConfigYAML, "registry:", "retention:\n  releases: 7\n\nregistry:", 1)
	input = strings.Replace(input, "  production:\n    servers:", "  production:\n    retention:\n      releases: 3\n    servers:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if got := cfg.Environments["production"].Retention.Releases; got != 3 {
		t.Fatalf("environment retention.releases = %d, want 3", got)
	}
}

func TestValidateRejectsRetentionBelowTwo(t *testing.T) {
	for _, releases := range []int{0, 1} {
		t.Run(fmt.Sprintf("top-level %d", releases), func(t *testing.T) {
			input := strings.Replace(validConfigYAML, "registry:", fmt.Sprintf("retention:\n  releases: %d\n\nregistry:", releases), 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil {
				t.Fatal("expected retention.releases below 2 to fail")
			}
			if !strings.Contains(err.Error(), "retention.releases") {
				t.Fatalf("expected retention error, got: %v", err)
			}
		})
	}
}

func TestValidateAcceptsRetentionTwo(t *testing.T) {
	input := strings.Replace(validConfigYAML, "registry:", "retention:\n  releases: 2\n\nregistry:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if got := cfg.Retention.Releases; got != 2 {
		t.Fatalf("top-level retention.releases = %d, want 2", got)
	}
}

func TestValidateRejectsExplicitEnvironmentRetentionBelowTwo(t *testing.T) {
	for _, releases := range []int{0, 1} {
		t.Run(fmt.Sprintf("environment %d", releases), func(t *testing.T) {
			input := strings.Replace(validConfigYAML, "  production:\n    servers:", fmt.Sprintf("  production:\n    retention:\n      releases: %d\n    servers:", releases), 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil {
				t.Fatal("expected environment retention.releases below 2 to fail")
			}
			if !strings.Contains(err.Error(), "environments.production.retention.releases") {
				t.Fatalf("expected environment retention error, got: %v", err)
			}
		})
	}
}

func TestLoadBytesRejectsUnknownRetentionFields(t *testing.T) {
	input := strings.Replace(validConfigYAML, "registry:", "retention:\n  days: 30\n\nregistry:", 1)
	_, err := LoadBytes([]byte(input))
	if err == nil {
		t.Fatal("expected unknown retention field to fail")
	}
	if !strings.Contains(err.Error(), "days") {
		t.Fatalf("expected error to name unknown field, got: %v", err)
	}
}

func TestLoadBytesNormalizesRegistryPasswordList(t *testing.T) {
	input := strings.Replace(validConfigYAML, "password_secret: REGISTRY_PASSWORD", "password:\n    - REGISTRY_PASSWORD", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if cfg.Registry.PasswordSecret != "REGISTRY_PASSWORD" {
		t.Fatalf("registry password secret = %q", cfg.Registry.PasswordSecret)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestLoadBytesAcceptsBuildPlatform(t *testing.T) {
	input := strings.Replace(validConfigYAML, "          dockerfile: Dockerfile\n", "          dockerfile: Dockerfile\n          platform: linux/amd64\n", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	platform := cfg.Environments["production"].Services["web"].Build.Platform
	if platform != "linux/amd64" {
		t.Fatalf("build platform = %q", platform)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateRejectsInvalidBuildPlatform(t *testing.T) {
	input := strings.Replace(validConfigYAML, "          dockerfile: Dockerfile\n", "          dockerfile: Dockerfile\n          platform: linux amd64\n", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected invalid build platform to fail")
	}
	if !strings.Contains(err.Error(), "build.platform") || !strings.Contains(err.Error(), "linux amd64") {
		t.Fatalf("expected build platform error, got: %v", err)
	}
}

func TestValidateRejectsMultilineStaticEnvValues(t *testing.T) {
	input := strings.Replace(validConfigYAML, "          RACK_ENV: production\n", "          RACK_ENV: |\n            production\n            MALICIOUS=true\n", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected multiline env value to fail")
	}
	if !strings.Contains(err.Error(), "env.RACK_ENV") || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected env newline error, got: %v", err)
	}
}

func TestValidateAcceptsOnePasswordSecretProvider(t *testing.T) {
	input := strings.Replace(validConfigYAML, "  fetch: op run --env-file=.env.production -- printenv\n  names:", "  provider:\n    type: 1password\n    account: example-account\n    vault: example-vault\n    item: example-item\n  names:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if cfg.Secrets.Provider.Type != "1password" {
		t.Fatalf("provider type = %q", cfg.Secrets.Provider.Type)
	}
	if cfg.Secrets.Provider.Account != "example-account" {
		t.Fatalf("provider account = %q", cfg.Secrets.Provider.Account)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateRejectsOnePasswordProviderWithoutItem(t *testing.T) {
	input := strings.Replace(validConfigYAML, "  fetch: op run --env-file=.env.production -- printenv\n  names:", "  provider:\n    type: 1password\n    account: example-account\n    vault: example-vault\n  names:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected missing 1Password item to fail")
	}
	if !strings.Contains(err.Error(), "secrets.provider.item") {
		t.Fatalf("expected item error, got: %v", err)
	}
}

func TestValidateRejectsFetchAndProviderTogether(t *testing.T) {
	input := strings.Replace(validConfigYAML, "  fetch: op run --env-file=.env.production -- printenv\n  names:", "  fetch: op run --env-file=.env.production -- printenv\n  provider:\n    type: 1password\n    account: acct\n    vault: vault\n    item: item\n  names:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected fetch and provider to fail")
	}
	if !strings.Contains(err.Error(), "secrets.fetch") || !strings.Contains(err.Error(), "secrets.provider") {
		t.Fatalf("expected fetch/provider conflict error, got: %v", err)
	}
}

func TestValidateRejectsAmbiguousRegistryPasswordFields(t *testing.T) {
	input := strings.Replace(validConfigYAML, "password_secret: REGISTRY_PASSWORD", "password_secret: REGISTRY_PASSWORD\n  password:\n    - OTHER_PASSWORD", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected ambiguous registry password fields to fail")
	}
	if !strings.Contains(err.Error(), "registry.password") {
		t.Fatalf("expected registry password error, got: %v", err)
	}
}

func TestLoadBytesRejectsUnknownFields(t *testing.T) {
	input := strings.Replace(validConfigYAML, "host: 203.0.113.10", "hostname: 203.0.113.10", 1)
	_, err := LoadBytes([]byte(input))
	if err == nil {
		t.Fatal("expected unknown YAML field to fail")
	}
	if !strings.Contains(err.Error(), "hostname") {
		t.Fatalf("expected error to name unknown field, got: %v", err)
	}
}

func TestLoadBytesRejectsStaleSchemaFields(t *testing.T) {
	staleInputs := map[string]string{
		"defaults":       strings.Replace(validConfigYAML, "secrets:", "defaults:\n  backend: docker\n\nsecrets:", 1),
		"ssh.port":       strings.Replace(validConfigYAML, "secrets:", "ssh:\n  port: 22\n\nsecrets:", 1),
		"ssh_port":       strings.Replace(validConfigYAML, "host_ssh_port: 2201", "ssh_port: 2201", 1),
		"proxy.provider": strings.Replace(validConfigYAML, "    proxy:\n      routes:", "    proxy:\n      provider: caddy\n      routes:", 1),
		"roles":          strings.Replace(validConfigYAML, "hosts: [app-1]", "roles: [web]", 1),
		"variables":      strings.Replace(validConfigYAML, "names:", "variables:", 1),
		"command":        strings.Replace(validConfigYAML, "hosts: [app-1]", "command: ./server\n        hosts: [app-1]", 1),
	}
	for name, input := range staleInputs {
		t.Run(name, func(t *testing.T) {
			_, err := LoadBytes([]byte(input))
			if err == nil {
				t.Fatal("expected stale field to fail")
			}
		})
	}
}

func TestValidateRejectsInvalidNames(t *testing.T) {
	input := strings.Replace(validConfigYAML, "project: demo_app", "project: Demo_App", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected invalid project name to fail")
	}
	if !strings.Contains(err.Error(), "project") || !strings.Contains(err.Error(), "Demo_App") {
		t.Fatalf("expected project name error, got: %v", err)
	}
}

func TestValidateRejectsServiceHostWithoutMatchingServer(t *testing.T) {
	input := strings.Replace(validConfigYAML, "hosts: [worker-1]\n        secrets:", "hosts: [queue-1]\n        secrets:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected unmatched service host to fail")
	}
	if !strings.Contains(err.Error(), "queue-1") {
		t.Fatalf("expected unmatched host error, got: %v", err)
	}
}

func TestValidateRejectsMissingHostsWhenEnvironmentHasMultipleServers(t *testing.T) {
	input := strings.Replace(validConfigYAML, "        hosts: [worker-1]\n        secrets:", "        secrets:", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected missing hosts to fail with multiple servers")
	}
	if !strings.Contains(err.Error(), "hosts") || !strings.Contains(err.Error(), "worker") {
		t.Fatalf("expected hosts error, got: %v", err)
	}
}

func TestLoadBytesDefaultsHostsWhenEnvironmentHasOneServer(t *testing.T) {
	input := strings.Replace(validConfigYAML, `      worker-1:
        host: 203.0.113.11
        ssh_user: root
`, "", 1)
	input = strings.Replace(input, "        hosts: [app-1]\n", "", 1)
	input = strings.Replace(input, "      worker:\n        image: ghcr.io/example/demo-worker\n        build:\n          context: .\n          dockerfile: Dockerfile.worker\n        hosts: [worker-1]\n        secrets:\n          - DATABASE_URL\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	webHosts := cfg.Environments["production"].Services["web"].Hosts
	if len(webHosts) != 1 || webHosts[0] != "app-1" {
		t.Fatalf("web hosts = %#v", webHosts)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestValidateRejectsUndefinedServiceSecret(t *testing.T) {
	input := strings.Replace(validConfigYAML, "    - REDIS_URL\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected undefined secret to fail")
	}
	if !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("expected undefined secret name in error, got: %v", err)
	}
}

func TestValidateRejectsUndefinedRegistryPasswordSecret(t *testing.T) {
	input := strings.Replace(validConfigYAML, "    - REGISTRY_PASSWORD\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected undefined registry password secret to fail")
	}
	if !strings.Contains(err.Error(), "REGISTRY_PASSWORD") {
		t.Fatalf("expected registry secret name in error, got: %v", err)
	}
}

func TestValidateAcceptsSecretsNamesWithoutFetch(t *testing.T) {
	input := strings.Replace(validConfigYAML, "  fetch: op run --env-file=.env.production -- printenv\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg, err := LoadBytes([]byte(validConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateRejectsInvalidDurations(t *testing.T) {
	input := strings.Replace(validConfigYAML, "path: /healthz", "path: /healthz\n          timeout: someday", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected invalid duration to fail")
	}
	if !strings.Contains(err.Error(), "timeout") || !strings.Contains(err.Error(), "someday") {
		t.Fatalf("expected duration error, got: %v", err)
	}
}

func TestValidateRejectsNonPositiveHealthDurations(t *testing.T) {
	for name, replacement := range map[string]string{
		"zero interval":     "path: /healthz\n          interval: 0s",
		"negative interval": "path: /healthz\n          interval: -1s",
		"zero timeout":      "path: /healthz\n          timeout: 0s",
		"negative timeout":  "path: /healthz\n          timeout: -1s",
	} {
		t.Run(name, func(t *testing.T) {
			input := strings.Replace(validConfigYAML, "path: /healthz", replacement, 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil {
				t.Fatal("expected non-positive duration to fail")
			}
		})
	}
}

func TestValidateRejectsInvalidServiceEnvName(t *testing.T) {
	input := strings.Replace(validConfigYAML, "          RACK_ENV: production", "          rack-env: production", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected invalid env name to fail")
	}
	if !strings.Contains(err.Error(), "rack-env") {
		t.Fatalf("expected invalid env name in error, got: %v", err)
	}
}

func TestValidateRejectsDuplicateServiceEnvAndSecretName(t *testing.T) {
	input := strings.Replace(validConfigYAML, "          RACK_ENV: production", "          DATABASE_URL: postgres://example", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected duplicate env and secret name to fail")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected duplicate name in error, got: %v", err)
	}
}

func TestLoadBytesAcceptsRedisAccessoryAndDependency(t *testing.T) {
	input := configWithRedisAccessory(validConfigYAML)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	redis := cfg.Environments["production"].Accessories["redis"]
	if redis.Type != "redis" || redis.Image != "redis:7-alpine" || redis.Host != "app-1" {
		t.Fatalf("redis accessory = %#v", redis)
	}
	if redis.Storage.Volume != "redis-data" {
		t.Fatalf("redis volume = %q", redis.Storage.Volume)
	}
	if got := cfg.Environments["production"].Services["web"].DependsOn; len(got) != 1 || got[0] != "redis" {
		t.Fatalf("web depends_on = %#v", got)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestLoadBytesAcceptsGuardedPostgresAccessory(t *testing.T) {
	input := configWithPostgresAccessory(validConfigYAML)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	postgres := cfg.Environments["production"].Accessories["postgres"]
	if postgres.Type != "postgres" || postgres.Image != "postgres:17-alpine" || postgres.Host != "app-1" {
		t.Fatalf("postgres accessory = %#v", postgres)
	}
	if major, ok := PostgresMajor(postgres.Image); !ok || major != "17" {
		t.Fatalf("PostgresMajor(%q) = %q, %t", postgres.Image, major, ok)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateRejectsPostgresWithoutGuardableMajor(t *testing.T) {
	for _, image := range []string{"postgres:latest", "postgres:alpine", "postgres@sha256:abcdef"} {
		t.Run(image, func(t *testing.T) {
			input := strings.Replace(configWithPostgresAccessory(validConfigYAML), "postgres:17-alpine", image, 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), "PostgreSQL major version") {
				t.Fatalf("expected PostgreSQL major error, got: %v", err)
			}
		})
	}
}

func TestValidateRejectsPostgresMissingRequiredSettings(t *testing.T) {
	input := configWithPostgresAccessory(validConfigYAML)
	input = strings.Replace(input, "          POSTGRES_DB: app\n          POSTGRES_USER: app\n", "", 1)
	input = strings.Replace(input, "          - POSTGRES_PASSWORD\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "POSTGRES_DB") || !strings.Contains(err.Error(), "POSTGRES_USER") || !strings.Contains(err.Error(), "POSTGRES_PASSWORD") {
		t.Fatalf("expected required PostgreSQL settings errors, got: %v", err)
	}
}

func TestPostgresMajor(t *testing.T) {
	for image, want := range map[string]string{
		"postgres:17-alpine":               "17",
		"postgres:17.4-alpine":             "17",
		"registry.example/app/postgres:16": "16",
	} {
		if got, ok := PostgresMajor(image); !ok || got != want {
			t.Fatalf("PostgresMajor(%q) = %q, %t; want %q", image, got, ok, want)
		}
	}
	for _, image := range []string{"postgres", "postgres:latest", "postgres:beta", "postgres@sha256:abcdef"} {
		if got, ok := PostgresMajor(image); ok {
			t.Fatalf("PostgresMajor(%q) = %q, true", image, got)
		}
	}
}

func TestLoadBytesRejectsUnknownAccessoryFields(t *testing.T) {
	input := strings.Replace(configWithRedisAccessory(validConfigYAML), "        type: redis", "        type: redis\n        ports: [6379]", 1)
	_, err := LoadBytes([]byte(input))
	if err == nil || !strings.Contains(err.Error(), "ports") {
		t.Fatalf("expected unknown accessory field error, got: %v", err)
	}
}

func TestValidateRejectsUnsupportedAccessoryType(t *testing.T) {
	input := strings.Replace(configWithRedisAccessory(validConfigYAML), "type: redis", "type: mysql", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), `type must be "postgres" or "redis"`) {
		t.Fatalf("expected unsupported type error, got: %v", err)
	}
}

func TestValidateRejectsUnpinnedAccessoryImage(t *testing.T) {
	for _, image := range []string{"redis", "redis:latest"} {
		t.Run(image, func(t *testing.T) {
			input := strings.Replace(configWithRedisAccessory(validConfigYAML), "redis:7-alpine", image, 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), "explicit non-latest tag or digest") {
				t.Fatalf("expected pinned image error, got: %v", err)
			}
		})
	}
}

func TestValidateRejectsAccessoryUnknownHostAndSecret(t *testing.T) {
	input := configWithRedisAccessory(validConfigYAML)
	input = strings.Replace(input, "host: app-1\n        secrets:\n          - REDIS_PASSWORD", "host: data-1\n        secrets:\n          - MISSING_PASSWORD", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "data-1") || !strings.Contains(err.Error(), "MISSING_PASSWORD") || !strings.Contains(err.Error(), "REDIS_PASSWORD") {
		t.Fatalf("expected host and secret errors, got: %v", err)
	}
}

func TestValidateRejectsAccessoryEnvSecretDuplicate(t *testing.T) {
	input := strings.Replace(configWithRedisAccessory(validConfigYAML), "        secrets:\n          - REDIS_PASSWORD", "        env:\n          REDIS_PASSWORD: unsafe\n        secrets:\n          - REDIS_PASSWORD", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), `defines "REDIS_PASSWORD" in both env and secrets`) {
		t.Fatalf("expected duplicate env/secret error, got: %v", err)
	}
}

func TestValidateRejectsUnknownOrRemoteAccessoryDependency(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"unknown": func(input string) string {
			return strings.Replace(input, "depends_on: [redis]", "depends_on: [missing]", 1)
		},
		"remote": func(input string) string {
			return strings.Replace(input, "hosts: [app-1]\n        depends_on: [redis]", "hosts: [worker-1]\n        depends_on: [redis]", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := mutate(configWithRedisAccessory(validConfigYAML))
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), "depends_on") {
				t.Fatalf("expected dependency error, got: %v", err)
			}
		})
	}
}

func configWithRedisAccessory(input string) string {
	input = strings.Replace(input, "    - REDIS_URL\n", "    - REDIS_URL\n    - REDIS_PASSWORD\n", 1)
	input = strings.Replace(input, "    services:\n", `    accessories:
      redis:
        type: redis
        image: redis:7-alpine
        host: app-1
        secrets:
          - REDIS_PASSWORD
        storage:
          volume: redis-data
    services:
`, 1)
	return strings.Replace(input, "        hosts: [app-1]\n", "        hosts: [app-1]\n        depends_on: [redis]\n", 1)
}

func configWithPostgresAccessory(input string) string {
	input = strings.Replace(input, "    - REDIS_URL\n", "    - REDIS_URL\n    - POSTGRES_PASSWORD\n", 1)
	return strings.Replace(input, "    services:\n", `    accessories:
      postgres:
        type: postgres
        image: postgres:17-alpine
        host: app-1
        env:
          POSTGRES_DB: app
          POSTGRES_USER: app
        secrets:
          - POSTGRES_PASSWORD
        storage:
          volume: postgres-data
    services:
`, 1)
}

func TestValidateRejectsRoutedServiceWithoutHealthPath(t *testing.T) {
	input := strings.Replace(validConfigYAML, "        health_check:\n          path: /healthz\n", "", 1)
	cfg, err := LoadBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	err = Validate(cfg)
	if err == nil {
		t.Fatal("expected routed service without health path to fail")
	}
	if !strings.Contains(err.Error(), "health_check.path") {
		t.Fatalf("expected health_check.path error, got: %v", err)
	}
}

func TestValidateAllowsNonRoutedWorkerWithoutPortOrHealthPath(t *testing.T) {
	cfg, err := LoadBytes([]byte(validConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	worker := cfg.Environments["production"].Services["worker"]
	if worker.InternalPort != 0 || worker.HealthCheck.Path != "" {
		t.Fatalf("test fixture worker unexpectedly has port/path: %#v", worker)
	}
	if err := ValidateEnv(cfg, "production"); err != nil {
		t.Fatalf("ValidateEnv returned error: %v", err)
	}
}

func TestValidateRejectsProxyRouteHostsWithShellActiveCharacters(t *testing.T) {
	for _, host := range []string{
		"app.example.com`id`",
		"app.example.com;id",
		"app.example.com\"",
		"app.example.com\\",
	} {
		t.Run(host, func(t *testing.T) {
			input := strings.Replace(validConfigYAML, "host: app.example.com", "host: "+host, 1)
			cfg, err := LoadBytes([]byte(input))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			err = Validate(cfg)
			if err == nil {
				t.Fatal("expected unsupported route host to fail validation")
			}
			if !strings.Contains(err.Error(), "proxy.routes[0].host") {
				t.Fatalf("expected route host error, got: %v", err)
			}
		})
	}
}
