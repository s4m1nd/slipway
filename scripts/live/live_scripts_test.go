package live_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderConfigCreatesValidSlipwayConfig(t *testing.T) {
	root := repoRoot(t)
	configPath := filepath.Join(root, ".tmp", "live-nginx", "test-slipway.yml")
	_ = os.Remove(configPath)
	t.Cleanup(func() {
		_ = os.Remove(configPath)
	})

	run(t, root, []string{
		"SLIPWAY_LIVE_HOST=192.0.2.10",
		"SLIPWAY_LIVE_SSH_USER=root",
		"SLIPWAY_LIVE_ROUTE_HOST=192.0.2.10",
		"SLIPWAY_LIVE_IMAGE=ghcr.io/example/slipway-live-nginx",
		"SLIPWAY_REGISTRY_SERVER=ghcr.io",
		"SLIPWAY_REGISTRY_USERNAME=example",
		"MOCK_VERSION=v1",
		"SLIPWAY_LIVE_CONFIG=" + configPath,
	}, "sh", "scripts/live/render-config.sh")

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read rendered config: %v", err)
	}
	config := string(data)
	for _, want := range []string{
		"project: slipway_live",
		"username: example",
		"password:",
		"host: 192.0.2.10",
		"ssh_user: root",
		"host: http://192.0.2.10",
		"tls: false",
		"internal_port: 80",
		"path: /healthz",
		"- MOCK_VERSION=v1",
		"- REGISTRY_PASSWORD",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, config)
		}
	}
	if strings.Contains(config, "super-secret") {
		t.Fatalf("rendered config contains a secret value:\n%s", config)
	}

	run(t, root, nil, "go", "run", "./cmd/slipway", "validate", "-c", configPath, "--env", "production")
}

func TestRenderConfigIncludesOnePasswordFetchCommand(t *testing.T) {
	root := repoRoot(t)
	configPath := filepath.Join(root, ".tmp", "live-nginx", "test-op-slipway.yml")
	_ = os.Remove(configPath)
	t.Cleanup(func() {
		_ = os.Remove(configPath)
	})

	run(t, root, []string{
		"SLIPWAY_LIVE_HOST=203.0.113.10",
		"SLIPWAY_LIVE_SSH_USER=root",
		"SLIPWAY_LIVE_ROUTE_HOST=203.0.113.10",
		"SLIPWAY_LIVE_IMAGE=ghcr.io/example/slipway-live-nginx",
		"SLIPWAY_REGISTRY_SERVER=ghcr.io",
		"SLIPWAY_REGISTRY_USERNAME=example",
		"SLIPWAY_LIVE_SECRETS_FETCH=SLIPWAY_OP_ACCOUNT=acct SLIPWAY_OP_VAULT=vault SLIPWAY_OP_ITEM=item scripts/live/onepassword-fetch.sh",
		"SLIPWAY_LIVE_CONFIG=" + configPath,
	}, "sh", "scripts/live/render-config.sh")

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read rendered config: %v", err)
	}
	config := string(data)
	for _, want := range []string{
		"fetch: 'SLIPWAY_OP_ACCOUNT=acct SLIPWAY_OP_VAULT=vault SLIPWAY_OP_ITEM=item scripts/live/onepassword-fetch.sh'",
		"username: example",
		"password:",
		"- REGISTRY_PASSWORD",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, config)
		}
	}

	run(t, root, nil, "go", "run", "./cmd/slipway", "validate", "-c", configPath, "--env", "production")
}

func TestCheckSecretsUsesRegistryPasswordFromEnv(t *testing.T) {
	root := repoRoot(t)
	output, err := runScript(root, []string{
		"REGISTRY_PASSWORD=super-secret",
	}, "scripts/live/check-secrets.sh")
	if err != nil {
		t.Fatalf("check-secrets failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "using REGISTRY_PASSWORD from environment") {
		t.Fatalf("check-secrets did not report env mode:\n%s", output)
	}
	if strings.Contains(output, "super-secret") {
		t.Fatalf("check-secrets printed a secret value:\n%s", output)
	}
}

func TestCheckSecretsFailsWhenNoSecretModeIsConfigured(t *testing.T) {
	root := repoRoot(t)
	output, err := runScript(root, nil, "scripts/live/check-secrets.sh")
	if err == nil {
		t.Fatalf("check-secrets succeeded without a secret mode:\n%s", output)
	}
	for _, want := range []string{
		"export REGISTRY_PASSWORD",
		"OP_SERVICE_ACCOUNT_TOKEN",
		"SLIPWAY_LIVE_SECRETS_FETCH",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("check-secrets missing guidance %q:\n%s", want, output)
		}
	}
}

func TestOnePasswordFetchFailsClearlyWhenOpIsMissing(t *testing.T) {
	root := repoRoot(t)
	output, err := runScriptWithPath(root, t.TempDir(), []string{
		"SLIPWAY_SECRET_NAMES=REGISTRY_PASSWORD",
		"SLIPWAY_OP_ACCOUNT=acct",
		"SLIPWAY_OP_VAULT=vault",
		"SLIPWAY_OP_ITEM=item",
	}, "scripts/live/onepassword-fetch.sh")
	if err == nil {
		t.Fatalf("onepassword-fetch succeeded without op:\n%s", output)
	}
	if !strings.Contains(output, "1Password CLI 'op' is required") {
		t.Fatalf("onepassword-fetch did not explain missing op:\n%s", output)
	}
}

func TestOnePasswordFetchRejectsSecretNamesWithInvalidCharacters(t *testing.T) {
	root := repoRoot(t)
	fakeBin := fakeOpBin(t)
	output, err := runScriptWithPath(root, fakeBin, []string{
		"OP_SERVICE_ACCOUNT_TOKEN=fake-token",
		"SLIPWAY_SECRET_NAMES=AB-bad",
		"SLIPWAY_OP_ACCOUNT=acct",
		"SLIPWAY_OP_VAULT=vault",
		"SLIPWAY_OP_ITEM=item",
	}, "scripts/live/onepassword-fetch.sh")
	if err == nil {
		t.Fatalf("onepassword-fetch accepted invalid secret name:\n%s", output)
	}
	if !strings.Contains(output, "invalid secret name requested") {
		t.Fatalf("onepassword-fetch did not reject invalid secret name:\n%s", output)
	}
	assertFakeOpNotRun(t, fakeBin)
}

func TestSmokePrintCommandsDoesNotRequireLiveDependencies(t *testing.T) {
	root := repoRoot(t)
	output, err := runScript(root, nil, "scripts/live/smoke.sh", "--print-commands")
	if err != nil {
		t.Fatalf("smoke --print-commands failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"MOCK_VERSION=v1 scripts/live/render-config.sh",
		"go run ./cmd/slipway validate -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
		"go run ./cmd/slipway provision -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
		"SLIPWAY_GIT_SHA=111111111111 go run ./cmd/slipway deploy -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
		"curl -fsS \"$SLIPWAY_LIVE_ROUTE_URL/healthz\"",
		"go run ./cmd/slipway status -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
		"MOCK_VERSION=v2 scripts/live/render-config.sh",
		"SLIPWAY_GIT_SHA=222222222222 go run ./cmd/slipway deploy -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
		"go run ./cmd/slipway rollback -c \"$SLIPWAY_LIVE_CONFIG\" --env production",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("smoke --print-commands missing %q:\n%s", want, output)
		}
	}
}

func TestMakeLiveSmokeDoesNotCarryStaleSecretModeCheck(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)
	for _, forbidden := range []string{
		"set REGISTRY_PASSWORD or SLIPWAY_LIVE_SECRETS_FETCH",
		`[ -z "$$SLIPWAY_LIVE_SECRETS_FETCH" ] && [ -z "$$REGISTRY_PASSWORD" ]`,
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("Makefile live-smoke still has stale generated-config secret check %q:\n%s", forbidden, makefile)
		}
	}
}

func TestLiveNginxDockerfileUsesNeutralMockVersionDefault(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "examples/live-nginx/Dockerfile"))
	if err != nil {
		t.Fatalf("read live nginx Dockerfile: %v", err)
	}
	if !strings.Contains(string(data), "ARG MOCK_VERSION=unknown") {
		t.Fatalf("Dockerfile should default MOCK_VERSION to unknown:\n%s", data)
	}
}

func TestPrepareServerPrintCommandsDoesNotExecuteSSH(t *testing.T) {
	root := repoRoot(t)
	fakeBin := fakeSSHBin(t)
	output, err := runScriptWithPath(root, fakeBin, nil, "scripts/live/prepare-server.sh", "root@203.0.113.10", "--print-commands")
	if err != nil {
		t.Fatalf("prepare --print-commands failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "ssh 'root@203.0.113.10'") {
		t.Fatalf("prepare --print-commands did not print ssh commands:\n%s", output)
	}
	if strings.Contains(output, "systemctl stop caddy") {
		t.Fatalf("prepare --print-commands printed stop command without explicit flag:\n%s", output)
	}
	assertFakeSSHNotRun(t, fakeBin)
}

func TestPrepareServerPrintCommandsIncludesExplicitStop(t *testing.T) {
	root := repoRoot(t)
	fakeBin := fakeSSHBin(t)
	output, err := runScriptWithPath(root, fakeBin, nil, "scripts/live/prepare-server.sh", "root@203.0.113.10", "--stop-system-caddy", "--print-commands")
	if err != nil {
		t.Fatalf("prepare stop --print-commands failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "systemctl stop caddy") {
		t.Fatalf("prepare stop --print-commands missing stop command:\n%s", output)
	}
	assertFakeSSHNotRun(t, fakeBin)
}

func TestRestoreCaddyPrintCommandsDoesNotExecuteSSH(t *testing.T) {
	root := repoRoot(t)
	fakeBin := fakeSSHBin(t)
	output, err := runScriptWithPath(root, fakeBin, nil, "scripts/live/restore-caddy.sh", "root@203.0.113.10", "/root/slipway-backups/example", "--print-commands")
	if err != nil {
		t.Fatalf("restore --print-commands failed: %v\n%s", err, output)
	}
	for _, want := range []string{
		"ssh 'root@203.0.113.10'",
		"docker rm -f slipway_live_production_caddy",
		"restored_from=/root/slipway-backups/example",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("restore --print-commands missing %q:\n%s", want, output)
		}
	}
	assertFakeSSHNotRun(t, fakeBin)
}

func TestPrintModesDoNotPrintRegistryPasswordValue(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		name string
		args []string
	}{
		{name: "smoke", args: []string{"scripts/live/smoke.sh", "--print-commands"}},
		{name: "prepare", args: []string{"scripts/live/prepare-server.sh", "root@203.0.113.10", "--print-commands"}},
		{name: "restore", args: []string{"scripts/live/restore-caddy.sh", "root@203.0.113.10", "/root/slipway-backups/example", "--print-commands"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output, err := runScript(root, []string{"REGISTRY_PASSWORD=super-secret", "OP_SERVICE_ACCOUNT_TOKEN=op-super-secret"}, tc.args[0], tc.args[1:]...)
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", strings.Join(tc.args, " "), err, output)
			}
			for _, secret := range []string{"super-secret", "op-super-secret"} {
				if strings.Contains(output, secret) {
					t.Fatalf("%s printed a secret value %q:\n%s", tc.name, secret, output)
				}
			}
		})
	}
}

func TestPrepareServerRequiresExplicitFlagsBeforeStoppingOrPurgingCaddy(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts", "live", "prepare-server.sh"))
	if err != nil {
		t.Fatalf("read prepare-server.sh: %v", err)
	}
	script := string(data)
	stopIndex := strings.Index(script, "--stop-system-caddy")
	purgeIndex := strings.Index(script, "--purge-system-caddy")
	if stopIndex < 0 {
		t.Fatal("prepare-server.sh must expose --stop-system-caddy")
	}
	if purgeIndex < 0 {
		t.Fatal("prepare-server.sh must expose --purge-system-caddy")
	}
	if !strings.Contains(script, "requires --stop-system-caddy") {
		t.Fatal("prepare-server.sh must reject purge unless stop is also explicit")
	}
	stopCommandIndex := strings.Index(script, "systemctl stop caddy")
	disableCommandIndex := strings.Index(script, "systemctl disable caddy")
	if stopCommandIndex < stopIndex || disableCommandIndex < stopIndex {
		t.Fatalf("system Caddy stop/disable must appear behind the explicit stop flag:\n%s", script)
	}
	if strings.Index(script, "apt-get purge") < purgeIndex {
		t.Fatalf("apt purge must appear behind the explicit purge flag:\n%s", script)
	}
}

func TestLiveSSHHelpersDoNotPipeScriptsOverSSHStdin(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"prepare-server.sh", "restore-caddy.sh"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "scripts", "live", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			script := string(data)
			if strings.Contains(script, "<<'REMOTE'") {
				t.Fatalf("%s should not send its remote script over SSH stdin", name)
			}
			if strings.Contains(script, "sh -s") || strings.Contains(script, "REMOTE_PAYLOAD") {
				t.Fatalf("%s should avoid SSH stdin helper transports", name)
			}
			if !strings.Contains(script, "run_remote()") {
				t.Fatalf("%s should use short remote commands", name)
			}
		})
	}
}

func TestLiveScriptsDoNotPrintSecretValues(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{"check-local-docker.sh", "check-secrets.sh", "prepare-server.sh", "render-config.sh", "smoke.sh", "restore-caddy.sh", "onepassword-fetch.sh"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, "scripts", "live", name))
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			script := string(data)
			for _, forbidden := range []string{
				`echo "$REGISTRY_PASSWORD"`,
				`echo "${REGISTRY_PASSWORD}"`,
				`printf '%s\n' "$REGISTRY_PASSWORD"`,
				`printf "%s\n" "$REGISTRY_PASSWORD"`,
				`echo "$OP_SERVICE_ACCOUNT_TOKEN"`,
				`echo "${OP_SERVICE_ACCOUNT_TOKEN}"`,
				`printf '%s\n' "$OP_SERVICE_ACCOUNT_TOKEN"`,
				`printf "%s\n" "$OP_SERVICE_ACCOUNT_TOKEN"`,
			} {
				if strings.Contains(script, forbidden) {
					t.Fatalf("%s may print or write a secret via %q", name, forbidden)
				}
			}
		})
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func run(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
}

func runScript(root string, env []string, script string, args ...string) (string, error) {
	return runScriptWithPath(root, "/usr/bin:/bin:/usr/sbin:/sbin", env, script, args...)
}

func runScriptWithPath(root string, path string, env []string, script string, args ...string) (string, error) {
	cmdArgs := append([]string{script}, args...)
	cmd := exec.Command("/bin/sh", cmdArgs...)
	cmd.Dir = root
	cmd.Env = append(cleanLiveEnv(), "PATH="+path)
	cmd.Env = append(cmd.Env, env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func cleanLiveEnv() []string {
	blocked := map[string]bool{
		"REGISTRY_PASSWORD":            true,
		"OP_SERVICE_ACCOUNT_TOKEN":     true,
		"SLIPWAY_LIVE_SECRETS_FETCH":   true,
		"SLIPWAY_SECRET_NAMES":         true,
		"SLIPWAY_OP_ACCOUNT":           true,
		"SLIPWAY_OP_VAULT":             true,
		"SLIPWAY_OP_ITEM":              true,
		"SLIPWAY_OP_FIELD_PREFIX":      true,
		"SLIPWAY_LIVE_IMAGE":           true,
		"SLIPWAY_REGISTRY_SERVER":      true,
		"SLIPWAY_REGISTRY_USERNAME":    true,
		"SLIPWAY_LIVE_HOST":            true,
		"SLIPWAY_LIVE_SSH_USER":        true,
		"SLIPWAY_LIVE_ROUTE_HOST":      true,
		"SLIPWAY_LIVE_ROUTE_URL":       true,
		"SLIPWAY_LIVE_CONFIG":          true,
		"SLIPWAY_LIVE_TARGET":          true,
		"SLIPWAY_LIVE_SSH_PORT":        true,
		"SLIPWAY_LIVE_REQUIRED_SECRET": true,
	}
	var env []string
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && blocked[name] {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func fakeSSHBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ran > \"$0.ran\"\nexit 43\n"), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	return dir + ":/usr/bin:/bin:/usr/sbin:/sbin"
}

func fakeOpBin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "op")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho ran > \"$0.ran\"\nexit 43\n"), 0o755); err != nil {
		t.Fatalf("write fake op: %v", err)
	}
	return dir + ":/usr/bin:/bin:/usr/sbin:/sbin"
}

func assertFakeSSHNotRun(t *testing.T, path string) {
	t.Helper()
	dir, _, _ := strings.Cut(path, ":")
	marker := filepath.Join(dir, "ssh.ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("fake ssh was executed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat fake ssh marker: %v", err)
	}
}

func assertFakeOpNotRun(t *testing.T, path string) {
	t.Helper()
	dir, _, _ := strings.Cut(path, ":")
	marker := filepath.Join(dir, "op.ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("fake op was executed")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat fake op marker: %v", err)
	}
}
