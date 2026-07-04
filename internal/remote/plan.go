package remote

import (
	"fmt"
	"io"
)

// Plan is a printable sequence of remote commands.
type Plan struct {
	Title    string
	Commands []Command
}

func (p Plan) Empty() bool { return len(p.Commands) == 0 }

func (p Plan) Print(w io.Writer) {
	if p.Title != "" {
		fmt.Fprintf(w, "%s\n", p.Title)
	}
	if len(p.Commands) == 0 {
		fmt.Fprintln(w, "  no commands")
		return
	}
	for i, cmd := range p.Commands {
		fmt.Fprintf(w, "%d. %s\n", i+1, cmd.Description)
		if cmd.Host != "" {
			if cmd.SSHUser != "" {
				fmt.Fprintf(w, "   host: %s@%s\n", cmd.SSHUser, cmd.Host)
			} else {
				fmt.Fprintf(w, "   host: %s\n", cmd.Host)
			}
			if cmd.SSHPort != 0 {
				fmt.Fprintf(w, "   port: %d\n", cmd.SSHPort)
			}
		} else {
			fmt.Fprintln(w, "   local: true")
		}
		if cmd.Script != "" {
			if cmd.Sensitive {
				fmt.Fprintln(w, "   run: <sensitive command redacted>")
			} else {
				fmt.Fprintf(w, "   run: %s\n", cmd.Script)
			}
		}
	}
}
