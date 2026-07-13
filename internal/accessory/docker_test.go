package accessory

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s4m1nd/slipway/internal/config"
)

func TestDockerApplyBuildsStableRedisLifecycleWithoutLeakingSecrets(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	server := config.Server{Host: "203.0.113.10", SSHUser: "deploy", SSHPort: 2201}
	redis := redisConfig()
	commands, err := manager.Apply(server, "redis", redis, map[string]string{"REDIS_PASSWORD": "super-secret"})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	upload, apply, health := commands[0], commands[1], commands[2]
	if !upload.Sensitive || upload.Stdin != "REDIS_PASSWORD=super-secret\n" {
		t.Fatalf("upload command = %#v", upload)
	}
	for _, command := range commands {
		if strings.Contains(command.Script, "super-secret") || strings.Contains(command.String(), "super-secret") {
			t.Fatalf("command leaked secret: %s", command.String())
		}
		if command.Host != server.Host || command.SSHUser != server.SSHUser || command.SSHPort != server.SSHPort {
			t.Fatalf("remote target = %#v", command)
		}
	}
	for _, want := range []string{
		"demo_production_redis",
		"--network-alias' 'redis'",
		"type=volume,src=redis-data,dst=/data",
		"--restart' 'unless-stopped'",
		"--user' 'redis'",
		"slipway.accessory.config_hash=",
		`redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
		`redis-cli -a "$REDIS_PASSWORD"`,
		`[ ! -f "$ENV_CHANGED_FILE" ]`,
		`RUNNING_IMAGE_ID=$(docker inspect --format '{{.Image}}'`,
	} {
		if !strings.Contains(apply.Script, want) {
			t.Errorf("apply script missing %q:\n%s", want, apply.Script)
		}
	}
	if strings.Contains(apply.Script, "docker volume rm") {
		t.Fatalf("apply script can remove persistent volume:\n%s", apply.Script)
	}
	if !strings.Contains(apply.Script, "refusing to replace persistent volume") {
		t.Fatalf("apply script lacks volume replacement guard:\n%s", apply.Script)
	}
	if !strings.Contains(apply.Script, "refusing to replace unmanaged or differently owned container") {
		t.Fatalf("apply script lacks ownership guard:\n%s", apply.Script)
	}
	if !strings.Contains(health.Script, "did not become healthy") {
		t.Fatalf("health script = %s", health.Script)
	}
}

func TestDockerApplyTracksSecretChangesInPrivateEnvMarkerNotVisibleLabels(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	server := config.Server{Host: "example.test"}
	first, err := manager.Apply(server, "redis", redisConfig(), map[string]string{"REDIS_PASSWORD": "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Apply(server, "redis", redisConfig(), map[string]string{"REDIS_PASSWORD": "second"})
	if err != nil {
		t.Fatal(err)
	}
	if first[1].Script != second[1].Script {
		t.Fatal("visible config hash changed with secret value")
	}
	if strings.Contains(first[1].Script, "first") || strings.Contains(second[1].Script, "second") {
		t.Fatal("resolved secret was embedded in apply script")
	}
	if !strings.Contains(first[0].Script, "cmp -s") || !strings.Contains(first[0].Script, ".env.changed") {
		t.Fatalf("upload command does not track private env changes:\n%s", first[0].Script)
	}
}

func TestDockerApplyRejectsMissingOrMultilineSecrets(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	for name, secrets := range map[string]map[string]string{
		"missing":   {},
		"empty":     {"REDIS_PASSWORD": ""},
		"multiline": {"REDIS_PASSWORD": "first\nsecond"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := manager.Apply(config.Server{}, "redis", redisConfig(), secrets); err == nil {
				t.Fatal("expected Apply to fail")
			}
		})
	}
}

func TestDockerApplyBuildsGuardedPostgresLifecycle(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	commands, err := manager.Apply(config.Server{Host: "example.test"}, "postgres", postgresConfig(), map[string]string{"POSTGRES_PASSWORD": "secret"})
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if len(commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(commands))
	}
	apply := commands[1].Script
	for _, want := range []string{
		"demo_production_postgres",
		"type=volume,src=postgres-data,dst=/var/lib/postgresql/data",
		"postgres:17-alpine",
		"slipway.accessory.postgres_major=17",
		`pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`,
		"PostgreSQL major upgrades require a database migration procedure",
	} {
		if !strings.Contains(apply, want) {
			t.Fatalf("PostgreSQL apply script missing %q:\n%s", want, apply)
		}
	}
	if strings.Contains(apply, "--user' 'postgres'") {
		t.Fatalf("PostgreSQL entrypoint must retain its root-to-postgres initialization path:\n%s", apply)
	}
	if commands[0].Stdin != "POSTGRES_DB=app\nPOSTGRES_PASSWORD=secret\nPOSTGRES_USER=app\n" {
		t.Fatalf("PostgreSQL env = %q", commands[0].Stdin)
	}
}

func TestDockerCommandsStayInsideAccessoryContainer(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	server := config.Server{Host: "example.test"}
	redis := redisConfig()
	logs := manager.Logs(server, "redis", redis, LogsOptions{Tail: 25, Follow: true})
	if !strings.Contains(logs.Script, "docker' 'logs' '--tail' '25' '--follow' 'demo_production_redis") {
		t.Fatalf("logs script = %s", logs.Script)
	}
	if !strings.Contains(logs.Script, "refusing to operate on unmanaged or differently owned container") {
		t.Fatalf("logs script lacks ownership guard: %s", logs.Script)
	}
	restart := manager.Restart(server, "redis", redis)
	if len(restart) != 2 || !strings.Contains(restart[0].Script, "docker restart 'demo_production_redis'") {
		t.Fatalf("restart commands = %#v", restart)
	}
	execCommand, err := manager.Exec(server, "redis", redis, []string{"redis-cli", "--raw", "x; rm -rf /"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(execCommand.Script, `'x; rm -rf /'`) {
		t.Fatalf("exec args were not shell quoted: %s", execCommand.Script)
	}
	if _, err := manager.Exec(server, "redis", redis, nil); err == nil {
		t.Fatal("expected empty exec command to fail")
	}
}

func TestParseAndRenderAccessoryStatus(t *testing.T) {
	target := Target{
		HostName: "app-1",
		Server:   config.Server{Host: "203.0.113.10"},
		Name:     "redis",
		Config:   redisConfig(),
	}
	status, err := ParseStatus(target, "managed=true\nstatus=running\nhealth=healthy\nimage=redis:7-alpine\nconfig_hash=abc\nvolume=redis-data\n")
	if err != nil {
		t.Fatalf("ParseStatus returned error: %v", err)
	}
	if !status.Exists || !status.Managed || status.State != "running" || status.Health != "healthy" || status.Volume != "redis-data" {
		t.Fatalf("status = %#v", status)
	}
	var out bytes.Buffer
	RenderStatuses(&out, "demo", "production", []Status{status})
	for _, want := range []string{"demo/production accessories", "redis", "app-1", "healthy", "redis-data"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("rendered status missing %q:\n%s", want, out.String())
		}
	}
}

func TestParseAccessoryStatusRejectsMalformedOutput(t *testing.T) {
	if _, err := ParseStatus(Target{}, "status running\n"); err == nil {
		t.Fatal("expected malformed output to fail")
	}
}

func TestGeneratedAccessoryShellIsSyntacticallyValid(t *testing.T) {
	manager := NewDocker("demo", "production", "/opt/slipway")
	server := config.Server{Host: "example.test"}
	redis := redisConfig()
	commands, err := manager.Apply(server, "redis", redis, map[string]string{"REDIS_PASSWORD": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	commands = append(commands,
		manager.Inspect(server, "redis", redis),
		manager.Logs(server, "redis", redis, LogsOptions{Tail: 100}),
		manager.Verify(server, "redis", redis),
	)
	commands = append(commands, manager.Restart(server, "redis", redis)...)
	execCommand, err := manager.Exec(server, "redis", redis, []string{"redis-cli", "PING"})
	if err != nil {
		t.Fatal(err)
	}
	commands = append(commands, execCommand)
	for _, command := range commands {
		if strings.Contains(command.Script, "%!") {
			t.Fatalf("generated command contains fmt diagnostic:\n%s", command.Script)
		}
		check := exec.Command("sh", "-n", "-c", command.Script)
		if output, err := check.CombinedOutput(); err != nil {
			t.Fatalf("%s shell syntax: %v\n%s\n%s", command.Description, err, output, command.Script)
		}
	}
}

func TestApplyRefusesUnmanagedContainerBeforeAnyDockerMutation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	fakeDocker := filepath.Join(dir, "docker")
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  inspect)
    case "$*" in
      *slipway.project*) printf 'someone-else\n' ;;
      *slipway.env*) printf 'production\n' ;;
      *slipway.accessory.type*) printf 'redis\n' ;;
      *slipway.accessory*) printf 'redis\n' ;;
      *slipway.accessory.volume*) printf 'redis-data\n' ;;
    esac
    exit 0
    ;;
esac
exit 0
`
	if err := os.WriteFile(fakeDocker, []byte(fake), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	manager := NewDocker("demo", "production", dir)
	commands, err := manager.Apply(config.Server{}, "redis", redisConfig(), map[string]string{"REDIS_PASSWORD": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", commands[1].Script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "DOCKER_LOG="+logPath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected unmanaged container refusal; output=%s", output)
	}
	if !strings.Contains(string(output), "refusing to replace unmanaged or differently owned container") {
		t.Fatalf("unexpected refusal output: %s", output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake docker log: %v", err)
	}
	log := string(logData)
	for _, mutation := range []string{"pull ", "network create", "volume create", "rm ", "run "} {
		if strings.Contains(log, mutation) {
			t.Fatalf("unmanaged guard allowed %q:\n%s", mutation, log)
		}
	}
}

func TestApplyLeavesMatchingHealthyContainerInPlace(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	fakeDocker := filepath.Join(dir, "docker")
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  inspect)
    [ "$2" = --format ] || exit 0
    case "$3" in
      *slipway.project*) printf 'demo\n' ;;
      *slipway.env*) printf 'production\n' ;;
      *slipway.accessory.type*) printf 'redis\n' ;;
      *slipway.accessory.volume*) printf 'redis-data\n' ;;
      *slipway.accessory.config_hash*) printf '%s\n' "$CONFIG_HASH" ;;
      *slipway.accessory*) printf 'redis\n' ;;
      *Config.Image*) printf 'redis:7-alpine\n' ;;
      *State.Running*) printf 'true\n' ;;
      *Image*) printf 'sha256:current\n' ;;
    esac
    ;;
  image)
    printf 'sha256:current\n'
    ;;
esac
exit 0
`
	if err := os.WriteFile(fakeDocker, []byte(fake), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	manager := NewDocker("demo", "production", dir)
	commands, err := manager.Apply(config.Server{}, "redis", redisConfig(), map[string]string{"REDIS_PASSWORD": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	hash := scriptAssignment(commands[1].Script, "CONFIG_HASH")
	if hash == "" {
		t.Fatalf("config hash not found:\n%s", commands[1].Script)
	}
	cmd := exec.Command("sh", "-c", commands[1].Script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "DOCKER_LOG="+logPath, "CONFIG_HASH="+hash)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("idempotent apply failed: %v\n%s", err, output)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake docker log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "pull redis:7-alpine") {
		t.Fatalf("apply did not refresh the configured image reference:\n%s", log)
	}
	for _, mutation := range []string{"network create", "volume create", "rm ", "run ", "start "} {
		if strings.Contains(log, mutation) {
			t.Fatalf("idempotent apply performed %q:\n%s", mutation, log)
		}
	}
}

func TestApplyRefusesPostgresMajorUpgradeBeforeAnyDockerMutation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	fakeDocker := filepath.Join(dir, "docker")
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_LOG"
case "$1" in
  inspect)
    [ "$2" = --format ] || exit 0
    case "$3" in
      *slipway.project*) printf 'demo\n' ;;
      *slipway.env*) printf 'production\n' ;;
      *slipway.accessory.type*) printf 'postgres\n' ;;
      *slipway.accessory.volume*) printf 'postgres-data\n' ;;
      *slipway.accessory.postgres_major*) printf '16\n' ;;
      *slipway.accessory*) printf 'postgres\n' ;;
      *Config.Image*) printf 'postgres:16-alpine\n' ;;
    esac
    ;;
esac
exit 0
`
	if err := os.WriteFile(fakeDocker, []byte(fake), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	manager := NewDocker("demo", "production", dir)
	commands, err := manager.Apply(config.Server{}, "postgres", postgresConfig(), map[string]string{"POSTGRES_PASSWORD": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", commands[1].Script)
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "DOCKER_LOG="+logPath)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected major upgrade refusal; output=%s", output)
	}
	for _, want := range []string{"Refusing to replace postgres automatically", "Configured image: postgres:17-alpine", "Running image: postgres:16-alpine", "major upgrades require a database migration procedure"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("major guard output missing %q:\n%s", want, output)
		}
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fake docker log: %v", err)
	}
	for _, mutation := range []string{"pull ", "network create", "volume create", "rm ", "run "} {
		if strings.Contains(string(logData), mutation) {
			t.Fatalf("major guard allowed %q:\n%s", mutation, logData)
		}
	}
}

func scriptAssignment(script string, name string) string {
	prefix := name + "='"
	for _, line := range strings.Split(script, "\n") {
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, "'") {
			return strings.TrimSuffix(strings.TrimPrefix(line, prefix), "'")
		}
	}
	return ""
}

func redisConfig() config.Accessory {
	return config.Accessory{
		Type:    "redis",
		Image:   "redis:7-alpine",
		Host:    "app-1",
		Secrets: []string{"REDIS_PASSWORD"},
		Storage: config.AccessoryStorage{Volume: "redis-data"},
	}
}

func postgresConfig() config.Accessory {
	return config.Accessory{
		Type:  "postgres",
		Image: "postgres:17-alpine",
		Host:  "app-1",
		Env: map[string]string{
			"POSTGRES_DB":   "app",
			"POSTGRES_USER": "app",
		},
		Secrets: []string{"POSTGRES_PASSWORD"},
		Storage: config.AccessoryStorage{Volume: "postgres-data"},
	}
}
