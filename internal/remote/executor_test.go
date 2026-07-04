package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestExecutePrintsRedactedSensitiveSteps(t *testing.T) {
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
