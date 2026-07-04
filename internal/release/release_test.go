package release

import (
	"regexp"
	"testing"
	"time"
)

func TestNewReleaseIDWithoutSHAUsesNanosecondTimestamp(t *testing.T) {
	t.Setenv("GITHUB_SHA", "")
	t.Setenv("SLIPWAY_GIT_SHA", "")

	rel := New(fixedTime())

	if rel.ID != "20260701T120000.123456789Z" {
		t.Fatalf("release ID = %q, want nanosecond timestamp", rel.ID)
	}
	assertSafeReleaseID(t, rel.ID)
}

func TestNewReleaseIDUsesValidGitHubSHASuffix(t *testing.T) {
	t.Setenv("GITHUB_SHA", "abcdef1234567890abcdef1234567890abcdef12")
	t.Setenv("SLIPWAY_GIT_SHA", "")

	rel := New(fixedTime())

	if rel.ID != "20260701T120000.123456789Z-abcdef123456" {
		t.Fatalf("release ID = %q, want 12-char GitHub suffix", rel.ID)
	}
	assertSafeReleaseID(t, rel.ID)
}

func TestNewReleaseIDFallsBackToSlipwayGitSHA(t *testing.T) {
	t.Setenv("GITHUB_SHA", "")
	t.Setenv("SLIPWAY_GIT_SHA", "1234567890abcdef")

	rel := New(fixedTime())

	if rel.ID != "20260701T120000.123456789Z-1234567890ab" {
		t.Fatalf("release ID = %q, want SLIPWAY_GIT_SHA suffix", rel.ID)
	}
	assertSafeReleaseID(t, rel.ID)
}

func TestNewReleaseIDPrefersGitHubSHA(t *testing.T) {
	t.Setenv("GITHUB_SHA", "aaaaaaaaaaaabbbb")
	t.Setenv("SLIPWAY_GIT_SHA", "bbbbbbbbbbbbaaaa")

	rel := New(fixedTime())

	if rel.ID != "20260701T120000.123456789Z-aaaaaaaaaaaa" {
		t.Fatalf("release ID = %q, want GitHub suffix to win", rel.ID)
	}
	assertSafeReleaseID(t, rel.ID)
}

func TestNewReleaseIDIgnoresMalformedSHAs(t *testing.T) {
	for _, value := range []string{
		"abc123; touch /tmp/slipway-pwned",
		"abc123 with-space",
		"abc123:latest",
		"abc123/branch",
		"abc123`touch /tmp/slipway-pwned`",
		"abc123☃",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GITHUB_SHA", value)
			t.Setenv("SLIPWAY_GIT_SHA", "")

			rel := New(fixedTime())

			if rel.ID != "20260701T120000.123456789Z" {
				t.Fatalf("release ID = %q, want malformed SHA omitted", rel.ID)
			}
			assertSafeReleaseID(t, rel.ID)
		})
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 123456789, time.UTC)
}

func assertSafeReleaseID(t *testing.T, id string) {
	t.Helper()
	re := regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z(-[0-9a-fA-F]{1,12})?$`)
	if !re.MatchString(id) {
		t.Fatalf("release ID %q does not match safe pattern", id)
	}
}
