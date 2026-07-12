package console

import (
	"io"
	"os"
	"strings"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

type Style string

const (
	StyleBold   Style = "1"
	StyleDim    Style = "2"
	StyleRed    Style = "31"
	StyleGreen  Style = "32"
	StyleYellow Style = "33"
	StyleBlue   Style = "34"
	StyleCyan   Style = "36"
)

func colorModeFromEnvironment() ColorMode {
	switch ColorMode(strings.ToLower(strings.TrimSpace(os.Getenv("SLIPWAY_COLOR")))) {
	case ColorAlways:
		return ColorAlways
	case ColorNever:
		return ColorNever
	default:
		return ColorAuto
	}
}

func colorEnabled(w io.Writer, mode ColorMode) bool {
	switch mode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	}
	if _, present := os.LookupEnv("NO_COLOR"); present {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
