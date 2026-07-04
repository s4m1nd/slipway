package remote

import (
	"context"
	"fmt"
	"io"
)

type ExecutorRunner interface {
	Run(context.Context, Command) error
}

type OutputRunner interface {
	Output(context.Context, Command) (string, error)
}

func Execute(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer) error {
	return ExecuteWithSucceeded(ctx, plan, runner, stdout, map[string]bool{})
}

func ExecuteWithSucceeded(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer, succeeded map[string]bool) error {
	_, err := ExecuteWithSucceededFrom(ctx, plan, runner, stdout, succeeded, 1)
	return err
}

func ExecuteWithSucceededFrom(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer, succeeded map[string]bool, startIndex int) (int, error) {
	if succeeded == nil {
		succeeded = map[string]bool{}
	}
	if startIndex < 1 {
		startIndex = 1
	}
	if plan.Title != "" {
		fmt.Fprintln(stdout, plan.Title)
	}
	var firstErr error
	nextIndex := startIndex
	for _, command := range plan.Commands {
		if firstErr != nil && !command.Always {
			continue
		}
		if command.RunIfSucceeded != "" && !succeeded[command.RunIfSucceeded] {
			continue
		}
		fmt.Fprintf(stdout, "%d. %s\n", nextIndex, command.Description)
		nextIndex++
		if command.Host != "" {
			if command.SSHUser != "" {
				fmt.Fprintf(stdout, "   host: %s@%s\n", command.SSHUser, command.Host)
			} else {
				fmt.Fprintf(stdout, "   host: %s\n", command.Host)
			}
			if command.SSHPort != 0 {
				fmt.Fprintf(stdout, "   port: %d\n", command.SSHPort)
			}
		} else {
			fmt.Fprintln(stdout, "   local: true")
		}
		if command.Sensitive {
			fmt.Fprintln(stdout, "   run: <sensitive command redacted>")
		} else if command.Script != "" {
			fmt.Fprintf(stdout, "   run: %s\n", command.Script)
		}
		if err := runner.Run(ctx, command); err != nil {
			if firstErr == nil {
				firstErr = err
			} else {
				firstErr = fmt.Errorf("%w; cleanup command %q failed: %v", firstErr, command.Description, err)
			}
			continue
		}
		if command.ID != "" {
			succeeded[command.ID] = true
		}
	}
	return nextIndex, firstErr
}
