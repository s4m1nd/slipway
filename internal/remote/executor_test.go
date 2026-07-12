package remote

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/s4m1nd/slipway/internal/console"
)

func TestExecutePrintsConciseStepsWithoutGeneratedScripts(t *testing.T) {
	plan := Plan{Title: "Deploy demo/production", Subtitle: "Release release-a", Commands: []Command{{
		Description: "build image for web",
		Script:      "docker build --secret-value",
	}}}
	var out strings.Builder

	if err := Execute(context.Background(), plan, &recordingRunner{}, &out); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Deploy demo/production", "Release release-a", "1/1", "build image for web", "local", "✓ complete", "("} {
		if !strings.Contains(got, want) {
			t.Fatalf("concise output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "docker build") || strings.Contains(got, "--secret-value") {
		t.Fatalf("normal execution dumped generated script:\n%s", got)
	}
}

func TestExecutePrintsSemanticStepResults(t *testing.T) {
	plan := Plan{Commands: []Command{
		{Description: "start inactive web release"},
		{Description: "wait for web health check"},
		{Description: "switch Caddy routes"},
		{Description: "record active web release"},
	}}
	var out strings.Builder
	if err := Execute(context.Background(), plan, &recordingRunner{}, &out); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	for _, want := range []string{"✓ container started", "✓ healthy", "✓ traffic switched", "✓ state recorded"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("semantic output missing %q:\n%s", want, out.String())
		}
	}
}

func TestExecuteVerbosePrintsOnlyNonSensitiveScripts(t *testing.T) {
	plan := Plan{Commands: []Command{
		{Description: "build", Script: "docker build ."},
		{Description: "upload env", Script: "cat > secret.env", Sensitive: true},
	}}
	var out strings.Builder
	c := console.NewWithMode(&out, &out, console.ColorNever)
	c.Verbose = true

	if err := ExecuteWithConsole(context.Background(), plan, &recordingRunner{}, c); err != nil {
		t.Fatalf("ExecuteWithConsole returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "run: docker build .") || !strings.Contains(got, "run: <sensitive command redacted>") {
		t.Fatalf("verbose output missing command details:\n%s", got)
	}
	if strings.Contains(got, "cat > secret.env") {
		t.Fatalf("verbose output leaked sensitive command:\n%s", got)
	}
}

func TestExecutePrintsConciseFailure(t *testing.T) {
	plan := Plan{Commands: []Command{{Description: "wait for web health check"}}}
	var out strings.Builder
	err := Execute(context.Background(), plan, &recordingRunner{err: errors.New("exit status 1")}, &out)
	if err == nil {
		t.Fatal("expected Execute to return runner error")
	}
	if got := out.String(); !strings.Contains(got, "✗ wait for web health check: exit status 1") {
		t.Fatalf("failure output was not concise and actionable:\n%s", got)
	}
}

func TestExecuteDoesNotLeakSensitiveSteps(t *testing.T) {
	runner := &recordingRunner{}
	plan := Plan{Commands: []Command{{
		Description: "upload env",
		Script:      "cat > secret.env",
		Stdin:       "DATABASE_URL=secret",
		Sensitive:   true,
	}}}
	var out strings.Builder
	err := Execute(context.Background(), plan, runner, &out)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(out.String(), "DATABASE_URL=secret") || strings.Contains(out.String(), "cat > secret.env") {
		t.Fatalf("sensitive details leaked: %s", out.String())
	}
	if len(runner.commands) != 1 || runner.commands[0].Stdin == "" {
		t.Fatalf("runner did not receive command stdin: %#v", runner.commands)
	}
}

func TestPlanPrintRedactsSensitiveSteps(t *testing.T) {
	plan := Plan{Commands: []Command{{
		Description: "upload env",
		Script:      "cat > secret.env",
		Stdin:       "DATABASE_URL=secret",
		Sensitive:   true,
	}}}
	var out strings.Builder
	plan.Print(&out)
	if strings.Contains(out.String(), "DATABASE_URL=secret") || strings.Contains(out.String(), "cat > secret.env") {
		t.Fatalf("sensitive details leaked: %s", out.String())
	}
}

func TestPlanPrintStylesDryRunHeadingWhenColorIsForced(t *testing.T) {
	t.Setenv("SLIPWAY_COLOR", "always")
	var out strings.Builder
	Plan{Title: "Deploy demo/production"}.Print(&out)
	if got := out.String(); !strings.Contains(got, "\x1b[1;33mDRY RUN\x1b[0m") {
		t.Fatalf("dry-run heading was not styled yellow: %q", got)
	}
}

func TestExecuteStopsOnFailure(t *testing.T) {
	runner := &recordingRunner{err: errors.New("boom")}
	plan := Plan{Commands: []Command{{Description: "one"}, {Description: "two"}}}
	err := Execute(context.Background(), plan, runner, &strings.Builder{})
	if err == nil {
		t.Fatal("expected Execute to return runner error")
	}
	if len(runner.commands) != 1 {
		t.Fatalf("ran %d commands, want 1", len(runner.commands))
	}
}

func TestExecuteRunsAlwaysCommandAfterPriorSuccess(t *testing.T) {
	runner := &recordingRunner{errByDescription: map[string]error{"mutate": errors.New("boom")}}
	plan := Plan{Commands: []Command{
		{ID: "lock:host", Description: "acquire lock"},
		{Description: "mutate"},
		{Description: "skip after failure"},
		{Description: "release lock", Always: true, RunIfSucceeded: "lock:host"},
	}}

	err := Execute(context.Background(), plan, runner, &strings.Builder{})
	if err == nil {
		t.Fatal("expected Execute to return mutate error")
	}
	if got := commandDescriptions(runner.commands); strings.Join(got, ",") != "acquire lock,mutate,release lock" {
		t.Fatalf("commands ran in wrong order: %v", got)
	}
}

func TestExecuteSkipsConditionalAlwaysCommandWhenAcquireFails(t *testing.T) {
	runner := &recordingRunner{errByDescription: map[string]error{"acquire lock": errors.New("held")}}
	plan := Plan{Commands: []Command{
		{ID: "lock:host", Description: "acquire lock"},
		{Description: "mutate"},
		{Description: "release lock", Always: true, RunIfSucceeded: "lock:host"},
	}}

	err := Execute(context.Background(), plan, runner, &strings.Builder{})
	if err == nil {
		t.Fatal("expected Execute to return acquire error")
	}
	if got := commandDescriptions(runner.commands); strings.Join(got, ",") != "acquire lock" {
		t.Fatalf("commands ran in wrong order: %v", got)
	}
}

type recordingRunner struct {
	commands         []Command
	err              error
	errByDescription map[string]error
}

func (r *recordingRunner) Run(_ context.Context, command Command) error {
	r.commands = append(r.commands, command)
	if r.errByDescription != nil {
		return r.errByDescription[command.Description]
	}
	return r.err
}

func commandDescriptions(commands []Command) []string {
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		out = append(out, command.Description)
	}
	return out
}
