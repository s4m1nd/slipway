package console

import (
	"os"
	"strings"
	"testing"
)

func TestColorModes(t *testing.T) {
	var out strings.Builder
	if got := NewWithMode(&out, &out, ColorAuto); got.Colored {
		t.Fatal("auto color should be disabled for a non-terminal writer")
	}
	if got := NewWithMode(&out, &out, ColorAlways); !got.Colored {
		t.Fatal("always should enable color for a non-terminal writer")
	}
	if got := NewWithMode(os.Stdout, os.Stderr, ColorNever); got.Colored {
		t.Fatal("never should disable color")
	}
}

func TestAutoColorHonorsEnvironment(t *testing.T) {
	t.Setenv("SLIPWAY_COLOR", "auto")
	t.Setenv("NO_COLOR", "1")
	if got := New(os.Stdout, os.Stderr); got.Colored {
		t.Fatal("NO_COLOR should disable automatic color")
	}

	t.Setenv("SLIPWAY_COLOR", "always")
	if got := New(&strings.Builder{}, &strings.Builder{}); !got.Colored {
		t.Fatal("SLIPWAY_COLOR=always should override automatic detection")
	}
}

func TestAutoColorIsDisabledForDumbTerminals(t *testing.T) {
	oldNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatalf("unset NO_COLOR: %v", err)
	}
	t.Cleanup(func() {
		if hadNoColor {
			_ = os.Setenv("NO_COLOR", oldNoColor)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})
	t.Setenv("SLIPWAY_COLOR", "auto")
	t.Setenv("TERM", "dumb")
	if got := New(os.Stdout, os.Stderr); got.Colored {
		t.Fatal("TERM=dumb should disable automatic color")
	}
}

func TestPaintAndCommandRedaction(t *testing.T) {
	var out strings.Builder
	c := NewWithMode(&out, &out, ColorAlways)
	c.Verbose = true
	c.Success("healthy")
	c.Command("printf super-secret", true)

	got := out.String()
	if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "✓ healthy") {
		t.Fatalf("colored success output missing expected content: %q", got)
	}
	if strings.Contains(got, "super-secret") || !strings.Contains(got, "<sensitive command redacted>") {
		t.Fatalf("sensitive command was not redacted: %q", got)
	}
}
