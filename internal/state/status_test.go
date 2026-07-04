package state

import (
	"strings"
	"testing"
)

func TestParseServiceStatusWithActiveAndPreviousState(t *testing.T) {
	output := strings.Join([]string{
		"state=present",
		"project=demo",
		"environment=production",
		"service=web",
		"active_color=green",
		"active_release=20260630T120000Z",
		"active_image=ghcr.io/example/app:20260630T120000Z",
		"previous_color=blue",
		"previous_release=20260629T120000Z",
		"previous_image=ghcr.io/example/app:20260629T120000Z",
		"blue_exists=true",
		"blue_running=true",
		"blue_release=20260629T120000Z",
		"blue_image=ghcr.io/example/app:20260629T120000Z",
		"green_exists=true",
		"green_running=true",
		"green_release=20260630T120000Z",
		"green_image=ghcr.io/example/app:20260630T120000Z",
	}, "\n")

	got, err := ParseServiceStatus(Target{
		HostName: "app-1",
		Host:     "203.0.113.10",
		SSHUser:  "root",
		SSHPort:  22,
		Service:  "web",
	}, output)
	if err != nil {
		t.Fatalf("ParseServiceStatus returned error: %v", err)
	}

	if !got.StateExists {
		t.Fatal("StateExists = false, want true")
	}
	if got.Project != "demo" || got.Environment != "production" || got.Service != "web" {
		t.Fatalf("unexpected identity: %#v", got)
	}
	if got.Active.Color != "green" || got.Active.Release != "20260630T120000Z" || got.Active.Image != "ghcr.io/example/app:20260630T120000Z" {
		t.Fatalf("unexpected active release: %#v", got.Active)
	}
	if got.Previous.Color != "blue" || got.Previous.Release != "20260629T120000Z" || got.Previous.Image != "ghcr.io/example/app:20260629T120000Z" {
		t.Fatalf("unexpected previous release: %#v", got.Previous)
	}
	if !got.Blue.Exists || !got.Blue.Running || got.Blue.Release != "20260629T120000Z" {
		t.Fatalf("unexpected blue container: %#v", got.Blue)
	}
	if !got.Green.Exists || !got.Green.Running || got.Green.Release != "20260630T120000Z" {
		t.Fatalf("unexpected green container: %#v", got.Green)
	}
}

func TestParseServiceStatusHandlesMissingStateAndContainers(t *testing.T) {
	output := strings.Join([]string{
		"state=missing",
		"project=demo",
		"environment=production",
		"service=worker",
		"active_color=",
		"active_release=",
		"active_image=",
		"previous_color=",
		"previous_release=",
		"previous_image=",
		"blue_exists=false",
		"blue_running=false",
		"blue_release=",
		"blue_image=",
		"green_exists=false",
		"green_running=false",
		"green_release=",
		"green_image=",
	}, "\n")

	got, err := ParseServiceStatus(Target{HostName: "worker-1", Host: "203.0.113.11", SSHUser: "root", SSHPort: 22, Service: "worker"}, output)
	if err != nil {
		t.Fatalf("ParseServiceStatus returned error: %v", err)
	}
	if got.StateExists {
		t.Fatal("StateExists = true, want false")
	}
	if got.Active.Release != "" || got.Previous.Release != "" {
		t.Fatalf("missing state should not invent releases: active=%#v previous=%#v", got.Active, got.Previous)
	}
	if got.Blue.Exists || got.Green.Exists {
		t.Fatalf("containers should be missing: blue=%#v green=%#v", got.Blue, got.Green)
	}
}

func TestParseServiceStatusRejectsMalformedOutput(t *testing.T) {
	_, err := ParseServiceStatus(Target{HostName: "app-1", Host: "203.0.113.10", Service: "web"}, "state=present\nnot-a-pair\n")
	if err == nil {
		t.Fatal("expected malformed output error")
	}
	if !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "KEY=VALUE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseServiceStatusRejectsMissingRequiredKeys(t *testing.T) {
	output := strings.Join([]string{
		"state=present",
		"project=demo",
		"environment=production",
		"service=web",
		"active_color=green",
		"active_release=20260630T120000Z",
		"active_image=ghcr.io/example/app:20260630T120000Z",
		"previous_color=blue",
		"previous_release=20260629T120000Z",
		"previous_image=ghcr.io/example/app:20260629T120000Z",
		"blue_exists=true",
		"blue_running=true",
		"blue_release=20260629T120000Z",
		"blue_image=ghcr.io/example/app:20260629T120000Z",
		"green_running=true",
		"green_release=20260630T120000Z",
		"green_image=ghcr.io/example/app:20260630T120000Z",
	}, "\n")

	_, err := ParseServiceStatus(Target{HostName: "app-1", Host: "203.0.113.10", Service: "web"}, output)
	if err == nil {
		t.Fatal("expected missing key error")
	}
	if !strings.Contains(err.Error(), "green_exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseServiceStatusRejectsInvalidContainerBooleans(t *testing.T) {
	output := strings.Join([]string{
		"state=missing",
		"project=demo",
		"environment=production",
		"service=worker",
		"active_color=",
		"active_release=",
		"active_image=",
		"previous_color=",
		"previous_release=",
		"previous_image=",
		"blue_exists=maybe",
		"blue_running=false",
		"blue_release=",
		"blue_image=",
		"green_exists=false",
		"green_running=false",
		"green_release=",
		"green_image=",
	}, "\n")

	_, err := ParseServiceStatus(Target{HostName: "worker-1", Host: "203.0.113.11", Service: "worker"}, output)
	if err == nil {
		t.Fatal("expected invalid boolean error")
	}
	if !strings.Contains(err.Error(), "blue_exists") || !strings.Contains(err.Error(), "boolean") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRenderReportIncludesServiceNamesAndHostIdentifiers(t *testing.T) {
	statuses := []ServiceStatus{{
		Project:     "demo",
		Environment: "production",
		Service:     "web",
		HostName:    "app-1",
		Host:        "203.0.113.10",
		SSHUser:     "root",
		SSHPort:     22,
		StateExists: true,
		Active:      Release{Color: "green", Release: "20260630T120000Z", Image: "ghcr.io/example/app:20260630T120000Z"},
		Previous:    Release{Color: "blue", Release: "20260629T120000Z", Image: "ghcr.io/example/app:20260629T120000Z"},
		Blue:        Container{Exists: true, Running: true, Release: "20260629T120000Z", Image: "ghcr.io/example/app:20260629T120000Z"},
		Green:       Container{Exists: true, Running: true, Release: "20260630T120000Z", Image: "ghcr.io/example/app:20260630T120000Z"},
	}}

	var out strings.Builder
	RenderReport(&out, "demo", "production", statuses)
	got := out.String()
	for _, want := range []string{"demo/production status", "web", "app-1/root@203.0.113.10", "green 20260630T120000Z", "blue 20260629T120000Z", "running"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
}

func TestValidateRollbackReadyPassesForValidActiveAndPreviousState(t *testing.T) {
	err := ValidateRollbackReady([]ServiceStatus{rollbackReadyStatus()})
	if err != nil {
		t.Fatalf("ValidateRollbackReady returned error: %v", err)
	}
}

func TestValidateRollbackReadyFailsWhenStateIsMissing(t *testing.T) {
	status := rollbackReadyStatus()
	status.StateExists = false

	err := ValidateRollbackReady([]ServiceStatus{status})
	assertRollbackReadyError(t, err, "state is missing")
}

func TestValidateRollbackReadyFailsWhenPreviousReleaseIsEmpty(t *testing.T) {
	status := rollbackReadyStatus()
	status.Previous.Release = ""

	err := ValidateRollbackReady([]ServiceStatus{status})
	assertRollbackReadyError(t, err, "previous_release is required")
}

func TestValidateRollbackReadyFailsWhenPreviousColorIsEmptyOrInvalid(t *testing.T) {
	for _, color := range []string{"", "purple"} {
		status := rollbackReadyStatus()
		status.Previous.Color = color

		err := ValidateRollbackReady([]ServiceStatus{status})
		assertRollbackReadyError(t, err, "previous_color must be blue or green")
	}
}

func TestValidateRollbackReadyFailsWhenPreviousColorEqualsActiveColor(t *testing.T) {
	status := rollbackReadyStatus()
	status.Previous.Color = status.Active.Color

	err := ValidateRollbackReady([]ServiceStatus{status})
	assertRollbackReadyError(t, err, "previous_color must differ from active_color")
}

func TestValidateRollbackReadyFailsWhenPreviousContainerIsMissing(t *testing.T) {
	status := rollbackReadyStatus()
	status.Previous.Color = "blue"
	status.Blue.Exists = false

	err := ValidateRollbackReady([]ServiceStatus{status})
	assertRollbackReadyError(t, err, "previous blue container is missing")
}

func TestValidateRollbackReadyAllowsPreviousContainerToBeStopped(t *testing.T) {
	status := rollbackReadyStatus()
	status.Previous.Color = "blue"
	status.Blue.Exists = true
	status.Blue.Running = false

	err := ValidateRollbackReady([]ServiceStatus{status})
	if err != nil {
		t.Fatalf("ValidateRollbackReady returned error for stopped previous container: %v", err)
	}
}

func TestValidateRollbackReadyFailsWhenStatusServiceDoesNotMatchConfiguredTarget(t *testing.T) {
	status := rollbackReadyStatus()
	status.TargetService = "web"
	status.Service = "api"

	err := ValidateRollbackReady([]ServiceStatus{status})
	assertRollbackReadyError(t, err, `state service "api" does not match configured service "web"`)
}

func rollbackReadyStatus() ServiceStatus {
	return ServiceStatus{
		Project:       "demo",
		Environment:   "production",
		Service:       "web",
		TargetService: "web",
		HostName:      "app-1",
		Host:          "203.0.113.10",
		SSHUser:       "root",
		SSHPort:       22,
		StateExists:   true,
		Active:        Release{Color: "green", Release: "20260630T120000Z", Image: "ghcr.io/example/app:20260630T120000Z"},
		Previous:      Release{Color: "blue", Release: "20260629T120000Z", Image: "ghcr.io/example/app:20260629T120000Z"},
		Blue:          Container{Exists: true, Running: false, Release: "20260629T120000Z", Image: "ghcr.io/example/app:20260629T120000Z"},
		Green:         Container{Exists: true, Running: true, Release: "20260630T120000Z", Image: "ghcr.io/example/app:20260630T120000Z"},
	}
}

func assertRollbackReadyError(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected rollback readiness error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("rollback readiness error missing %q:\n%v", want, err)
	}
}
