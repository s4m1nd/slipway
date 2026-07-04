package slipway

import (
	"bufio"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var ipv4RE = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
var literalOnePasswordMetadataRE = regexp.MustCompile(`(?m)^\s*(account|vault|item):\s*[A-Za-z0-9]{20,}\s*$`)

func TestPublicRepoHygiene(t *testing.T) {
	denylist := loadLocalDenylist(t)
	var matches []string

	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch path {
			case ".git", ".tmp", "bin", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Base(path) {
		case ".env", "slipway.live.yml", "slipway.local.yml", ".public-hygiene-denylist":
			return nil
		}
		if !shouldCheckPublicHygiene(path) {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)

		for _, ip := range ipv4RE.FindAllString(text, -1) {
			if isNonDocumentationPublicIPv4(ip) {
				matches = append(matches, path+": non-documentation public IPv4 address")
			}
		}
		if literalOnePasswordMetadataRE.MatchString(text) {
			matches = append(matches, path+": literal 1Password metadata value")
		}
		for i, needle := range denylist {
			if needle != "" && strings.Contains(text, needle) {
				matches = append(matches, path+": local private denylist entry "+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repo public hygiene: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("public hygiene problems found:\n%s", strings.Join(matches, "\n"))
	}
}

func shouldCheckPublicHygiene(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".sh", ".md", ".yml", ".yaml", ".mod", ".sum", "":
		return true
	default:
		return false
	}
}

func loadLocalDenylist(t *testing.T) []string {
	t.Helper()

	var out []string
	addLines := func(source string, text string) {
		scanner := bufio.NewScanner(strings.NewReader(text))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			out = append(out, line)
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("read %s: %v", source, err)
		}
	}

	if env := os.Getenv("SLIPWAY_PUBLIC_HYGIENE_DENYLIST"); env != "" {
		addLines("SLIPWAY_PUBLIC_HYGIENE_DENYLIST", env)
	}
	if data, err := os.ReadFile(".public-hygiene-denylist"); err == nil {
		addLines(".public-hygiene-denylist", string(data))
	} else if !os.IsNotExist(err) {
		t.Fatalf("read .public-hygiene-denylist: %v", err)
	}
	return out
}

func isNonDocumentationPublicIPv4(value string) bool {
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return false
	}
	for _, prefix := range allowedIPv4Prefixes() {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func allowedIPv4Prefixes() []netip.Prefix {
	cidrs := []string{
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"255.255.255.255/32",
	}

	out := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		out = append(out, netip.MustParsePrefix(cidr))
	}
	return out
}

func TestIsNonDocumentationPublicIPv4(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "public routable address", ip: netip.AddrFrom4([4]byte{8, 8, 8, 8}).String(), want: true},
		{name: "documentation address", ip: "192.0.2.10", want: false},
		{name: "private address", ip: "10.0.0.1", want: false},
		{name: "invalid address", ip: "999.1.1.1", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNonDocumentationPublicIPv4(tc.ip); got != tc.want {
				t.Fatalf("isNonDocumentationPublicIPv4(%q) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

func TestLiteralOnePasswordMetadataPattern(t *testing.T) {
	if !literalOnePasswordMetadataRE.MatchString("account: ABCDEFGHIJKLMNOPQRSTUVWX") {
		t.Fatal("expected literal 1Password account metadata to match")
	}
	if literalOnePasswordMetadataRE.MatchString("note: ABCDEFGHIJKLMNOPQRSTUVWX") {
		t.Fatal("did not expect unrelated keys to match")
	}
}
