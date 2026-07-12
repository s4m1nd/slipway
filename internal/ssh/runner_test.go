package ssh

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/s4m1nd/slipway/internal/remote"
)

func TestSSHArgsIncludeUserPortAndScript(t *testing.T) {
	cmd := remote.Command{
		Host:    "203.0.113.10",
		SSHUser: "root",
		SSHPort: 2201,
		Script:  "docker ps",
	}
	want := []string{"-p", "2201", "root@203.0.113.10", "docker ps"}
	if got := SSHArgs(cmd); !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgs() = %#v, want %#v", got, want)
	}
}

func TestRunnerIndentsStreamedCommandOutput(t *testing.T) {
	var out bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &out, OutputIndent: "        │ "}
	err := runner.Run(context.Background(), remote.Command{Description: "show output", Script: "printf 'one\\ntwo\\n'"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := out.String(), "        │ one\n        │ two\n"; got != want {
		t.Fatalf("indented output = %q, want %q", got, want)
	}
}

func TestRunnerQuietOutputIsReplayedOnlyOnFailure(t *testing.T) {
	var out bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &out, OutputIndent: "        │ "}
	quiet := remote.Command{Description: "routine", Script: "printf 'routine detail\\n'", OutputMode: remote.OutputQuietOnSuccess}
	if err := runner.Run(context.Background(), quiet); err != nil {
		t.Fatalf("successful quiet command returned error: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("successful quiet command printed output: %q", out.String())
	}
	quiet.Script = "printf 'WARNING: credential store is not configured\\n' >&2"
	if err := runner.Run(context.Background(), quiet); err != nil {
		t.Fatalf("successful warning command returned error: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "! WARNING: credential store is not configured") {
		t.Fatalf("successful quiet command swallowed warning: %q", got)
	}

	out.Reset()
	quiet.Script = "printf 'failure detail\\n'; exit 1"
	if err := runner.Run(context.Background(), quiet); err == nil {
		t.Fatal("expected quiet command failure")
	}
	if got := out.String(); !strings.Contains(got, "        │ failure detail") {
		t.Fatalf("failed quiet command did not replay output: %q", got)
	}
}

func TestRunnerCompactsSuccessfulCaddyOutputToWarnings(t *testing.T) {
	var out bytes.Buffer
	runner := Runner{Stdout: &out, Stderr: &out}
	command := remote.Command{
		Description: "switch Caddy routes",
		OutputMode:  remote.OutputCaddySummary,
		Script: `printf 'Valid configuration\n'
printf '%s\n' '{"level":"info","msg":"routine detail"}' >&2
printf '%s\n' '{"level":"warn","msg":"HTTP-only route"}' >&2`,
	}
	if err := runner.Run(context.Background(), command); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "! Caddy: HTTP-only route") {
		t.Fatalf("compact output omitted warning: %q", got)
	}
	if strings.Contains(got, "routine detail") || strings.Contains(got, "Valid configuration") {
		t.Fatalf("compact output retained routine Caddy details: %q", got)
	}
}

func TestSSHArgsOmitUserWhenUnset(t *testing.T) {
	cmd := remote.Command{
		Host:    "example.com",
		SSHPort: 22,
		Script:  "true",
	}
	want := []string{"-p", "22", "example.com", "true"}
	if got := SSHArgs(cmd); !reflect.DeepEqual(got, want) {
		t.Fatalf("SSHArgs() = %#v, want %#v", got, want)
	}
}

func TestRunnerOutputRunsLocalCommandThroughShell(t *testing.T) {
	runner := Runner{}
	got, err := runner.Output(context.Background(), remote.Command{
		Description: "print status",
		Script:      "printf 'state=missing\\n'",
	})
	if err != nil {
		t.Fatalf("Output returned error: %v", err)
	}
	if got != "state=missing\n" {
		t.Fatalf("Output() = %q, want state line", got)
	}
}
