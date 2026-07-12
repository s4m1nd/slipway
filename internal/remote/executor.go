package remote

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/s4m1nd/slipway/internal/console"
)

type ExecutorRunner interface {
	Run(context.Context, Command) error
}

type OutputRunner interface {
	Output(context.Context, Command) (string, error)
}

func Execute(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer) error {
	c := console.New(stdout, stdout)
	return ExecuteWithConsole(ctx, plan, runner, c)
}

func ExecuteWithSucceeded(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer, succeeded map[string]bool) error {
	c := console.New(stdout, stdout)
	_, err := ExecuteWithSucceededFromConsole(ctx, plan, runner, c, succeeded, 1, len(plan.Commands))
	return err
}

func ExecuteWithSucceededFrom(ctx context.Context, plan Plan, runner ExecutorRunner, stdout io.Writer, succeeded map[string]bool, startIndex int) (int, error) {
	c := console.New(stdout, stdout)
	return ExecuteWithSucceededFromConsole(ctx, plan, runner, c, succeeded, startIndex, startIndex+len(plan.Commands)-1)
}

func ExecuteWithConsole(ctx context.Context, plan Plan, runner ExecutorRunner, c console.Console) error {
	_, err := ExecuteWithSucceededFromConsole(ctx, plan, runner, c, map[string]bool{}, 1, len(plan.Commands))
	return err
}

func ExecuteWithSucceededFromConsole(ctx context.Context, plan Plan, runner ExecutorRunner, c console.Console, succeeded map[string]bool, startIndex int, totalSteps int) (int, error) {
	if succeeded == nil {
		succeeded = map[string]bool{}
	}
	if startIndex < 1 {
		startIndex = 1
	}
	if totalSteps < startIndex+len(plan.Commands)-1 {
		totalSteps = startIndex + len(plan.Commands) - 1
	}
	if plan.Title != "" {
		c.Title(plan.Title)
	}
	if plan.Subtitle != "" {
		c.Subtitle("", plan.Subtitle)
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
		c.Step(nextIndex, totalSteps, command.Description)
		nextIndex++
		if command.Host != "" {
			target := command.Host
			if command.SSHUser != "" {
				target = command.SSHUser + "@" + command.Host
			}
			if command.SSHPort != 0 && command.SSHPort != 22 {
				target += ":" + strconv.Itoa(command.SSHPort)
			}
			c.Target(target)
		} else {
			c.Target("local")
		}
		c.Command(command.Script, command.Sensitive)
		started := time.Now()
		if err := runner.Run(ctx, command); err != nil {
			c.Failure(fmt.Sprintf("%s: %v", command.Description, err))
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
		c.SuccessTimed(successMessage(command.Description), time.Since(started))
	}
	return nextIndex, firstErr
}

func successMessage(description string) string {
	switch {
	case strings.HasPrefix(description, "start inactive "), strings.HasPrefix(description, "start previous "):
		return "container started"
	case strings.HasPrefix(description, "wait for ") && strings.HasSuffix(description, " health check"):
		return "healthy"
	case description == "switch Caddy routes":
		return "traffic switched"
	case description == "sync Caddy routes":
		return "routes synced"
	case strings.HasPrefix(description, "record active "):
		return "state recorded"
	case strings.HasPrefix(description, "rollback state for "):
		return "rollback state recorded"
	default:
		return "complete"
	}
}
