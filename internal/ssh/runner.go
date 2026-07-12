package ssh

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/s4m1nd/slipway/internal/console"
	"github.com/s4m1nd/slipway/internal/remote"
)

type Runner struct {
	Stdout       io.Writer
	Stderr       io.Writer
	Verbose      bool
	OutputIndent string
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
	if err := r.runCommand(cmd, command); err != nil {
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
	if err := r.runCommand(cmd, command); err != nil {
		return fmt.Errorf("%s: %q failed: %w", command.Host, command.Description, err)
	}
	return nil
}

func (r Runner) runCommand(cmd *exec.Cmd, command remote.Command) error {
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

	if r.Verbose || command.OutputMode == remote.OutputStream {
		cmd.Stdout = r.outputWriter(r.Stdout)
		cmd.Stderr = r.outputWriter(r.Stderr)
		return cmd.Run()
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		r.replayOutput(stdout.String(), r.Stdout)
		r.replayOutput(stderr.String(), r.Stderr)
		return err
	}
	if command.OutputMode == remote.OutputCaddySummary {
		r.writeCaddySummary(stdout.String(), stderr.String())
	} else if command.OutputMode == remote.OutputQuietOnSuccess {
		r.writeRoutineWarnings(stdout.String(), stderr.String())
	}
	return nil
}

func (r Runner) writeRoutineWarnings(stdout string, stderr string) {
	c := console.New(r.Stdout, r.Stderr)
	for _, output := range []string{stdout, stderr} {
		scanner := bufio.NewScanner(strings.NewReader(output))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			lower := strings.ToLower(line)
			if line == "" || (!strings.Contains(lower, "warn") && !strings.Contains(lower, "error") && !strings.Contains(lower, "fail") && !strings.Contains(lower, "deprecat") && !strings.Contains(lower, "refus")) {
				continue
			}
			c.Warning(line)
		}
	}
}

func (r Runner) outputWriter(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	if r.OutputIndent == "" {
		return w
	}
	return &linePrefixWriter{writer: w, prefix: r.OutputIndent, lineStart: true}
}

func (r Runner) replayOutput(output string, w io.Writer) {
	if output == "" || w == nil {
		return
	}
	_, _ = io.WriteString(r.outputWriter(w), output)
}

func (r Runner) writeCaddySummary(stdout string, stderr string) {
	c := console.New(r.Stdout, r.Stderr)
	for _, output := range []string{stdout, stderr} {
		scanner := bufio.NewScanner(strings.NewReader(output))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || line == "Valid configuration" {
				continue
			}
			var entry struct {
				Level string `json:"level"`
				Msg   string `json:"msg"`
			}
			if json.Unmarshal([]byte(line), &entry) == nil {
				if entry.Level == "warn" || entry.Level == "error" {
					c.Warning("Caddy: " + entry.Msg)
				}
				continue
			}
			r.replayOutput(line+"\n", r.Stdout)
		}
	}
}

type linePrefixWriter struct {
	mu        sync.Mutex
	writer    io.Writer
	prefix    string
	lineStart bool
}

func (w *linePrefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	written := 0
	for len(p) > 0 {
		if w.lineStart {
			if _, err := io.WriteString(w.writer, w.prefix); err != nil {
				return written, err
			}
			w.lineStart = false
		}
		newline := bytes.IndexByte(p, '\n')
		if newline == -1 {
			n, err := w.writer.Write(p)
			written += n
			return written, err
		}
		chunk := p[:newline+1]
		n, err := w.writer.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
		w.lineStart = true
		p = p[newline+1:]
	}
	return written, nil
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
