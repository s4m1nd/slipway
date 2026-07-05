package slipway

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeDocumentsOperatorRequirementsAndSupportedSecretModes(t *testing.T) {
	readme := readTextFile(t, "README.md")

	for _, want := range []string{
		"## Requirements",
		"Docker installed locally",
		"Docker available to the configured SSH user",
		"SSH access to each target server",
		"registry credentials",
		"Only built-in secret provider today is `1password`",
		"`secrets.fetch`",
		"`REGISTRY_PASSWORD`",
		"`slipway.example.yml`",
		"`slipway.live.example.yml`",
		"`examples/terraform/hetzner-single-node`",
		"`examples/live-nginx`",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q", want)
		}
	}
}

func TestGitignoreProtectsLocalSecretsAndTerraformArtifacts(t *testing.T) {
	gitignore, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	text := string(gitignore)

	for _, want := range []string{
		"slipway.live.yml",
		".public-hygiene-denylist",
		"**/.terraform/",
		"*.tfstate",
		"*.tfvars",
		"*.tfvars.json",
		"*.tfplan",
		"*.tfplan.json",
		".terraformrc",
		"terraform.rc",
		"!*.tfvars.example",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, text)
		}
	}
}
