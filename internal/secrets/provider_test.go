package secrets

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/s4m1nd/slipway/internal/config"
)

func TestNewResolverUsesEnvironmentWhenFetchIsEmpty(t *testing.T) {
	t.Setenv("REGISTRY_PASSWORD", "registry-secret")
	t.Setenv("DATABASE_URL", "database-secret")

	resolver, err := NewResolver(config.Secrets{
		Names: []string{"REGISTRY_PASSWORD", "DATABASE_URL"},
	})
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	if _, ok := resolver.(EnvResolver); !ok {
		t.Fatalf("resolver = %T, want EnvResolver", resolver)
	}

	resolved, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD", "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved["REGISTRY_PASSWORD"] != "registry-secret" || resolved["DATABASE_URL"] != "database-secret" {
		t.Fatalf("resolved secrets = %#v", resolved)
	}
}

func TestEnvResolverRejectsMultilineSecretValues(t *testing.T) {
	t.Setenv("REGISTRY_PASSWORD", "registry-secret\nMALICIOUS=true")

	resolver := EnvResolver{}
	_, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD"})
	if err == nil {
		t.Fatal("expected multiline env secret to fail")
	}
	if !strings.Contains(err.Error(), "REGISTRY_PASSWORD") || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected newline error naming REGISTRY_PASSWORD, got: %v", err)
	}
	if strings.Contains(err.Error(), "registry-secret") {
		t.Fatalf("error leaked secret value: %v", err)
	}
}

func TestCommandResolverRejectsMultilineSecretValues(t *testing.T) {
	resolver := CommandResolver{Command: "printf 'REGISTRY_PASSWORD=registry-secret\\rMALICIOUS=true\\n'"}

	_, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD"})
	if err == nil {
		t.Fatal("expected multiline command secret to fail")
	}
	if !strings.Contains(err.Error(), "REGISTRY_PASSWORD") || !strings.Contains(err.Error(), "newline") {
		t.Fatalf("expected newline error naming REGISTRY_PASSWORD, got: %v", err)
	}
	if strings.Contains(err.Error(), "registry-secret") {
		t.Fatalf("error leaked secret value: %v", err)
	}
}

func TestCommandResolverPreservesSecretValueWhitespace(t *testing.T) {
	resolver := CommandResolver{Command: "printf 'REGISTRY_PASSWORD=  registry-secret  \\n'"}

	resolved, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if got := resolved["REGISTRY_PASSWORD"]; got != "  registry-secret  " {
		t.Fatalf("REGISTRY_PASSWORD = %q, want surrounding spaces preserved", got)
	}
}

func TestNewResolverUsesOnePasswordProvider(t *testing.T) {
	resolver, err := NewResolver(config.Secrets{
		Provider: config.SecretProvider{
			Type:    "1password",
			Account: "acct",
			Vault:   "vault",
			Item:    "item",
		},
		Names: []string{"REGISTRY_PASSWORD"},
	})
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	opResolver, ok := resolver.(OnePasswordResolver)
	if !ok {
		t.Fatalf("resolver = %T, want OnePasswordResolver", resolver)
	}
	if opResolver.Account != "acct" || opResolver.Vault != "vault" || opResolver.Item != "item" {
		t.Fatalf("resolver = %#v", opResolver)
	}
}

func TestOnePasswordResolverReadsRequestedSecrets(t *testing.T) {
	binDir := t.TempDir()
	opPath := filepath.Join(binDir, "op")
	if runtime.GOOS == "windows" {
		opPath += ".bat"
	}
	script := `#!/bin/sh
set -eu
if [ "$1" != "read" ]; then
  echo "unexpected command" >&2
  exit 10
fi
if [ "$3" != "--account" ] || [ "$4" != "acct" ]; then
  echo "unexpected account" >&2
  exit 11
fi
case "$2" in
  op://vault/item/REGISTRY_PASSWORD) printf 'registry-secret\n' ;;
  op://vault/item/DATABASE_URL) printf 'database-secret\n' ;;
  *) echo "unexpected ref $2" >&2; exit 12 ;;
esac
`
	if err := os.WriteFile(opPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake op: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	resolver := OnePasswordResolver{Account: "acct", Vault: "vault", Item: "item"}
	resolved, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD", "DATABASE_URL"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolved["REGISTRY_PASSWORD"] != "registry-secret" {
		t.Fatalf("REGISTRY_PASSWORD = %q", resolved["REGISTRY_PASSWORD"])
	}
	if resolved["DATABASE_URL"] != "database-secret" {
		t.Fatalf("DATABASE_URL = %q", resolved["DATABASE_URL"])
	}
}

func TestOnePasswordResolverDoesNotIncludeSecretOutputInErrors(t *testing.T) {
	binDir := t.TempDir()
	opPath := filepath.Join(binDir, "op")
	script := `#!/bin/sh
echo "super-secret-value" >&2
exit 1
`
	if err := os.WriteFile(opPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake op: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	resolver := OnePasswordResolver{Account: "acct", Vault: "vault", Item: "item"}
	_, err := resolver.Resolve(context.Background(), []string{"REGISTRY_PASSWORD"})
	if err == nil {
		t.Fatal("expected Resolve to fail")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Fatalf("error leaked command output: %v", err)
	}
}
