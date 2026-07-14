package secrets

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/s4m1nd/slipway/internal/config"
)

// Resolver resolves named secrets at deploy time. Resolved values should never
// be printed; command plans mark secret writes as Sensitive.
type Resolver interface {
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

func NewResolver(spec config.Secrets) (Resolver, error) {
	providerConfigured := secretProviderConfigured(spec.Provider)
	if strings.TrimSpace(spec.Fetch) != "" && providerConfigured {
		return nil, fmt.Errorf("secrets.fetch and secrets.provider cannot both be set")
	}
	switch strings.TrimSpace(spec.Provider.Type) {
	case "":
		if providerConfigured {
			return nil, fmt.Errorf("secrets.provider.type is required")
		}
		// Continue below.
	case "1password":
		return OnePasswordResolver{
			Account:     spec.Provider.Account,
			Vault:       spec.Provider.Vault,
			Item:        spec.Provider.Item,
			FieldPrefix: spec.Provider.FieldPrefix,
		}, nil
	case "doppler":
		project := strings.TrimSpace(spec.Provider.Project)
		configName := strings.TrimSpace(spec.Provider.Config)
		if project == "" {
			return nil, fmt.Errorf("secrets.provider.project is required for Doppler")
		}
		if configName == "" {
			return nil, fmt.Errorf("secrets.provider.config is required for Doppler")
		}
		return DopplerResolver{Project: project, Config: configName}, nil
	default:
		return nil, fmt.Errorf("unsupported secret provider %q", spec.Provider.Type)
	}
	if strings.TrimSpace(spec.Fetch) == "" {
		return EnvResolver{}, nil
	}
	return CommandResolver{Command: spec.Fetch}, nil
}

func secretProviderConfigured(provider config.SecretProvider) bool {
	for _, value := range []string{
		provider.Type,
		provider.Account,
		provider.Vault,
		provider.Item,
		provider.FieldPrefix,
		provider.Project,
		provider.Config,
	} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

type EnvResolver struct{}

func (r EnvResolver) Resolve(_ context.Context, names []string) (map[string]string, error) {
	resolved := map[string]string{}
	var missing []string
	for _, name := range names {
		value, ok := os.LookupEnv(name)
		if !ok {
			missing = append(missing, name)
			continue
		}
		if err := validateEnvFileSecret(name, value); err != nil {
			return nil, err
		}
		resolved[name] = value
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing secrets: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

// CommandResolver runs a wrapper command that prints KEY=VALUE lines. The
// command receives SLIPWAY_SECRET_NAMES as a comma-separated requested list.
type CommandResolver struct {
	Command string
}

func (r CommandResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", r.Command)
	cmd.Env = append(os.Environ(), "SLIPWAY_SECRET_NAMES="+strings.Join(names, ","))
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run secret provider command: %w", err)
	}
	resolved := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		raw := strings.TrimRight(scanner.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("secret provider output must be KEY=VALUE lines")
		}
		key = strings.TrimSpace(key)
		if err := validateEnvFileSecret(key, value); err != nil {
			return nil, err
		}
		resolved[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var missing []string
	for _, name := range names {
		if _, ok := resolved[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("secret provider did not return: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

type OnePasswordResolver struct {
	Account     string
	Vault       string
	Item        string
	FieldPrefix string
}

func (r OnePasswordResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	resolved := map[string]string{}
	for _, name := range names {
		value, err := r.readSecret(ctx, name)
		if err != nil {
			return nil, err
		}
		resolved[name] = value
	}
	return resolved, nil
}

func (r OnePasswordResolver) readSecret(ctx context.Context, name string) (string, error) {
	ref := fmt.Sprintf("op://%s/%s/%s%s", r.Vault, r.Item, r.FieldPrefix, name)
	cmd := exec.CommandContext(ctx, "op", "read", ref, "--account", r.Account)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read 1Password secret %s: %w", name, err)
	}
	value := strings.TrimSuffix(string(output), "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("read 1Password secret %s: value contains unsupported newline", name)
	}
	return value, nil
}

// DopplerResolver reads only the requested names from a configured Doppler
// project and config. The Doppler CLI handles authentication, usually through
// DOPPLER_TOKEN in automation or a local Doppler login session.
type DopplerResolver struct {
	Project string
	Config  string
}

type dopplerSecret struct {
	Computed *string `json:"computed"`
}

func (r DopplerResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	resolved := map[string]string{}
	if len(names) == 0 {
		return resolved, nil
	}

	args := []string{
		"--no-check-version",
		"--silent",
		"--json",
		"secrets",
		"get",
		"--project", r.Project,
		"--config", r.Config,
	}
	args = append(args, names...)
	cmd := exec.CommandContext(ctx, "doppler", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read Doppler secrets for project %q config %q: %w", r.Project, r.Config, err)
	}

	var response map[string]dopplerSecret
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode Doppler secret response: %w", err)
	}

	var unavailable []string
	for _, name := range names {
		secret, ok := response[name]
		if !ok || secret.Computed == nil {
			unavailable = append(unavailable, name)
			continue
		}
		if err := validateEnvFileSecret(name, *secret.Computed); err != nil {
			return nil, err
		}
		resolved[name] = *secret.Computed
	}
	if len(unavailable) > 0 {
		return nil, fmt.Errorf("Doppler did not return readable values for: %s", strings.Join(unavailable, ", "))
	}
	return resolved, nil
}

func validateEnvFileSecret(name string, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("secret %s contains unsupported newline", name)
	}
	return nil
}
