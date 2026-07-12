package planner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

func TestDeployPlanSwitchesCaddyOncePerProxyHost(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)
	if plan.Title != "Deploy demo/production" || !strings.HasPrefix(plan.Subtitle, "Release ") {
		t.Fatalf("deploy heading = %q / %q", plan.Title, plan.Subtitle)
	}

	var switches []remote.Command
	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "switch Caddy") {
			switches = append(switches, command)
		}
	}
	if len(switches) != 2 {
		t.Fatalf("Caddy switch count = %d, want 2; descriptions=%v", len(switches), commandDescriptions(plan.Commands))
	}

	seenHosts := map[string]bool{}
	for _, command := range switches {
		if seenHosts[command.Host] {
			t.Fatalf("host %s had more than one Caddy switch", command.Host)
		}
		seenHosts[command.Host] = true
	}
}

func TestDeployPlanOrdersBlueGreenPhases(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)

	firstRemote := firstCommandIndex(plan.Commands, func(command remote.Command) bool {
		return command.Host != "" && (strings.Contains(command.Description, "log in to registry") ||
			strings.Contains(command.Description, "upload env") ||
			strings.Contains(command.Description, "start inactive"))
	})
	firstHealth := firstCommandIndex(plan.Commands, descriptionContains("wait for "))
	firstSwitch := firstCommandIndex(plan.Commands, descriptionContains("switch Caddy"))
	firstRecord := firstCommandIndex(plan.Commands, descriptionContains("record active"))
	lastBuildPush := lastCommandIndex(plan.Commands, func(command remote.Command) bool {
		return strings.Contains(command.Description, "build image") || strings.Contains(command.Description, "push image")
	})
	lastRemoteStart := lastCommandIndex(plan.Commands, func(command remote.Command) bool {
		return command.Host != "" && (strings.Contains(command.Description, "log in to registry") ||
			strings.Contains(command.Description, "upload env") ||
			strings.Contains(command.Description, "start inactive"))
	})
	lastHealth := lastCommandIndex(plan.Commands, descriptionContains("wait for "))
	lastSwitch := lastCommandIndex(plan.Commands, descriptionContains("switch Caddy"))

	for name, index := range map[string]int{
		"first remote login/upload/start": firstRemote,
		"first health check":              firstHealth,
		"first Caddy switch":              firstSwitch,
		"first state record":              firstRecord,
		"last build/push":                 lastBuildPush,
		"last remote login/upload/start":  lastRemoteStart,
		"last health check":               lastHealth,
		"last Caddy switch":               lastSwitch,
	} {
		if index == -1 {
			t.Fatalf("%s was not found; descriptions=%v", name, commandDescriptions(plan.Commands))
		}
	}

	if !(lastBuildPush < firstRemote) {
		t.Fatalf("build/push must finish before remote login/upload/start; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastRemoteStart < firstHealth) {
		t.Fatalf("remote login/upload/start must finish before health checks; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastHealth < firstSwitch) {
		t.Fatalf("health checks must finish before Caddy switches; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastSwitch < firstRecord) {
		t.Fatalf("Caddy switches must finish before active state is recorded; descriptions=%v", commandDescriptions(plan.Commands))
	}
}

func TestWithDeployLockWrapsMutatingPlan(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.WithDeployLock(deployPlan(t, p), "deploy", 30*time.Minute)

	lastLocalBuildPush := lastCommandIndex(plan.Commands, func(command remote.Command) bool {
		return command.Host == "" && (strings.Contains(command.Description, "build image") || strings.Contains(command.Description, "push image"))
	})
	lastAcquire := lastCommandIndex(plan.Commands, descriptionContains("acquire deploy lock"))
	firstRemoteMutation := firstCommandIndex(plan.Commands, func(command remote.Command) bool {
		return command.Host != "" && !strings.Contains(command.Description, "acquire deploy lock")
	})
	if lastLocalBuildPush == -1 || lastAcquire == -1 || firstRemoteMutation == -1 {
		t.Fatalf("lock or deploy command not found; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastLocalBuildPush < lastAcquire && lastAcquire < firstRemoteMutation) {
		t.Fatalf("deploy locks must be acquired after local build/push and before remote mutation; descriptions=%v", commandDescriptions(plan.Commands))
	}

	if got := countCommandDescriptions(plan.Commands, "acquire deploy lock"); got != 2 {
		t.Fatalf("acquire lock count = %d, want 2; descriptions=%v", got, commandDescriptions(plan.Commands))
	}
	if got := countCommandDescriptions(plan.Commands, "release deploy lock"); got != 2 {
		t.Fatalf("release lock count = %d, want 2; descriptions=%v", got, commandDescriptions(plan.Commands))
	}

	for _, command := range commandsMatching(plan.Commands, descriptionContains("acquire deploy lock")) {
		for _, want := range []string{
			`LOCK_DIR='/opt/slipway/demo/production/locks/deploy.lock'`,
			"LOCK_TIMEOUT_SECONDS=1800",
			"command=deploy",
			`mkdir "$LOCK_DIR"`,
		} {
			if !strings.Contains(command.Script, want) {
				t.Fatalf("acquire lock command missing %q:\n%s", want, command.Script)
			}
		}
	}

	for _, command := range plan.Commands[len(plan.Commands)-2:] {
		if !strings.Contains(command.Description, "release deploy lock") {
			t.Fatalf("plan should end with lock releases; descriptions=%v", commandDescriptions(plan.Commands))
		}
		if !command.Always {
			t.Fatalf("release lock command must run after failures where possible: %#v", command)
		}
		if command.RunIfSucceeded == "" {
			t.Fatalf("release lock command should be tied to a successful acquire: %#v", command)
		}
		if !strings.Contains(command.Script, `rm -rf -- "$LOCK_DIR"`) {
			t.Fatalf("release lock command should remove lock dir only after owner check:\n%s", command.Script)
		}
	}
}

func TestDeployPlanFiltersCaddyRoutesPerHost(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)

	var appSwitch, apiSwitch remote.Command
	for _, command := range plan.Commands {
		if !strings.Contains(command.Description, "switch Caddy") {
			continue
		}
		switch command.Host {
		case "203.0.113.10":
			appSwitch = command
		case "203.0.113.11":
			apiSwitch = command
		}
	}
	if appSwitch.Script == "" || apiSwitch.Script == "" {
		t.Fatalf("expected switches for both proxy hosts, got app=%q api=%q", appSwitch.Script, apiSwitch.Script)
	}
	if !strings.Contains(appSwitch.Script, "app.example.com") || strings.Contains(appSwitch.Script, "api.example.com") {
		t.Fatalf("app host Caddyfile was not filtered to app-local routes:\n%s", appSwitch.Script)
	}
	if !strings.Contains(apiSwitch.Script, "api.example.com") || strings.Contains(apiSwitch.Script, "app.example.com") {
		t.Fatalf("api host Caddyfile was not filtered to api-local routes:\n%s", apiSwitch.Script)
	}
}

func TestDeployPlanDoesNotLeakSecretValuesInPlanOrExecutorOutput(t *testing.T) {
	p := newTestPlanner(t).WithSecrets(map[string]string{
		"REGISTRY_PASSWORD": "registry-password-value",
		"DATABASE_URL":      "postgres://secret-value",
		"REDIS_URL":         "redis://secret-value",
	})
	plan := deployPlan(t, p)

	var printed strings.Builder
	plan.Print(&printed)
	assertNoSecretValues(t, printed.String())

	var executed strings.Builder
	if err := remote.Execute(context.Background(), plan, noOpRunner{}, &executed); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	assertNoSecretValues(t, executed.String())
}

func TestDeployPlanRejectsMultilineEnvFileValues(t *testing.T) {
	p := newTestPlanner(t).WithSecrets(map[string]string{
		"REGISTRY_PASSWORD": "registry-password-value",
		"DATABASE_URL":      "postgres://secret-value\nmalicious-extra-line",
		"REDIS_URL":         "redis://secret-value",
	})

	_, err := p.Deploy(fixedDeployTime())
	if err == nil {
		t.Fatal("expected multiline secret value to fail")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected newline error naming DATABASE_URL, got: %v", err)
	}
	if strings.Contains(err.Error(), "postgres://secret-value") {
		t.Fatalf("error leaked secret value: %v", err)
	}
}

func TestProvisionPlanDoesNotStartCaddyWhenNoProxyRoutesAreConfigured(t *testing.T) {
	p := newTestPlanner(t)
	p.Env.Proxy.Routes = nil

	plan := p.Provision()
	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "Caddy") || strings.Contains(command.Script, "caddy:") {
			t.Fatalf("provision without proxy routes should not include Caddy command: %#v", command)
		}
	}
}

func TestDeployPlanDoesNotSwitchCaddyWhenNoProxyRoutesAreConfigured(t *testing.T) {
	p := newTestPlanner(t)
	p.Env.Proxy.Routes = nil

	plan := deployPlan(t, p)
	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "switch Caddy") || strings.Contains(command.Script, "Caddyfile") {
			t.Fatalf("deploy without proxy routes should not include Caddy switch: %#v", command)
		}
	}
}

func TestSyncProxyPlanSwitchesOnlyProxyRoutesUsingActiveState(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.SyncProxy()

	if plan.Title != "Sync proxy routes demo/production" {
		t.Fatalf("title = %q", plan.Title)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("command count = %d, want 2; descriptions=%v", len(plan.Commands), commandDescriptions(plan.Commands))
	}
	for _, command := range plan.Commands {
		if !strings.Contains(command.Description, "sync Caddy routes") {
			t.Fatalf("unexpected sync proxy command: %#v", command)
		}
		if !strings.Contains(command.Script, `json_field color "$STATE"`) {
			t.Fatalf("sync proxy command should read active color from state:\n%s", command.Script)
		}
		for _, forbidden := range []string{"build image", "push image", "log in to registry", "upload env", "start inactive", "wait for ", "record active", "rollback state", "cleanup old"} {
			if strings.Contains(command.Description, forbidden) || strings.Contains(command.Script, forbidden) {
				t.Fatalf("sync proxy plan contains forbidden %q command: %#v", forbidden, command)
			}
		}
	}
}

func TestSyncProxyPlanDoesNotSwitchCaddyWhenNoProxyRoutesAreConfigured(t *testing.T) {
	p := newTestPlanner(t)
	p.Env.Proxy.Routes = nil

	plan := p.SyncProxy()
	if len(plan.Commands) != 0 {
		t.Fatalf("sync proxy without routes command count = %d, want 0; descriptions=%v", len(plan.Commands), commandDescriptions(plan.Commands))
	}
}

func TestRollbackPlanOrdersBlueGreenPhases(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	firstStart := firstCommandIndex(plan.Commands, descriptionContains("start previous"))
	firstHealth := firstCommandIndex(plan.Commands, descriptionContains("wait for "))
	firstSwitch := firstCommandIndex(plan.Commands, descriptionContains("switch Caddy"))
	firstRollbackState := firstCommandIndex(plan.Commands, descriptionContains("rollback state"))
	firstStop := firstCommandIndex(plan.Commands, descriptionContains("stop previous"))
	lastStart := lastCommandIndex(plan.Commands, descriptionContains("start previous"))
	lastHealth := lastCommandIndex(plan.Commands, descriptionContains("wait for "))
	lastSwitch := lastCommandIndex(plan.Commands, descriptionContains("switch Caddy"))
	lastRollbackState := lastCommandIndex(plan.Commands, descriptionContains("rollback state"))

	for name, index := range map[string]int{
		"first start previous": firstStart,
		"first health check":   firstHealth,
		"first Caddy switch":   firstSwitch,
		"first rollback state": firstRollbackState,
		"first stop previous":  firstStop,
		"last start previous":  lastStart,
		"last health check":    lastHealth,
		"last Caddy switch":    lastSwitch,
		"last rollback state":  lastRollbackState,
	} {
		if index == -1 {
			t.Fatalf("%s was not found; descriptions=%v", name, commandDescriptions(plan.Commands))
		}
	}

	if !(lastStart < firstHealth) {
		t.Fatalf("start previous must finish before health checks; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastHealth < firstSwitch) {
		t.Fatalf("health checks must finish before Caddy switches; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastSwitch < firstRollbackState) {
		t.Fatalf("Caddy switches must finish before rollback state; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(lastRollbackState < firstStop) {
		t.Fatalf("rollback state must finish before non-routed stops; descriptions=%v", commandDescriptions(plan.Commands))
	}
}

func TestRollbackPlanDoesNotBuildPushLoginOrUploadEnv(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	for _, command := range plan.Commands {
		for _, forbidden := range []string{"build image", "push image", "log in to registry", "upload env"} {
			if strings.Contains(command.Description, forbidden) {
				t.Fatalf("rollback plan contains forbidden %q command: %#v", forbidden, command)
			}
		}
	}
}

func TestRollbackPlanSwitchesCaddyOncePerProxyHost(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	var switches []remote.Command
	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "switch Caddy") {
			switches = append(switches, command)
		}
	}
	if len(switches) != 2 {
		t.Fatalf("Caddy switch count = %d, want 2; descriptions=%v", len(switches), commandDescriptions(plan.Commands))
	}

	seenHosts := map[string]bool{}
	for _, command := range switches {
		if seenHosts[command.Host] {
			t.Fatalf("host %s had more than one Caddy switch", command.Host)
		}
		seenHosts[command.Host] = true
	}
}

func TestRollbackPlanFiltersCaddyRoutesPerHost(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	var appSwitch, apiSwitch remote.Command
	for _, command := range plan.Commands {
		if !strings.Contains(command.Description, "switch Caddy") {
			continue
		}
		switch command.Host {
		case "203.0.113.10":
			appSwitch = command
		case "203.0.113.11":
			apiSwitch = command
		}
	}
	if appSwitch.Script == "" || apiSwitch.Script == "" {
		t.Fatalf("expected switches for both proxy hosts, got app=%q api=%q", appSwitch.Script, apiSwitch.Script)
	}
	if !strings.Contains(appSwitch.Script, "app.example.com") || strings.Contains(appSwitch.Script, "api.example.com") {
		t.Fatalf("app host rollback Caddyfile was not filtered to app-local routes:\n%s", appSwitch.Script)
	}
	if !strings.Contains(apiSwitch.Script, "api.example.com") || strings.Contains(apiSwitch.Script, "app.example.com") {
		t.Fatalf("api host rollback Caddyfile was not filtered to api-local routes:\n%s", apiSwitch.Script)
	}
}

func TestRollbackPlanDoesNotSwitchCaddyWhenNoProxyRoutesAreConfigured(t *testing.T) {
	p := newTestPlanner(t)
	p.Env.Proxy.Routes = nil

	plan := p.Rollback()
	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "switch Caddy") || strings.Contains(command.Script, "Caddyfile") {
			t.Fatalf("rollback without proxy routes should not include Caddy switch: %#v", command)
		}
	}
}

func TestDeployPlanStopsPreviousForNonRoutedServicesAfterRecordingState(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)

	recordWorker := firstCommandIndex(plan.Commands, descriptionContains("record active worker release"))
	stopWorker := firstCommandIndex(plan.Commands, descriptionContains("stop previous worker release"))
	if recordWorker == -1 || stopWorker == -1 {
		t.Fatalf("worker record/stop commands not found; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(recordWorker < stopWorker) {
		t.Fatalf("worker previous container must stop after active state is recorded; descriptions=%v", commandDescriptions(plan.Commands))
	}
}

func TestCleanupPlanTargetsEveryServiceHost(t *testing.T) {
	p := newTestPlanner(t)
	service := p.Env.Services["web"]
	service.Hosts = []string{"app-1", "api-1"}
	p.Env.Services["web"] = service

	plan := p.Cleanup()
	if plan.Title != "Cleanup demo/production" {
		t.Fatalf("cleanup title = %q", plan.Title)
	}
	if got, want := len(plan.Commands), 4; got != want {
		t.Fatalf("cleanup command count = %d, want %d; descriptions=%v", got, want, commandDescriptions(plan.Commands))
	}
	for _, want := range []string{
		"cleanup old api release artifacts",
		"cleanup old web release artifacts",
		"cleanup old worker release artifacts",
	} {
		if firstCommandIndex(plan.Commands, descriptionContains(want)) == -1 {
			t.Fatalf("cleanup plan missing %q; descriptions=%v", want, commandDescriptions(plan.Commands))
		}
	}
}

func TestCleanupPlanUsesEffectiveEnvironmentRetention(t *testing.T) {
	cfg, err := config.LoadBytes([]byte(strings.Replace(plannerConfigYAML, "  production:\n    servers:", "  production:\n    retention:\n      releases: 3\n    servers:", 1)))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	plan := p.Cleanup()
	if len(plan.Commands) == 0 {
		t.Fatal("cleanup plan had no commands")
	}
	for _, command := range plan.Commands {
		if !strings.Contains(command.Script, "KEEP_RELEASES=3") {
			t.Fatalf("cleanup command did not use environment retention:\n%s", command.Script)
		}
	}
}

func TestCleanupPlanDoesNotIncludeDeployRollbackStatusOrLogCommands(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Cleanup()

	for _, command := range plan.Commands {
		for _, forbidden := range []string{"build image", "push image", "log in to registry", "upload env", "start inactive", "wait for ", "switch Caddy", "record active", "rollback state", "docker logs", "inspect status"} {
			if strings.Contains(command.Description, forbidden) || strings.Contains(command.Script, forbidden) {
				t.Fatalf("cleanup plan contains forbidden %q command: %#v", forbidden, command)
			}
		}
	}
}

func TestDeployPlanAppendsCleanupAfterStateRecordAndNonRoutedStop(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)

	recordWorker := firstCommandIndex(plan.Commands, descriptionContains("record active worker release"))
	stopWorker := firstCommandIndex(plan.Commands, descriptionContains("stop previous worker release"))
	cleanupWorker := firstCommandIndex(plan.Commands, descriptionContains("cleanup old worker release artifacts"))
	recordWeb := firstCommandIndex(plan.Commands, descriptionContains("record active web release"))
	cleanupWeb := firstCommandIndex(plan.Commands, descriptionContains("cleanup old web release artifacts"))
	if recordWorker == -1 || stopWorker == -1 || cleanupWorker == -1 || recordWeb == -1 || cleanupWeb == -1 {
		t.Fatalf("expected record/stop/cleanup commands; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(recordWorker < stopWorker && stopWorker < cleanupWorker) {
		t.Fatalf("worker cleanup must run after state record and non-routed stop; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(recordWeb < cleanupWeb) {
		t.Fatalf("web cleanup must run after state record; descriptions=%v", commandDescriptions(plan.Commands))
	}
}

func TestDeployPlanDoesNotStopPreviousForRoutedServices(t *testing.T) {
	p := newTestPlanner(t)
	plan := deployPlan(t, p)

	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "stop previous web release") || strings.Contains(command.Description, "stop previous api release") {
			t.Fatalf("routed services should keep previous container running after deploy: %#v", command)
		}
	}
}

func TestRollbackPlanStopsPreviousForNonRoutedServicesAfterStateSwap(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	swapWorker := firstCommandIndex(plan.Commands, descriptionContains("rollback state for worker"))
	stopWorker := firstCommandIndex(plan.Commands, descriptionContains("stop previous worker release"))
	if swapWorker == -1 || stopWorker == -1 {
		t.Fatalf("worker rollback-state/stop commands not found; descriptions=%v", commandDescriptions(plan.Commands))
	}
	if !(swapWorker < stopWorker) {
		t.Fatalf("worker rolled-back-from container must stop after state swap; descriptions=%v", commandDescriptions(plan.Commands))
	}
}

func TestRollbackPlanKeepsRoutedRolledBackFromContainersRunning(t *testing.T) {
	p := newTestPlanner(t)
	plan := p.Rollback()

	for _, command := range plan.Commands {
		if strings.Contains(command.Description, "stop previous web release") || strings.Contains(command.Description, "stop previous api release") {
			t.Fatalf("routed services should keep rolled-back-from container running: %#v", command)
		}
	}
}

func TestLogsPlanTargetsConfiguredServiceHost(t *testing.T) {
	p := newTestPlanner(t)
	plan, err := p.Logs(LogsOptions{ServiceName: "web", Color: "active", Tail: 100})
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	if plan.Title != "Logs demo/production service web" {
		t.Fatalf("logs title = %q", plan.Title)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("logs command count = %d, want 1; descriptions=%v", len(plan.Commands), commandDescriptions(plan.Commands))
	}
	command := plan.Commands[0]
	if command.Host != "203.0.113.10" || !strings.Contains(command.Description, "logs for web active") {
		t.Fatalf("logs command targeted wrong host or description: %#v", command)
	}
}

func TestLogsPlanWithHostSelectsOnlyThatServer(t *testing.T) {
	p := newTestPlanner(t)
	service := p.Env.Services["web"]
	service.Hosts = []string{"app-1", "api-1"}
	p.Env.Services["web"] = service

	plan, err := p.Logs(LogsOptions{ServiceName: "web", HostName: "api-1", Color: "green", Tail: 100})
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("logs command count = %d, want 1", len(plan.Commands))
	}
	if got := plan.Commands[0].Host; got != "203.0.113.11" {
		t.Fatalf("logs host = %q, want 203.0.113.11", got)
	}
}

func TestLogsPlanFailsForUnknownService(t *testing.T) {
	p := newTestPlanner(t)
	_, err := p.Logs(LogsOptions{ServiceName: "missing", Color: "active", Tail: 100})
	if err == nil || !strings.Contains(err.Error(), `service "missing" was not found`) {
		t.Fatalf("Logs error = %v, want unknown service", err)
	}
}

func TestLogsPlanFailsForUnknownHost(t *testing.T) {
	p := newTestPlanner(t)
	_, err := p.Logs(LogsOptions{ServiceName: "web", HostName: "missing", Color: "active", Tail: 100})
	if err == nil || !strings.Contains(err.Error(), `host "missing" was not found`) {
		t.Fatalf("Logs error = %v, want unknown host", err)
	}
}

func TestLogsPlanFailsWhenServiceDoesNotRunOnRequestedHost(t *testing.T) {
	p := newTestPlanner(t)
	_, err := p.Logs(LogsOptions{ServiceName: "web", HostName: "api-1", Color: "active", Tail: 100})
	if err == nil || !strings.Contains(err.Error(), `service "web" does not run on host "api-1"`) {
		t.Fatalf("Logs error = %v, want service host mismatch", err)
	}
}

func TestLogsPlanFailsForInvalidColor(t *testing.T) {
	p := newTestPlanner(t)
	_, err := p.Logs(LogsOptions{ServiceName: "web", Color: "purple", Tail: 100})
	if err == nil || !strings.Contains(err.Error(), `color must be active, previous, blue, or green`) {
		t.Fatalf("Logs error = %v, want invalid color", err)
	}
}

func TestLogsPlanFailsForNegativeTail(t *testing.T) {
	p := newTestPlanner(t)
	_, err := p.Logs(LogsOptions{ServiceName: "web", Color: "active", Tail: -1})
	if err == nil || !strings.Contains(err.Error(), "tail must be >= 0") {
		t.Fatalf("Logs error = %v, want invalid tail", err)
	}
}

func TestLogsPlanFailsForFollowAcrossMultipleHostsWithoutHost(t *testing.T) {
	p := newTestPlanner(t)
	service := p.Env.Services["web"]
	service.Hosts = []string{"app-1", "api-1"}
	p.Env.Services["web"] = service

	_, err := p.Logs(LogsOptions{ServiceName: "web", Color: "active", Tail: 100, Follow: true})
	if err == nil || !strings.Contains(err.Error(), "--follow with multiple hosts requires --host") {
		t.Fatalf("Logs error = %v, want follow host requirement", err)
	}
}

func TestLogsPlanAllowsMultipleHostsWithoutFollow(t *testing.T) {
	p := newTestPlanner(t)
	service := p.Env.Services["web"]
	service.Hosts = []string{"app-1", "api-1"}
	p.Env.Services["web"] = service

	plan, err := p.Logs(LogsOptions{ServiceName: "web", Color: "active", Tail: 100})
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	if len(plan.Commands) != 2 {
		t.Fatalf("logs command count = %d, want 2; descriptions=%v", len(plan.Commands), commandDescriptions(plan.Commands))
	}
}

func TestLogsPlanDoesNotIncludeDeployOrMutationCommands(t *testing.T) {
	p := newTestPlanner(t)
	plan, err := p.Logs(LogsOptions{ServiceName: "web", Color: "previous", Tail: 100})
	if err != nil {
		t.Fatalf("Logs returned error: %v", err)
	}
	for _, command := range plan.Commands {
		for _, forbidden := range []string{"build image", "push image", "log in to registry", "upload env", "switch Caddy", "record active", "rollback state", "stop previous", "start inactive"} {
			if strings.Contains(command.Description, forbidden) || strings.Contains(command.Script, forbidden) {
				t.Fatalf("logs plan contains forbidden %q command: %#v", forbidden, command)
			}
		}
	}
}

func newTestPlanner(t *testing.T) *Planner {
	t.Helper()
	cfg, err := config.LoadBytes([]byte(plannerConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	p, err := New(cfg, "production")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return p
}

func deployPlan(t *testing.T, p *Planner) remote.Plan {
	t.Helper()
	plan, err := p.Deploy(fixedDeployTime())
	if err != nil {
		t.Fatalf("Deploy returned error: %v", err)
	}
	return plan
}

func fixedDeployTime() time.Time {
	return time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
}

func descriptionContains(substr string) func(remote.Command) bool {
	return func(command remote.Command) bool {
		return strings.Contains(command.Description, substr)
	}
}

func firstCommandIndex(commands []remote.Command, match func(remote.Command) bool) int {
	for i, command := range commands {
		if match(command) {
			return i
		}
	}
	return -1
}

func lastCommandIndex(commands []remote.Command, match func(remote.Command) bool) int {
	for i := len(commands) - 1; i >= 0; i-- {
		if match(commands[i]) {
			return i
		}
	}
	return -1
}

func commandDescriptions(commands []remote.Command) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, command.Description)
	}
	return out
}

func countCommandDescriptions(commands []remote.Command, description string) int {
	count := 0
	for _, command := range commands {
		if strings.Contains(command.Description, description) {
			count++
		}
	}
	return count
}

func commandsMatching(commands []remote.Command, match func(remote.Command) bool) []remote.Command {
	var out []remote.Command
	for _, command := range commands {
		if match(command) {
			out = append(out, command)
		}
	}
	return out
}

func assertNoSecretValues(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{"registry-password-value", "postgres://secret-value", "redis://secret-value"} {
		if strings.Contains(output, secret) {
			t.Fatalf("secret value %q leaked in output:\n%s", secret, output)
		}
	}
}

type noOpRunner struct{}

func (noOpRunner) Run(context.Context, remote.Command) error {
	return nil
}

const plannerConfigYAML = `project: demo

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
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
        host_ssh_port: 22
      api-1:
        host: 203.0.113.11
        ssh_user: root
        host_ssh_port: 22
    proxy:
      routes:
        - host: app.example.com
          service: web
          tls: true
        - host: api.example.com
          service: api
          tls: true
    services:
      api:
        image: ghcr.io/example/demo-api
        build:
          context: .
          dockerfile: Dockerfile.api
        hosts: [api-1]
        internal_port: 4000
        health_check:
          path: /healthz
        secrets:
          - DATABASE_URL
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
          dockerfile: Dockerfile
        hosts: [app-1]
        internal_port: 3000
        health_check:
          path: /healthz
        secrets:
          - DATABASE_URL
          - REDIS_URL
      worker:
        image: ghcr.io/example/demo-worker
        build:
          context: .
          dockerfile: Dockerfile.worker
        hosts: [api-1]
        env:
          RACK_ENV: production
        secrets:
          - DATABASE_URL
`
