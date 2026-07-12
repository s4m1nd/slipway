package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/planner"
	"github.com/s4m1nd/slipway/internal/remote"
)

func TestInitAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"validate", "-c", path, "--env", "production"}, &out, &errOut); code != 0 {
		t.Fatalf("validate code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "is valid") {
		t.Fatalf("unexpected validate output: %s", out.String())
	}
}

func TestDeployDryRunPrintsPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"deploy", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("deploy code=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Deploy demo/production\nRelease ") || !strings.Contains(out.String(), "switch Caddy route") {
		t.Fatalf("unexpected deploy output: %s", out.String())
	}
	if got := strings.Count(out.String(), "switch Caddy routes"); got != 1 {
		t.Fatalf("Caddy switch count = %d, want 1; output:\n%s", got, out.String())
	}
	if strings.Contains(out.String(), "plan only") {
		t.Fatalf("unexpected skeleton output: %s", out.String())
	}
}

func TestStatusDryRunPrintsInspectionCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"status", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("status code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Status demo/production", "inspect status for web", "inspect status for worker", "203.0.113.10", "203.0.113.11"} {
		if !strings.Contains(got, want) {
			t.Fatalf("status dry-run output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "plan only") {
		t.Fatalf("unexpected skeleton output: %s", got)
	}
}

func TestStatusDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	config := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"status", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("status code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("status unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestSyncProxyDryRunPrintsProxyOnlyPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"sync-proxy", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("sync-proxy code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Sync proxy routes demo/production", "sync Caddy routes", "app.example.com", `json_field color "$STATE"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sync-proxy dry-run output missing %q:\n%s", want, got)
		}
	}
	for _, forbidden := range []string{"build image", "push image", "log in to registry", "upload env", "start inactive", "wait for web health", "record active", "cleanup old"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sync-proxy dry-run included deploy-only work %q:\n%s", forbidden, got)
		}
	}
}

func TestSyncProxyDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	config := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"sync-proxy", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("sync-proxy code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("sync-proxy unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestRollbackDryRunPrintsPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"rollback", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("rollback code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Rollback demo/production", "start previous web release", "start previous worker release", "rollback state for web", "rollback state for worker", "switch Caddy routes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rollback dry-run output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "build image") || strings.Contains(got, "push image") || strings.Contains(got, "upload env") {
		t.Fatalf("rollback dry-run included deploy-only work:\n%s", got)
	}
}

func TestRollbackDryRunIncludesServiceNamesAndHostIdentifiers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"rollback", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("rollback code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"web", "worker", "root@203.0.113.10", "root@203.0.113.11"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rollback dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestRollbackDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	config := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"rollback", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("rollback code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("rollback unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestExecuteRollbackInspectsReadinessWhileLockIsHeld(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(exampleConfig))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := planner.New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runner := &recordingRollbackRunner{}
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := executeRollback(p, time.Minute, runner, &out, &errOut)
	if code != 0 {
		t.Fatalf("executeRollback code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}

	events := strings.Join(runner.events, "\n")
	lastAcquire := lastEventIndex(runner.events, "run:acquire deploy lock")
	firstInspect := firstEventIndex(runner.events, "output:inspect status")
	firstMutation := firstEventIndex(runner.events, "run:start previous")
	firstRelease := firstEventIndex(runner.events, "run:release deploy lock")
	for name, index := range map[string]int{
		"acquire":       lastAcquire,
		"inspect":       firstInspect,
		"rollback work": firstMutation,
		"release":       firstRelease,
	} {
		if index == -1 {
			t.Fatalf("%s event missing:\n%s", name, events)
		}
	}
	if !(lastAcquire < firstInspect && firstInspect < firstMutation && firstMutation < firstRelease) {
		t.Fatalf("rollback should inspect readiness inside the lock before mutation:\n%s", events)
	}
	for _, want := range []string{"Rollback complete", "active:", "blue", "release: 20260630T120000Z", "elapsed:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("rollback summary missing %q:\n%s", want, out.String())
		}
	}
}

func TestExecuteDeployPrintsFinalStatusSummary(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(exampleConfig))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := planner.New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	plan := remote.Plan{
		Title:    "Deploy demo/production",
		Subtitle: "Release planned-release",
		Commands: []remote.Command{{Description: "wait for web health check"}},
	}
	runner := &recordingRollbackRunner{}
	var out bytes.Buffer
	var errOut bytes.Buffer

	if code := executeDeployWithRunner(p, plan, runner, false, &out, &errOut); code != 0 {
		t.Fatalf("executeDeploy code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	for _, want := range []string{"Deploy demo/production", "Release planned-release", "✓ healthy", "Deployment complete", "active:", "green", "release: 20260701T120000Z", "elapsed:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("deploy summary missing %q:\n%s", want, out.String())
		}
	}
}

func TestExecuteRollbackPrintsContinuousCommandNumbers(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(exampleConfig))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := planner.New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	runner := &recordingRollbackRunner{}
	var out bytes.Buffer
	var errOut bytes.Buffer

	code := executeRollback(p, time.Minute, runner, &out, &errOut)
	if code != 0 {
		t.Fatalf("executeRollback code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}

	numbers := commandNumbers(out.String())
	if len(numbers) == 0 {
		t.Fatalf("rollback output had no numbered commands:\n%s", out.String())
	}
	for i, got := range numbers {
		want := i + 1
		if got != want {
			t.Fatalf("rollback command numbers = %v, want continuous sequence starting at 1; output:\n%s", numbers, out.String())
		}
	}
}

func TestRollbackEnvFlagBehaviorMatchesOtherCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"rollback", "-c", path, "--dry-run"}, &out, &errOut); code != 2 {
		t.Fatalf("rollback missing env code=%d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--env is required") {
		t.Fatalf("missing env error should match other commands: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"rollback", "-c", path, "--env", "staging", "--dry-run"}, &out, &errOut); code != 1 {
		t.Fatalf("rollback unknown env code=%d, want 1; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), `environment "staging" was not found`) {
		t.Fatalf("unknown env error should match other commands: %s", errOut.String())
	}
}

func commandNumbers(output string) []int {
	var numbers []int
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		label := strings.TrimSuffix(fields[0], ".")
		label, _, _ = strings.Cut(label, "/")
		n, err := strconv.Atoi(label)
		if err != nil {
			continue
		}
		numbers = append(numbers, n)
	}
	return numbers
}

func TestCleanupDryRunPrintsPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"cleanup", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("cleanup code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Cleanup demo/production", "cleanup old web release artifacts", "cleanup old worker release artifacts", "KEEP_RELEASES=5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cleanup dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestCleanupDryRunIncludesServiceNamesAndHostIdentifiers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"cleanup", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("cleanup code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"web", "worker", "root@203.0.113.10", "root@203.0.113.11"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cleanup dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestCleanupDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	config := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"cleanup", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("cleanup code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("cleanup unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestMutatingDryRunsIncludeDeployLocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	cases := []struct {
		name string
		args []string
	}{
		{name: "deploy", args: []string{"deploy", "-c", path, "--env", "production", "--dry-run", "--lock-timeout", "42s"}},
		{name: "rollback", args: []string{"rollback", "-c", path, "--env", "production", "--dry-run", "--lock-timeout", "42s"}},
		{name: "sync-proxy", args: []string{"sync-proxy", "-c", path, "--env", "production", "--dry-run", "--lock-timeout", "42s"}},
		{name: "cleanup", args: []string{"cleanup", "-c", path, "--env", "production", "--dry-run", "--lock-timeout", "42s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			errOut.Reset()
			if code := Execute(tc.args, &out, &errOut); code != 0 {
				t.Fatalf("%s code=%d stderr=%s", tc.name, code, errOut.String())
			}
			got := out.String()
			for _, want := range []string{"acquire deploy lock", "release deploy lock", "LOCK_TIMEOUT_SECONDS=42", "locks/deploy.lock"} {
				if !strings.Contains(got, want) {
					t.Fatalf("%s dry-run output missing %q:\n%s", tc.name, want, got)
				}
			}
		})
	}
}

func TestReadOnlyDryRunsDoNotIncludeDeployLocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	cases := []struct {
		name string
		args []string
	}{
		{name: "status", args: []string{"status", "-c", path, "--env", "production", "--dry-run"}},
		{name: "logs", args: []string{"logs", "-c", path, "--env", "production", "--service", "web", "--dry-run"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			errOut.Reset()
			if code := Execute(tc.args, &out, &errOut); code != 0 {
				t.Fatalf("%s code=%d stderr=%s", tc.name, code, errOut.String())
			}
			if strings.Contains(out.String(), "deploy.lock") || strings.Contains(out.String(), "deploy lock") {
				t.Fatalf("%s dry-run should not include locks:\n%s", tc.name, out.String())
			}
		})
	}
}

func TestCleanupEnvFlagBehaviorMatchesOtherCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"cleanup", "-c", path, "--dry-run"}, &out, &errOut); code != 2 {
		t.Fatalf("cleanup missing env code=%d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--env is required") {
		t.Fatalf("missing env error should match other commands: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"cleanup", "-c", path, "--env", "staging", "--dry-run"}, &out, &errOut); code != 1 {
		t.Fatalf("cleanup unknown env code=%d, want 1; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), `environment "staging" was not found`) {
		t.Fatalf("unknown env error should match other commands: %s", errOut.String())
	}
}

func TestHelpIncludesEveryMVPCommand(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"help"}, &out, &errOut); code != 0 {
		t.Fatalf("help code=%d stderr=%s", code, errOut.String())
	}
	for _, want := range []string{
		"slipway init [-c slipway.yml] [--force]",
		"slipway validate -c slipway.yml",
		"slipway provision -c slipway.yml --env production [--dry-run]",
		"slipway deploy -c slipway.yml --env production [--dry-run]",
		"slipway rollback -c slipway.yml --env production [--dry-run]",
		"slipway status -c slipway.yml --env production [--dry-run]",
		"slipway sync-proxy -c slipway.yml --env production [--dry-run]",
		"slipway cleanup -c slipway.yml --env production [--dry-run]",
		"slipway logs -c slipway.yml --env production --service web",
		"slipway secrets exec -c slipway.yml --secret NAME",
		"slipway version",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help output missing %q:\n%s", want, out.String())
		}
	}
}

func TestVersionPrintsBuildMetadata(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("version code=%d stderr=%s", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("version wrote stderr: %s", errOut.String())
	}

	want := strings.Join([]string{
		"Slipway version dev",
		"Commit: unknown",
		"Date: unknown",
		"Go: " + runtime.Version(),
		"",
	}, "\n")
	if out.String() != want {
		t.Fatalf("version output mismatch:\nwant:\n%s\ngot:\n%s", want, out.String())
	}
}

func TestEnvScopedCommandsRequireEnvAndReportUnknownEnvConsistently(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	for _, command := range []string{"provision", "deploy", "status", "rollback", "sync-proxy", "cleanup"} {
		t.Run(command+" missing env", func(t *testing.T) {
			out.Reset()
			errOut.Reset()
			if code := Execute([]string{command, "-c", path, "--dry-run"}, &out, &errOut); code != 2 {
				t.Fatalf("%s missing env code=%d, want 2; stderr=%s", command, code, errOut.String())
			}
			if !strings.Contains(errOut.String(), "--env is required") {
				t.Fatalf("%s missing env error should be clear: %s", command, errOut.String())
			}
		})

		t.Run(command+" unknown env", func(t *testing.T) {
			out.Reset()
			errOut.Reset()
			if code := Execute([]string{command, "-c", path, "--env", "staging", "--dry-run"}, &out, &errOut); code != 1 {
				t.Fatalf("%s unknown env code=%d, want 1; stderr=%s", command, code, errOut.String())
			}
			if !strings.Contains(errOut.String(), `environment "staging" was not found`) {
				t.Fatalf("%s unknown env error should name environment: %s", command, errOut.String())
			}
		})
	}
}

func TestDeployDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	config := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"deploy", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("deploy code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("deploy dry-run unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestLogsDryRunPrintsPlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--service", "web", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("logs code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"Logs demo/production service web", "logs for web active", "root@203.0.113.10", "docker logs --tail 100"} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs dry-run output missing %q:\n%s", want, got)
		}
	}
}

func TestLogsDryRunAcceptsColorPrevious(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--service", "web", "--color", "previous", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("logs code=%d stderr=%s", code, errOut.String())
	}
	if got := out.String(); !strings.Contains(got, "logs for web previous") || !strings.Contains(got, "previous_color") {
		t.Fatalf("logs previous output missing expected content:\n%s", got)
	}
}

func TestLogsDryRunAcceptsTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--service", "web", "--tail", "50", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("logs code=%d stderr=%s", code, errOut.String())
	}
	if got := out.String(); !strings.Contains(got, "docker logs --tail 50") {
		t.Fatalf("logs tail output missing tail 50:\n%s", got)
	}
}

func TestLogsDryRunAcceptsHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--service", "web", "--host", "app-1", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("logs code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	if !strings.Contains(got, "root@203.0.113.10") {
		t.Fatalf("logs host output missing app-1 host:\n%s", got)
	}
	if strings.Contains(got, "203.0.113.11") {
		t.Fatalf("logs host output included unrelated host:\n%s", got)
	}
}

func TestLogsDryRunDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	configText := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: exit 42\n  names:", 1)
	if err := os.WriteFile(path, []byte(configText), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--service", "web", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("logs code=%d stderr=%s", code, errOut.String())
	}
	if strings.Contains(errOut.String(), "secret") {
		t.Fatalf("logs unexpectedly resolved secrets: %s", errOut.String())
	}
}

func TestSecretsExecInjectsOnlySelectedSecretsIntoChildEnv(t *testing.T) {
	dir := t.TempDir()
	requestedPath := filepath.Join(dir, "requested-secrets")
	fetchPath := filepath.Join(dir, "fetch-secrets.sh")
	writeExecutable(t, fetchPath, `#!/bin/sh
printf '%s' "$SLIPWAY_SECRET_NAMES" > `+shellQuoteForTest(requestedPath)+`
case ",$SLIPWAY_SECRET_NAMES," in
  *,HCLOUD_TOKEN,*) printf 'HCLOUD_TOKEN=hcloud-secret\n' ;;
esac
case ",$SLIPWAY_SECRET_NAMES," in
  *,DATABASE_URL,*) printf 'DATABASE_URL=db-secret\n' ;;
esac
case ",$SLIPWAY_SECRET_NAMES," in
  *,REGISTRY_PASSWORD,*) printf 'REGISTRY_PASSWORD=registry-secret\n' ;;
esac
`)
	path := filepath.Join(dir, "slipway.yml")
	if err := os.WriteFile(path, []byte(configWithFetchAndHCLOUD(fetchPath)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := Execute([]string{
		"secrets", "exec", "-c", path, "--secret", "HCLOUD_TOKEN", "--",
		"sh", "-c", `printf 'token=%s db=%s\n' "$HCLOUD_TOKEN" "${DATABASE_URL-unset}"`,
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("secrets exec code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if got := out.String(); got != "token=hcloud-secret db=unset\n" {
		t.Fatalf("child env output = %q", got)
	}
	if got := readTestFile(t, requestedPath); got != "HCLOUD_TOKEN" {
		t.Fatalf("requested secrets = %q, want HCLOUD_TOKEN", got)
	}
}

func TestSecretsExecDryRunRedactsCommandAndDoesNotResolveSecrets(t *testing.T) {
	dir := t.TempDir()
	fetchPath := filepath.Join(dir, "fetch-secrets.sh")
	writeExecutable(t, fetchPath, `#!/bin/sh
printf 'super-secret-token\n' >&2
exit 42
`)
	path := filepath.Join(dir, "slipway.yml")
	if err := os.WriteFile(path, []byte(configWithFetchAndHCLOUD(fetchPath)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := Execute([]string{
		"secrets", "exec", "-c", path, "--secret", "HCLOUD_TOKEN", "--dry-run", "--",
		"terraform", "-chdir=examples/terraform/hetzner-single-node", "apply",
	}, &out, &errOut)
	if code != 0 {
		t.Fatalf("secrets exec dry-run code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Secrets exec") || !strings.Contains(got, "<sensitive command redacted>") {
		t.Fatalf("dry-run output should show a redacted plan:\n%s", got)
	}
	for _, forbidden := range []string{"super-secret-token", "HCLOUD_TOKEN", "terraform"} {
		if strings.Contains(got, forbidden) || strings.Contains(errOut.String(), forbidden) {
			t.Fatalf("dry-run leaked %q\nstdout:\n%s\nstderr:\n%s", forbidden, got, errOut.String())
		}
	}
}

func TestDeployResolvesOnlySecretsNeededBySelectedEnvironment(t *testing.T) {
	dir := t.TempDir()
	requestedPath := filepath.Join(dir, "deploy-requested-secrets")
	fetchPath := filepath.Join(dir, "fetch-secrets.sh")
	writeExecutable(t, fetchPath, `#!/bin/sh
printf '%s' "$SLIPWAY_SECRET_NAMES" > `+shellQuoteForTest(requestedPath)+`
case ",$SLIPWAY_SECRET_NAMES," in
  *,HCLOUD_TOKEN,*)
    echo "HCLOUD_TOKEN should not be resolved during deploy" >&2
    exit 42
    ;;
esac
printf 'REGISTRY_PASSWORD=registry-secret\n'
printf 'DATABASE_URL=postgres://example\n'
printf 'REDIS_URL=redis://example\n'
`)
	path := filepath.Join(dir, "slipway.yml")
	if err := os.WriteFile(path, []byte(configWithFetchAndHCLOUD(fetchPath)), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fakeBin := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeBin, 0o755); err != nil {
		t.Fatalf("mkdir fake bin: %v", err)
	}
	writeExecutable(t, filepath.Join(fakeBin, "docker"), `#!/bin/sh
if [ "$1" = "login" ]; then
  cat >/dev/null
fi
exit 0
`)
	writeExecutable(t, filepath.Join(fakeBin, "ssh"), `#!/bin/sh
exit 0
`)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := Execute([]string{"deploy", "-c", path, "--env", "production"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("deploy code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	if got := out.String(); !strings.Contains(got, "1/") || !strings.Contains(got, "✓ complete") || strings.Contains(got, "run: docker") {
		t.Fatalf("normal deploy output should be concise:\n%s", got)
	}
	requested := strings.Split(readTestFile(t, requestedPath), ",")
	if stringSliceContains(requested, "HCLOUD_TOKEN") {
		t.Fatalf("deploy requested HCLOUD_TOKEN: %v", requested)
	}
	for _, want := range []string{"REGISTRY_PASSWORD", "DATABASE_URL", "REDIS_URL"} {
		if !stringSliceContains(requested, want) {
			t.Fatalf("deploy requested secrets %v, missing %s", requested, want)
		}
	}

	out.Reset()
	errOut.Reset()
	code = Execute([]string{"deploy", "-c", path, "--env", "production", "--verbose"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("verbose deploy code=%d stderr=%s stdout=%s", code, errOut.String(), out.String())
	}
	got := out.String()
	if !strings.Contains(got, "run: docker build") || !strings.Contains(got, "run: <sensitive command redacted>") {
		t.Fatalf("verbose deploy output missing safe command details:\n%s", got)
	}
	for _, secret := range []string{"registry-secret", "postgres://example", "redis://example"} {
		if strings.Contains(got, secret) || strings.Contains(errOut.String(), secret) {
			t.Fatalf("verbose deploy output leaked secret %q", secret)
		}
	}
}

func TestLogsWithoutServiceFailsClearly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "production", "--dry-run"}, &out, &errOut); code != 2 {
		t.Fatalf("logs missing service code=%d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--service is required") {
		t.Fatalf("missing service error was not clear: %s", errOut.String())
	}
}

func TestLogsEnvFlagBehaviorMatchesOtherCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slipway.yml")
	var out bytes.Buffer
	var errOut bytes.Buffer
	if code := Execute([]string{"init", "-c", path}, &out, &errOut); code != 0 {
		t.Fatalf("init code=%d stderr=%s", code, errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--service", "web", "--dry-run"}, &out, &errOut); code != 2 {
		t.Fatalf("logs missing env code=%d, want 2; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "--env is required") {
		t.Fatalf("missing env error should match other commands: %s", errOut.String())
	}

	out.Reset()
	errOut.Reset()
	if code := Execute([]string{"logs", "-c", path, "--env", "staging", "--service", "web", "--dry-run"}, &out, &errOut); code != 1 {
		t.Fatalf("logs unknown env code=%d, want 1; stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), `environment "staging" was not found`) {
		t.Fatalf("unknown env error should match other commands: %s", errOut.String())
	}
}

func TestExecuteLogsPrintsHeadersForMultipleTargetsWithoutDumpingScripts(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(exampleConfig))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := planner.New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	service := p.Env.Services["web"]
	service.Hosts = []string{"app-1", "worker-1"}
	p.Env.Services["web"] = service

	var out bytes.Buffer
	var errOut bytes.Buffer
	runner := &recordingLogsRunner{}
	code := executeLogs(p, planner.LogsOptions{ServiceName: "web", Color: "active", Tail: 100}, runner, &out, &errOut)
	if code != 0 {
		t.Fatalf("executeLogs code=%d stderr=%s", code, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"==> web on app-1/root@203.0.113.10 active <==", "==> web on worker-1/root@203.0.113.11 active <=="} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs output missing header %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "docker logs") || strings.Contains(got, "json_field") {
		t.Fatalf("logs execution dumped generated script:\n%s", got)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("runner command count = %d, want 2", len(runner.commands))
	}
}

type recordingLogsRunner struct {
	commands []remote.Command
}

func configWithFetchAndHCLOUD(fetchPath string) string {
	configText := strings.Replace(exampleConfig, "secrets:\n  names:", "secrets:\n  fetch: sh "+shellQuoteForTest(fetchPath)+"\n  names:", 1)
	return strings.Replace(configText, "    - REGISTRY_PASSWORD\n", "    - REGISTRY_PASSWORD\n    - HCLOUD_TOKEN\n", 1)
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (r *recordingLogsRunner) Run(_ context.Context, command remote.Command) error {
	r.commands = append(r.commands, command)
	return nil
}

type recordingRollbackRunner struct {
	events []string
}

func (r *recordingRollbackRunner) Run(_ context.Context, command remote.Command) error {
	r.events = append(r.events, "run:"+command.Description)
	return nil
}

func (r *recordingRollbackRunner) Output(_ context.Context, command remote.Command) (string, error) {
	r.events = append(r.events, "output:"+command.Description)
	service := "web"
	if strings.Contains(command.Description, "worker") {
		service = "worker"
	}
	return rollbackReadyStatusOutput(service), nil
}

func rollbackReadyStatusOutput(service string) string {
	return strings.Join([]string{
		"state=present",
		"project=demo",
		"environment=production",
		"service=" + service,
		"active_color=green",
		"active_release=20260701T120000Z",
		"active_image=ghcr.io/example/" + service + ":20260701T120000Z",
		"previous_color=blue",
		"previous_release=20260630T120000Z",
		"previous_image=ghcr.io/example/" + service + ":20260630T120000Z",
		"blue_exists=true",
		"blue_running=true",
		"blue_release=20260630T120000Z",
		"blue_image=ghcr.io/example/" + service + ":20260630T120000Z",
		"green_exists=true",
		"green_running=true",
		"green_release=20260701T120000Z",
		"green_image=ghcr.io/example/" + service + ":20260701T120000Z",
	}, "\n")
}

func firstEventIndex(events []string, contains string) int {
	for i, event := range events {
		if strings.Contains(event, contains) {
			return i
		}
	}
	return -1
}

func lastEventIndex(events []string, contains string) int {
	for i := len(events) - 1; i >= 0; i-- {
		if strings.Contains(events[i], contains) {
			return i
		}
	}
	return -1
}
