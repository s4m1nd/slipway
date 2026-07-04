package release

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type Color string

const (
	Blue  Color = "blue"
	Green Color = "green"
)

var hexSHARE = regexp.MustCompile(`^[0-9a-fA-F]+$`)

type Release struct {
	ID        string
	CreatedAt time.Time
	GitSHA    string
}

func New(now time.Time) Release {
	sha := strings.TrimSpace(os.Getenv("GITHUB_SHA"))
	if sha == "" {
		sha = strings.TrimSpace(os.Getenv("SLIPWAY_GIT_SHA"))
	}
	id := now.UTC().Format("20060102T150405.000000000Z")
	short := safeSHASuffix(sha)
	if short != "" {
		id = fmt.Sprintf("%s-%s", id, short)
	}
	return Release{ID: id, CreatedAt: now.UTC(), GitSHA: short}
}

func safeSHASuffix(sha string) string {
	if sha == "" || !hexSHARE.MatchString(sha) {
		return ""
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func Opposite(color Color) Color {
	if color == Blue {
		return Green
	}
	return Blue
}

func ParseColor(value string) (Color, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "blue":
		return Blue, true
	case "green":
		return Green, true
	default:
		return "", false
	}
}
