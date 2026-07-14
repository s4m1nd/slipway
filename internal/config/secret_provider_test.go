package config

import (
	"strings"
	"testing"
)

func TestValidateAcceptsDopplerSecretProvider(t *testing.T) {
	cfg, err := LoadBytes([]byte(dopplerProviderConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	provider := cfg.Secrets.Provider
	if provider.Type != "doppler" || provider.Project != "backend" || provider.Config != "prd" {
		t.Fatalf("provider = %#v", provider)
	}
}

func TestValidateDopplerSecretProviderRequiresProjectAndConfig(t *testing.T) {
	for _, field := range []string{"project", "config"} {
		t.Run(field, func(t *testing.T) {
			cfg, err := LoadBytes([]byte(dopplerProviderConfigYAML))
			if err != nil {
				t.Fatalf("LoadBytes returned error: %v", err)
			}
			switch field {
			case "project":
				cfg.Secrets.Provider.Project = ""
			case "config":
				cfg.Secrets.Provider.Config = ""
			}

			err = Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), "secrets.provider."+field+" is required") {
				t.Fatalf("Validate error = %v, want missing %s", err, field)
			}
		})
	}
}

func TestValidateRejectsProviderSpecificFieldsFromAnotherAdapter(t *testing.T) {
	cfg, err := LoadBytes([]byte(dopplerProviderConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	cfg.Secrets.Provider.Vault = "wrong-provider-field"

	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), `secrets.provider.vault cannot be set when type is "doppler"`) {
		t.Fatalf("Validate error = %v, want provider-specific field error", err)
	}

	cfg, err = LoadBytes([]byte(onePasswordProviderConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	cfg.Secrets.Provider.Project = "backend"

	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), `secrets.provider.project cannot be set when type is "1password"`) {
		t.Fatalf("Validate error = %v, want provider-specific field error", err)
	}
}

func TestValidateRejectsFetchWithPartiallyConfiguredProvider(t *testing.T) {
	cfg, err := LoadBytes([]byte(dopplerProviderConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	cfg.Secrets.Fetch = "printf 'REGISTRY_PASSWORD=value\\n'"
	cfg.Secrets.Provider.Type = ""

	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "secrets.fetch and secrets.provider cannot both be set") {
		t.Fatalf("Validate error = %v, want fetch/provider conflict", err)
	}
}

func TestValidateUnsupportedProviderListsSupportedAdapters(t *testing.T) {
	cfg, err := LoadBytes([]byte(dopplerProviderConfigYAML))
	if err != nil {
		t.Fatalf("LoadBytes returned error: %v", err)
	}
	cfg.Secrets.Provider.Type = "unknown"

	err = Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), `must be "1password" or "doppler"`) {
		t.Fatalf("Validate error = %v, want supported provider list", err)
	}
}

const dopplerProviderConfigYAML = `project: demo

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
  provider:
    type: doppler
    project: backend
    config: prd
  names:
    - REGISTRY_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: root
    services:
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
        hosts: [app-1]
`

const onePasswordProviderConfigYAML = `project: demo

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
  provider:
    type: 1password
    account: account
    vault: vault
    item: item
  names:
    - REGISTRY_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: root
    services:
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
        hosts: [app-1]
`
