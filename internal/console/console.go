package console

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type Console struct {
	Out     io.Writer
	Err     io.Writer
	Colored bool
	Verbose bool
}

type Field struct {
	Label  string
	Value  string
	Styles []Style
}

func New(out io.Writer, errOut io.Writer) Console {
	return NewWithMode(out, errOut, colorModeFromEnvironment())
}

func NewWithMode(out io.Writer, errOut io.Writer, mode ColorMode) Console {
	if out == nil {
		out = io.Discard
	}
	if errOut == nil {
		errOut = io.Discard
	}
	return Console{Out: out, Err: errOut, Colored: colorEnabled(out, mode)}
}

func (c Console) Paint(text string, styles ...Style) string {
	if !c.Colored || text == "" || len(styles) == 0 {
		return text
	}
	codes := make([]string, 0, len(styles))
	for _, style := range styles {
		codes = append(codes, string(style))
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

func (c Console) Title(text string) {
	if text != "" {
		fmt.Fprintln(c.Out, c.Paint(text, StyleBold, StyleCyan))
	}
}

func (c Console) Subtitle(label string, value string) {
	if value == "" {
		return
	}
	if label != "" {
		fmt.Fprintf(c.Out, "%s %s\n", c.Paint(label, StyleDim), value)
		return
	}
	fmt.Fprintln(c.Out, value)
}

func (c Console) Step(current int, total int, description string) {
	width := len(fmt.Sprintf("%d", total))
	if width < 1 {
		width = 1
	}
	label := fmt.Sprintf("%*d/%d", width, current, total)
	fmt.Fprintf(c.Out, "  %s  %s\n", c.Paint(label, StyleCyan), c.Paint(description, StyleBold))
}

func (c Console) Target(target string) {
	if target != "" {
		fmt.Fprintf(c.Out, "        %s\n", c.Paint(target, StyleDim))
	}
}

func (c Console) Success(text string) {
	c.success(text, "")
}

func (c Console) SuccessTimed(text string, elapsed time.Duration) {
	c.success(text, FormatElapsed(elapsed))
}

func (c Console) success(text string, elapsed string) {
	if text == "" {
		text = "complete"
	}
	fmt.Fprintf(c.Out, "        %s", c.Paint("✓ "+text, StyleGreen))
	if elapsed != "" {
		fmt.Fprintf(c.Out, " %s", c.Paint("("+elapsed+")", StyleDim))
	}
	fmt.Fprintln(c.Out)
}

func (c Console) Warning(text string) {
	fmt.Fprintf(c.Out, "        %s\n", c.Paint("! "+text, StyleYellow))
}

func (c Console) Failure(text string) {
	if text == "" {
		text = "failed"
	}
	fmt.Fprintf(c.Err, "%s\n", c.Paint("✗ "+text, StyleRed))
}

func (c Console) Command(script string, sensitive bool) {
	if !c.Verbose || script == "" {
		return
	}
	if sensitive {
		script = "<sensitive command redacted>"
	}
	fmt.Fprintf(c.Out, "        %s\n", c.Paint("run: "+script, StyleDim))
}

func (c Console) Completion(title string, fields ...Field) {
	fmt.Fprintln(c.Out)
	fmt.Fprintln(c.Out, c.Paint(title, StyleBold, StyleGreen))
	labelWidth := 0
	for _, field := range fields {
		if field.Value != "" && len(field.Label) > labelWidth {
			labelWidth = len(field.Label)
		}
	}
	for _, field := range fields {
		if field.Value == "" {
			continue
		}
		label := fmt.Sprintf("%-*s", labelWidth+1, field.Label+":")
		fmt.Fprintf(c.Out, "  %s %s\n", c.Paint(label, StyleDim), c.Paint(field.Value, field.Styles...))
	}
}

func FormatElapsed(elapsed time.Duration) string {
	if elapsed < time.Millisecond {
		return "<1ms"
	}
	if elapsed < time.Second {
		return elapsed.Round(time.Millisecond).String()
	}
	return elapsed.Round(100 * time.Millisecond).String()
}
