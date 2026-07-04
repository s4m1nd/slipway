package runtime

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s4m1nd/slipway/internal/config"
)

func TestRecordActiveScriptPreservesPreviousState(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.RecordActive(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", "ghcr.io/example/demo-web:20260630T120000Z", "20260630T120000Z")

	for _, want := range []string{"previous_release", "previous_color", "previous_image", "updated_at"} {
		if !strings.Contains(cmd.Script, want) {
			t.Fatalf("record script missing %q:\n%s", want, cmd.Script)
		}
	}
	for _, want := range []string{`json_field release`, `json_field color`, `json_field image`} {
		if !strings.Contains(cmd.Script, want) {
			t.Fatalf("record script does not read existing %q:\n%s", want, cmd.Script)
		}
	}
}

func TestTargetColorScriptStillReadsTopLevelColor(t *testing.T) {
	script := targetColorScript("/opt/slipway/demo/production/state/web.json")
	if !strings.Contains(script, `"color"[[:space:]]*:[[:space:]]*"blue"`) {
		t.Fatalf("targetColorScript no longer checks top-level color exactly:\n%s", script)
	}
	if strings.Contains(script, "previous_color") {
		t.Fatalf("targetColorScript should not use previous_color:\n%s", script)
	}
}

func TestTargetColorScriptHandlesFormattedStateJSON(t *testing.T) {
	state := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(state, []byte("{\n  \"previous_color\": \"green\",\n  \"color\": \"blue\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	out, err := exec.Command("sh", "-c", targetColorScript(state)).Output()
	if err != nil {
		t.Fatalf("run target color script: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "green" {
		t.Fatalf("target color = %q, want green", got)
	}
}

func TestInspectServiceProducesReadOnlyStatusCommand(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.InspectService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{})

	if cmd.Sensitive {
		t.Fatal("status inspection command should not be sensitive")
	}
	for _, want := range []string{"inspect status for web", "emit state present", "active_release", "previous_release", `emit "${color}_running"`, "inspect_container blue", "inspect_container green", "docker inspect"} {
		if !strings.Contains(cmd.Description+cmd.Script, want) {
			t.Fatalf("inspect command missing %q:\n%s", want, cmd.Script)
		}
	}
}

func TestLogsServiceActiveReadsTopLevelColorFromState(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: "active", Tail: 100})

	for _, want := range []string{`STATE='/opt/slipway/demo/production/state/web.json'`, `json_field color "$STATE"`, `missing state for web`, `docker logs --tail 100 "$CONTAINER"`} {
		if !strings.Contains(cmd.Script, want) {
			t.Fatalf("active logs script missing %q:\n%s", want, cmd.Script)
		}
	}
	if strings.Contains(cmd.Script, "previous_color") {
		t.Fatalf("active logs script should not read previous_color:\n%s", cmd.Script)
	}
}

func TestLogsServicePreviousReadsPreviousColorFromState(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: "previous", Tail: 50})

	for _, want := range []string{`json_field previous_color "$STATE"`, `missing previous_color for web`, `docker logs --tail 50 "$CONTAINER"`} {
		if !strings.Contains(cmd.Script, want) {
			t.Fatalf("previous logs script missing %q:\n%s", want, cmd.Script)
		}
	}
}

func TestLogsServiceExplicitColorsDoNotRequireState(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	for _, color := range []string{"blue", "green"} {
		cmd := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: color, Tail: 25})
		if strings.Contains(cmd.Script, `[ ! -f "$STATE" ]`) {
			t.Fatalf("%s logs script should not require state:\n%s", color, cmd.Script)
		}
		if !strings.Contains(cmd.Script, "COLOR="+color) {
			t.Fatalf("%s logs script should set explicit color:\n%s", color, cmd.Script)
		}
	}
}

func TestLogsServiceFollowFlagIsOptional(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	withoutFollow := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: "blue", Tail: 10})
	withFollow := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: "blue", Tail: 10, Follow: true})

	if strings.Contains(withoutFollow.Script, " -f ") {
		t.Fatalf("logs script without follow included -f:\n%s", withoutFollow.Script)
	}
	if !strings.Contains(withFollow.Script, `docker logs --tail 10 -f "$CONTAINER"`) {
		t.Fatalf("logs script with follow missing -f:\n%s", withFollow.Script)
	}
}

func TestLogsServiceChecksContainerBeforeStreamingAndDoesNotRequireJQ(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{}, LogsOptions{Color: "green", Tail: 100})

	if !strings.Contains(cmd.Script, `docker inspect "$CONTAINER"`) {
		t.Fatalf("logs script should inspect target container before docker logs:\n%s", cmd.Script)
	}
	if strings.Contains(cmd.Script, "jq") {
		t.Fatalf("logs script should not require jq:\n%s", cmd.Script)
	}
}

func TestLogsServiceDoesNotContainSecretValues(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.LogsService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{
		Env:     map[string]string{"DATABASE_URL": "postgres://secret-value"},
		Secrets: []string{"REGISTRY_PASSWORD"},
	}, LogsOptions{Color: "active", Tail: 100})

	if cmd.Sensitive {
		t.Fatal("logs command should not be marked sensitive because Slipway adds no secret input")
	}
	for _, secret := range []string{"postgres://secret-value", "REGISTRY_PASSWORD"} {
		if strings.Contains(cmd.Script, secret) {
			t.Fatalf("logs script leaked %q:\n%s", secret, cmd.Script)
		}
	}
}

func TestDeployLockScriptsAcquireReleaseOwnerAndExpireStaleLock(t *testing.T) {
	root := t.TempDir()
	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	lockDir := filepath.Join(root, "locks", "deploy.lock")
	metadataPath := filepath.Join(lockDir, "metadata")

	first := LockOptions{Operation: "deploy", Timeout: time.Hour, Owner: "owner-1"}
	contender := LockOptions{Operation: "sync-proxy", Timeout: time.Hour, Owner: "owner-2"}
	staleContender := LockOptions{Operation: "sync-proxy", Timeout: time.Second, Owner: "owner-2"}

	runScript(t, docker.acquireLockScript(first))
	metadata := readFile(t, metadataPath)
	for _, want := range []string{"owner=owner-1", "command=deploy", "user=", "host=", "pid=", "timestamp="} {
		if !strings.Contains(metadata, want) {
			t.Fatalf("lock metadata missing %q:\n%s", want, metadata)
		}
	}

	runScriptExpectFailure(t, docker.acquireLockScript(contender))
	if got := readFile(t, metadataPath); !strings.Contains(got, "owner=owner-1") {
		t.Fatalf("failed acquire should not replace active lock:\n%s", got)
	}

	runScriptExpectFailure(t, docker.releaseLockScript(contender))
	if _, err := os.Stat(lockDir); err != nil {
		t.Fatalf("wrong owner release removed lock: %v", err)
	}

	runScript(t, docker.releaseLockScript(first))
	if _, err := os.Stat(lockDir); !os.IsNotExist(err) {
		t.Fatalf("matching owner release should remove lock, stat err=%v", err)
	}

	runScript(t, docker.acquireLockScript(first))
	metadata = readFile(t, metadataPath)
	metadata = replaceMetadataLine(metadata, "created_at_epoch", "1")
	if err := os.WriteFile(metadataPath, []byte(metadata), 0o600); err != nil {
		t.Fatalf("make lock stale: %v", err)
	}
	runScript(t, docker.acquireLockScript(staleContender))
	if got := readFile(t, metadataPath); !strings.Contains(got, "owner=owner-2") || !strings.Contains(got, "command=sync-proxy") {
		t.Fatalf("stale acquire should replace lock metadata:\n%s", got)
	}
}

func TestUploadEnvScriptReleaseIDCannotInjectShellTokens(t *testing.T) {
	root := t.TempDir()
	pwned := "pwned"
	maliciousRelease := "20260701T120000Z\"; touch " + pwned + "; #"
	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.UploadEnv(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", maliciousRelease, "RACK_ENV=production\n")

	sh := exec.Command("sh", "-c", cmd.Script)
	sh.Dir = root
	if output, err := sh.CombinedOutput(); err != nil {
		t.Fatalf("run upload script: %v\n%s\nscript:\n%s", err, string(output), cmd.Script)
	}

	if _, err := os.Stat(filepath.Join(root, pwned)); err == nil {
		t.Fatalf("upload env script executed injected shell token; script:\n%s", cmd.Script)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat injected marker: %v", err)
	}
}

func TestBuildArgsRemainShellQuotedAsSingleArguments(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.BuildService("web", config.Service{
		Build: config.Build{
			Context:    ".",
			Dockerfile: "Dockerfile",
			Args:       []string{"MOCK_VERSION=hello world; touch /tmp/slipway-pwned"},
		},
	}, "ghcr.io/example/demo-web:20260701T120000Z")

	if !strings.Contains(cmd.Script, "--build-arg 'MOCK_VERSION=hello world; touch /tmp/slipway-pwned'") {
		t.Fatalf("build arg was not shell-quoted as one argument:\n%s", cmd.Script)
	}
}

func TestBuildServiceResolvesRelativeDockerfileFromContext(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.BuildService("web", config.Service{
		Build: config.Build{
			Context:    "examples/live-nginx",
			Dockerfile: "Dockerfile",
		},
	}, "ghcr.io/example/demo-web:20260702T102728Z")

	if !strings.Contains(cmd.Script, "-f 'examples/live-nginx/Dockerfile' 'examples/live-nginx'") {
		t.Fatalf("build script should resolve dockerfile relative to context:\n%s", cmd.Script)
	}
}

func TestBuildServiceKeepsAbsoluteDockerfilePath(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.BuildService("web", config.Service{
		Build: config.Build{
			Context:    "examples/live-nginx",
			Dockerfile: "/tmp/custom.Dockerfile",
		},
	}, "ghcr.io/example/demo-web:20260702T102728Z")

	if !strings.Contains(cmd.Script, "-f '/tmp/custom.Dockerfile' 'examples/live-nginx'") {
		t.Fatalf("build script should keep absolute dockerfile path:\n%s", cmd.Script)
	}
}

func TestBuildServiceAddsConfiguredPlatform(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.BuildService("web", config.Service{
		Build: config.Build{
			Context:    "examples/live-nginx",
			Dockerfile: "Dockerfile",
			Platform:   "linux/amd64",
		},
	}, "ghcr.io/example/demo-web:20260702T103054Z")

	if !strings.Contains(cmd.Script, "--platform 'linux/amd64'") {
		t.Fatalf("build script should pass configured platform:\n%s", cmd.Script)
	}
	if !strings.Contains(cmd.Script, "--platform 'linux/amd64' --build-arg") && strings.Contains(cmd.Script, "--build-arg") {
		t.Fatalf("build platform should appear before build args for readability:\n%s", cmd.Script)
	}
}

func TestCleanupServiceScriptReadsStateAndDoesNotRequireJQ(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{
		Image: "ghcr.io/example/demo-web",
		Env:   map[string]string{"DATABASE_URL": "postgres://secret-value"},
	}, CleanupOptions{KeepReleases: 5})

	if cmd.Sensitive {
		t.Fatal("cleanup command should not be sensitive because it receives no secret input")
	}
	for _, want := range []string{
		`STATE='/opt/slipway/demo/production/state/web.json'`,
		"json_field release",
		"json_field image",
		"json_field previous_release",
		"json_field previous_image",
		"KEEP_RELEASES=5",
	} {
		if !strings.Contains(cmd.Script, want) {
			t.Fatalf("cleanup script missing %q:\n%s", want, cmd.Script)
		}
	}
	for _, forbidden := range []string{"jq", "postgres://secret-value"} {
		if strings.Contains(cmd.Script, forbidden) {
			t.Fatalf("cleanup script should not contain %q:\n%s", forbidden, cmd.Script)
		}
	}
}

func TestCleanupServiceFailsSafelyWhenStateFileIsMissing(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "secrets"), 0o755); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	envFile := filepath.Join(root, "secrets", "web-blue-20260629T120000Z.env")
	if err := os.WriteFile(envFile, []byte("RACK_ENV=production\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	dockerLog := fakeDockerImages(t, "")

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})
	runScriptExpectFailure(t, cmd.Script)

	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("cleanup removed env file even though state was missing: %v", err)
	}
	if got := strings.TrimSpace(readFile(t, dockerLog)); got != "" {
		t.Fatalf("cleanup should not call docker when state is missing, got:\n%s", got)
	}
}

func TestCleanupServiceRemovesOnlyOldTargetServiceEnvFiles(t *testing.T) {
	root := t.TempDir()
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	writeState(t, root, "web", `{
  "release": "20260701T120000Z",
  "image": "ghcr.io/example/demo-web:20260701T120000Z",
  "previous_release": "20260630T120000Z",
  "previous_image": "ghcr.io/example/demo-web:20260630T120000Z"
}`)
	for _, name := range []string{
		"web-green-20260701T120000Z.env",
		"web-blue-20260630T120000Z.env",
		"web-green-20260629T120000Z.env",
		"web-blue-20260628T120000Z.env",
		"worker-blue-20260628T120000Z.env",
		"web-extra-blue-20260628T120000Z.env",
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte("RACK_ENV=production\n"), 0o600); err != nil {
			t.Fatalf("write env file %s: %v", name, err)
		}
	}
	fakeDockerImages(t, "")

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})
	runScript(t, cmd.Script)

	for _, name := range []string{
		"web-green-20260701T120000Z.env",
		"web-blue-20260630T120000Z.env",
		"worker-blue-20260628T120000Z.env",
		"web-extra-blue-20260628T120000Z.env",
	} {
		if _, err := os.Stat(filepath.Join(secretsDir, name)); err != nil {
			t.Fatalf("expected %s to be preserved: %v", name, err)
		}
	}
	for _, name := range []string{"web-green-20260629T120000Z.env", "web-blue-20260628T120000Z.env"} {
		if _, err := os.Stat(filepath.Join(secretsDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed, stat err=%v", name, err)
		}
	}
}

func TestCleanupServicePreservesActiveAndPreviousArtifactsOutsideRetentionWindow(t *testing.T) {
	root := t.TempDir()
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	writeState(t, root, "web", `{
  "release": "20260601T120000Z",
  "image": "ghcr.io/example/demo-web:20260601T120000Z",
  "previous_release": "20260602T120000Z",
  "previous_image": "ghcr.io/example/demo-web:20260602T120000Z"
}`)
	for _, name := range []string{
		"web-green-20260701T120000Z.env",
		"web-blue-20260630T120000Z.env",
		"web-green-20260601T120000Z.env",
		"web-blue-20260602T120000Z.env",
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte("RACK_ENV=production\n"), 0o600); err != nil {
			t.Fatalf("write env file %s: %v", name, err)
		}
	}
	imageList := strings.Join([]string{
		"ghcr.io/example/demo-web:20260701T120000Z",
		"ghcr.io/example/demo-web:20260630T120000Z",
		"ghcr.io/example/demo-web:20260601T120000Z",
		"ghcr.io/example/demo-web:20260602T120000Z",
	}, "\n")
	dockerLog := fakeDockerImages(t, imageList)

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})
	runScript(t, cmd.Script)

	for _, name := range []string{"web-green-20260601T120000Z.env", "web-blue-20260602T120000Z.env"} {
		if _, err := os.Stat(filepath.Join(secretsDir, name)); err != nil {
			t.Fatalf("expected active/previous env file %s to be preserved: %v", name, err)
		}
	}
	got := strings.TrimSpace(readFile(t, dockerLog))
	for _, forbidden := range []string{
		"image rm ghcr.io/example/demo-web:20260601T120000Z",
		"image rm ghcr.io/example/demo-web:20260602T120000Z",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("cleanup removed active/previous image via %q:\n%s", forbidden, got)
		}
	}
}

func TestCleanupServiceIsIdempotent(t *testing.T) {
	root := t.TempDir()
	secretsDir := filepath.Join(root, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatalf("create secrets dir: %v", err)
	}
	writeState(t, root, "web", `{
  "release": "20260701T120000Z",
  "image": "ghcr.io/example/demo-web:20260701T120000Z",
  "previous_release": "20260630T120000Z",
  "previous_image": "ghcr.io/example/demo-web:20260630T120000Z"
}`)
	for _, name := range []string{
		"web-green-20260701T120000Z.env",
		"web-blue-20260630T120000Z.env",
		"web-green-20260629T120000Z.env",
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte("RACK_ENV=production\n"), 0o600); err != nil {
			t.Fatalf("write env file %s: %v", name, err)
		}
	}
	fakeDockerImages(t, "ghcr.io/example/demo-web:20260701T120000Z\nghcr.io/example/demo-web:20260630T120000Z\nghcr.io/example/demo-web:20260629T120000Z")

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})

	runScript(t, cmd.Script)
	runScript(t, cmd.Script)
}

func TestCleanupServiceDoesNotRemoveContainers(t *testing.T) {
	docker := Docker{Project: "demo", EnvName: "production", Root: "/opt/slipway/demo/production", Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})

	if strings.Contains(cmd.Script, "docker container rm") || strings.Contains(cmd.Script, "docker rm") {
		t.Fatalf("cleanup script must not remove containers:\n%s", cmd.Script)
	}
}

func TestCleanupServicePrunesOnlyTargetImageRepositoryAndIgnoresImageRemoveFailures(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "web", `{
  "release": "20260701T120000Z",
  "image": "ghcr.io/example/demo-web:20260701T120000Z",
  "previous_release": "20260630T120000Z",
  "previous_image": "ghcr.io/example/demo-web:20260630T120000Z"
}`)
	imageList := strings.Join([]string{
		"ghcr.io/example/demo-web:20260701T120000Z",
		"ghcr.io/example/demo-web:20260630T120000Z",
		"ghcr.io/example/demo-web:20260629T120000Z",
		"ghcr.io/example/demo-web:20260628T120000Z",
		"ghcr.io/example/demo-web:latest",
		"ghcr.io/example/demo-api:20260628T120000Z",
		"ghcr.io/example/other:20260628T120000Z",
	}, "\n")
	dockerLog := fakeDockerImages(t, imageList)

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web:latest"}, CleanupOptions{KeepReleases: 2})
	runScript(t, cmd.Script)

	got := strings.TrimSpace(readFile(t, dockerLog))
	for _, want := range []string{
		"image rm ghcr.io/example/demo-web:20260629T120000Z",
		"image rm ghcr.io/example/demo-web:20260628T120000Z",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("docker log missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{
		"ghcr.io/example/demo-web:20260701T120000Z",
		"ghcr.io/example/demo-web:20260630T120000Z",
		"ghcr.io/example/demo-web:latest",
		"ghcr.io/example/demo-api:20260628T120000Z",
		"ghcr.io/example/other:20260628T120000Z",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("docker log should not include %q:\n%s", forbidden, got)
		}
	}
}

func TestCleanupServicePrunesNanosecondReleaseImageTags(t *testing.T) {
	root := t.TempDir()
	writeState(t, root, "web", `{
  "release": "20260701T120000.111111111Z",
  "image": "ghcr.io/example/demo-web:20260701T120000.111111111Z",
  "previous_release": "20260630T120000.222222222Z",
  "previous_image": "ghcr.io/example/demo-web:20260630T120000.222222222Z"
}`)
	imageList := strings.Join([]string{
		"ghcr.io/example/demo-web:20260701T120000.111111111Z",
		"ghcr.io/example/demo-web:20260630T120000.222222222Z",
		"ghcr.io/example/demo-web:20260629T120000.333333333Z",
	}, "\n")
	dockerLog := fakeDockerImages(t, imageList)

	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	cmd := docker.CleanupService(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web", config.Service{Image: "ghcr.io/example/demo-web"}, CleanupOptions{KeepReleases: 2})
	runScript(t, cmd.Script)

	got := strings.TrimSpace(readFile(t, dockerLog))
	if !strings.Contains(got, "image rm ghcr.io/example/demo-web:20260629T120000.333333333Z") {
		t.Fatalf("docker log missing nanosecond release image cleanup:\n%s", got)
	}
	for _, forbidden := range []string{
		"ghcr.io/example/demo-web:20260701T120000.111111111Z",
		"ghcr.io/example/demo-web:20260630T120000.222222222Z",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("docker log should not include active/previous image %q:\n%s", forbidden, got)
		}
	}
}

func TestStartPreviousScriptReadsPreviousColorAndStartsContainer(t *testing.T) {
	root := t.TempDir()
	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	writeState(t, root, "worker", `{"previous_color":"blue"}`)
	dockerLog := fakeDocker(t)

	cmd := docker.StartPrevious(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "worker", config.Service{})
	runScript(t, cmd.Script)

	got := strings.TrimSpace(readFile(t, dockerLog))
	if got != "start demo_production_worker_blue" {
		t.Fatalf("docker command = %q, want start demo_production_worker_blue\nscript:\n%s", got, cmd.Script)
	}
	if strings.Contains(cmd.Script, "jq") {
		t.Fatalf("start previous script should not require jq:\n%s", cmd.Script)
	}
}

func TestRollbackStateScriptSwapsActiveAndPreviousReleaseMetadata(t *testing.T) {
	root := t.TempDir()
	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	writeState(t, root, "web", `{
  "release": "20260630T120000Z",
  "color": "green",
  "image": "ghcr.io/example/app:20260630T120000Z",
  "previous_release": "20260629T120000Z",
  "previous_color": "blue",
  "previous_image": "ghcr.io/example/app:20260629T120000Z"
}`)

	cmd := docker.RollbackState(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "web")
	runScript(t, cmd.Script)

	got := readFile(t, filepath.Join(root, "state", "web.json"))
	for _, want := range []string{
		`"release":"20260629T120000Z"`,
		`"color":"blue"`,
		`"image":"ghcr.io/example/app:20260629T120000Z"`,
		`"previous_release":"20260630T120000Z"`,
		`"previous_color":"green"`,
		`"previous_image":"ghcr.io/example/app:20260630T120000Z"`,
		`"updated_at":"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rollback state missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(cmd.Script, "jq") {
		t.Fatalf("rollback state script should not require jq:\n%s", cmd.Script)
	}
}

func TestStopPreviousScriptReadsPreviousColorAndStopsWithoutRemoving(t *testing.T) {
	root := t.TempDir()
	docker := Docker{Project: "demo", EnvName: "production", Root: root, Network: "demo_production"}
	writeState(t, root, "worker", `{"previous_color":"green"}`)
	dockerLog := fakeDocker(t)

	cmd := docker.StopPrevious(config.Server{Host: "203.0.113.10", SSHUser: "root", SSHPort: 22}, "worker")
	runScript(t, cmd.Script)

	got := strings.TrimSpace(readFile(t, dockerLog))
	if got != "inspect demo_production_worker_green\nstop demo_production_worker_green" {
		t.Fatalf("docker commands = %q, want inspect+stop for green\nscript:\n%s", got, cmd.Script)
	}
	if strings.Contains(cmd.Script, "docker rm") {
		t.Fatalf("stop previous must not remove containers:\n%s", cmd.Script)
	}
	if strings.Contains(cmd.Script, "jq") {
		t.Fatalf("stop previous script should not require jq:\n%s", cmd.Script)
	}
}

func writeState(t *testing.T, root string, serviceName string, text string) {
	t.Helper()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("create state dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, serviceName+".json"), []byte(text), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func fakeDocker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	dockerPath := filepath.Join(dir, "docker")
	script := "#!/bin/sh\nprintf '%s %s\\n' \"$1\" \"$2\" >> " + shell(logPath) + "\nexit 0\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func runScript(t *testing.T, script string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run script: %v\n%s\nscript:\n%s", err, string(output), script)
	}
}

func runScriptExpectFailure(t *testing.T, script string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("script unexpectedly succeeded\n%s\nscript:\n%s", string(output), script)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func replaceMetadataLine(metadata string, key string, value string) string {
	lines := strings.Split(metadata, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key+"=") {
			lines[i] = key + "=" + value
		}
	}
	return strings.Join(lines, "\n")
}

func fakeDockerImages(t *testing.T, imageList string) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "docker.log")
	dockerPath := filepath.Join(dir, "docker")
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatalf("create fake docker log: %v", err)
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = image ] && [ \"$2\" = ls ]; then\n" +
		"  cat <<'IMAGES'\n" + imageList + "\nIMAGES\n" +
		"  exit 0\n" +
		"fi\n" +
		"printf '%s %s %s\\n' \"$1\" \"$2\" \"$3\" >> " + shell(logPath) + "\n" +
		"if [ \"$1\" = image ] && [ \"$2\" = rm ]; then exit 1; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}
