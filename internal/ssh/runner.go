package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"

	"github.com/s4m1nd/slipway/internal/remote"
)

type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func (r Runner) Run(ctx context.Context, command remote.Command) error {
	if command.Host == "" {
		return r.runLocal(ctx, command)
	}
	return r.runSSH(ctx, command)
}

func (r Runner) Output(ctx context.Context, command remote.Command) (string, error) {
	if command.Host == "" {
		return r.outputLocal(ctx, command)
	}
	return r.outputSSH(ctx, command)
}

func (r Runner) runLocal(ctx context.Context, command remote.Command) error {
	cmd := exec.CommandContext(ctx, "sh", "-lc", command.Script)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if command.Stdin != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		go func() {
			defer stdin.Close()
			_, _ = io.WriteString(stdin, command.Stdin)
		}()
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("local command %q failed: %w", command.Description, err)
	}
	return nil
}

func (r Runner) outputLocal(ctx context.Context, command remote.Command) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-lc", command.Script)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = r.Stderr
	if command.Stdin != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		go func() {
			defer stdin.Close()
			_, _ = io.WriteString(stdin, command.Stdin)
		}()
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("local command %q failed: %w", command.Description, err)
	}
	return stdout.String(), nil
}

func (r Runner) runSSH(ctx context.Context, command remote.Command) error {
	cmd := exec.CommandContext(ctx, "ssh", SSHArgs(command)...)
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if command.Stdin != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		go func() {
			defer stdin.Close()
			_, _ = io.WriteString(stdin, command.Stdin)
		}()
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %q failed: %w", command.Host, command.Description, err)
	}
	return nil
}

func (r Runner) outputSSH(ctx context.Context, command remote.Command) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh", SSHArgs(command)...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = r.Stderr
	if command.Stdin != "" {
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		go func() {
			defer stdin.Close()
			_, _ = io.WriteString(stdin, command.Stdin)
		}()
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %q failed: %w", command.Host, command.Description, err)
	}
	return stdout.String(), nil
}

func SSHArgs(command remote.Command) []string {
	port := command.SSHPort
	if port == 0 {
		port = 22
	}
	target := command.Host
	if command.SSHUser != "" {
		target = command.SSHUser + "@" + command.Host
	}
	return []string{"-p", strconv.Itoa(port), target, command.Script}
}
