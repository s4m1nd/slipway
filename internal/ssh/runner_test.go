package ssh

import (
	"context"
	"reflect"
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
