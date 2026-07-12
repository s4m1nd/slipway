package remote

import "strings"

type OutputMode string

const (
	OutputStream         OutputMode = ""
	OutputQuietOnSuccess OutputMode = "quiet-on-success"
	OutputCaddySummary   OutputMode = "caddy-summary"
)

// Command is a local or host command. The planner creates Commands; transport
// implementations execute them.
type Command struct {
	Host        string
	SSHUser     string
	SSHPort     int
	Description string
	Script      string
	Stdin       string
	Sensitive   bool
	OutputMode  OutputMode

	// ID names a successfully completed command so later Always commands can
	// be tied to it. It is intentionally not rendered in plans.
	ID string
	// Always lets cleanup/finalizer commands run after an earlier failure.
	Always bool
	// RunIfSucceeded skips an Always command unless the named command completed.
	RunIfSucceeded string
}

func (c Command) String() string {
	parts := []string{}
	if c.Host != "" {
		if c.SSHUser != "" {
			parts = append(parts, c.SSHUser+"@"+c.Host)
		} else {
			parts = append(parts, c.Host)
		}
	}
	if c.Description != "" {
		parts = append(parts, c.Description)
	}
	if c.Script != "" {
		if c.Sensitive {
			parts = append(parts, "<sensitive command redacted>")
		} else {
			parts = append(parts, c.Script)
		}
	}
	return strings.Join(parts, " | ")
}
