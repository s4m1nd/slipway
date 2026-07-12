package remote

import (
	"fmt"
	"io"

	"github.com/s4m1nd/slipway/internal/console"
)

// Plan is a printable sequence of remote commands.
type Plan struct {
	Title    string
	Subtitle string
	Commands []Command
}

func (p Plan) Empty() bool { return len(p.Commands) == 0 }

func (p Plan) Print(w io.Writer) {
	c := console.New(w, w)
	fmt.Fprintln(w, c.Paint("DRY RUN", console.StyleBold, console.StyleYellow))
	if p.Title != "" {
		c.Title(p.Title)
	}
	if p.Subtitle != "" {
		c.Subtitle("", p.Subtitle)
	}
	if len(p.Commands) == 0 {
		fmt.Fprintln(w, "  no commands")
		return
	}
	for i, cmd := range p.Commands {
		fmt.Fprintf(w, "%d. %s\n", i+1, c.Paint(cmd.Description, console.StyleBold))
		if cmd.Host != "" {
			if cmd.SSHUser != "" {
				fmt.Fprintf(w, "   %s\n", c.Paint("host: "+cmd.SSHUser+"@"+cmd.Host, console.StyleDim))
			} else {
				fmt.Fprintf(w, "   %s\n", c.Paint("host: "+cmd.Host, console.StyleDim))
			}
			if cmd.SSHPort != 0 {
				fmt.Fprintf(w, "   %s\n", c.Paint(fmt.Sprintf("port: %d", cmd.SSHPort), console.StyleDim))
			}
		} else {
			fmt.Fprintf(w, "   %s\n", c.Paint("local: true", console.StyleDim))
		}
		if cmd.Script != "" {
			if cmd.Sensitive {
				fmt.Fprintf(w, "   %s\n", c.Paint("run: <sensitive command redacted>", console.StyleDim))
			} else {
				fmt.Fprintf(w, "   %s\n", c.Paint("run: "+cmd.Script, console.StyleDim))
			}
		}
	}
}
