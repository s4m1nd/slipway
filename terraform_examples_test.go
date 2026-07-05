package slipway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHetznerTerraformSSHKeyCanBeInlineOrFileBacked(t *testing.T) {
	dir := filepath.Join("examples", "terraform", "hetzner-single-node")
	mainTF := readTextFile(t, filepath.Join(dir, "main.tf"))
	variablesTF := readTextFile(t, filepath.Join(dir, "variables.tf"))
	tfvarsExample := readTextFile(t, filepath.Join(dir, "terraform.tfvars.example"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	for _, want := range []string{
		`variable "ssh_public_key"`,
		`variable "ssh_public_key_path"`,
		`default     = ""`,
	} {
		if !strings.Contains(variablesTF, want) {
			t.Fatalf("variables.tf missing %q:\n%s", want, variablesTF)
		}
	}

	for _, want := range []string{
		`try(file(pathexpand(var.ssh_public_key_path)), "")`,
		`local.configured_ssh_public_key != "" ? local.configured_ssh_public_key : local.file_ssh_public_key`,
		`Set ssh_public_key or set ssh_public_key_path to an existing .pub file.`,
	} {
		if !strings.Contains(mainTF, want) {
			t.Fatalf("main.tf missing %q:\n%s", want, mainTF)
		}
	}
	if strings.Contains(mainTF, `trimspace(file(pathexpand(var.ssh_public_key_path)))`) {
		t.Fatalf("main.tf should not eagerly read a missing SSH public key file:\n%s", mainTF)
	}

	for _, want := range []string{
		`ssh_public_key      = ""`,
		`ssh_public_key_path = "~/.ssh/id_ed25519.pub"`,
	} {
		if !strings.Contains(tfvarsExample, want) {
			t.Fatalf("terraform.tfvars.example missing %q:\n%s", want, tfvarsExample)
		}
	}

	for _, want := range []string{
		"1Password SSH agent",
		"ssh-add -L",
		"ssh_public_key",
		"ssh_public_key_path",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q:\n%s", want, readme)
		}
	}
}

func TestHetznerTerraformCanReuseExistingSSHKey(t *testing.T) {
	dir := filepath.Join("examples", "terraform", "hetzner-single-node")
	mainTF := readTextFile(t, filepath.Join(dir, "main.tf"))
	variablesTF := readTextFile(t, filepath.Join(dir, "variables.tf"))
	tfvarsExample := readTextFile(t, filepath.Join(dir, "terraform.tfvars.example"))
	readme := readTextFile(t, filepath.Join(dir, "README.md"))

	for _, want := range []string{
		`variable "existing_ssh_key_name"`,
		`Existing Hetzner Cloud SSH key name`,
	} {
		if !strings.Contains(variablesTF, want) {
			t.Fatalf("variables.tf missing %q:\n%s", want, variablesTF)
		}
	}

	for _, want := range []string{
		`use_existing_ssh_key`,
		`local.existing_ssh_key_name != ""`,
		`data "hcloud_ssh_key" "admin"`,
		`count = local.use_existing_ssh_key ? 1 : 0`,
		`resource "hcloud_ssh_key" "admin"`,
		`count      = local.use_existing_ssh_key ? 0 : 1`,
		`ssh_key_ids`,
		`[data.hcloud_ssh_key.admin[0].id]`,
		`[hcloud_ssh_key.admin[0].id]`,
		`data.hcloud_ssh_key.admin[0].public_key`,
		`ssh_keys     = local.ssh_key_ids`,
	} {
		if !strings.Contains(mainTF, want) {
			t.Fatalf("main.tf missing %q:\n%s", want, mainTF)
		}
	}

	if !strings.Contains(tfvarsExample, `existing_ssh_key_name = ""`) {
		t.Fatalf("terraform.tfvars.example missing existing_ssh_key_name:\n%s", tfvarsExample)
	}

	for _, want := range []string{
		"SSH key not unique",
		"existing_ssh_key_name",
		"hcloud ssh-key list",
		"curl -fsS",
		"https://api.hetzner.cloud/v1/ssh_keys",
		"Authorization: Bearer $HCLOUD_TOKEN",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q:\n%s", want, readme)
		}
	}
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
