package config

import "strings"

func secretProviderConfigured(provider SecretProvider) bool {
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

func validateOnePasswordSecretProvider(ve *ValidationError, path string, provider SecretProvider) {
	if strings.TrimSpace(provider.Account) == "" {
		ve.add("%s.account is required", path)
	}
	if strings.TrimSpace(provider.Vault) == "" {
		ve.add("%s.vault is required", path)
	}
	if strings.TrimSpace(provider.Item) == "" {
		ve.add("%s.item is required", path)
	}
	if containsConfigUnsafeChars(provider.Account) || strings.ContainsAny(provider.Account, "\"`\\;/") {
		ve.add("%s.account contains unsupported characters", path)
	}
	if containsConfigUnsafeChars(provider.Vault) || strings.ContainsAny(provider.Vault, "\"`\\;") {
		ve.add("%s.vault contains unsupported characters", path)
	}
	if containsConfigUnsafeChars(provider.Item) || strings.ContainsAny(provider.Item, "\"`\\;") {
		ve.add("%s.item contains unsupported characters", path)
	}
	if strings.ContainsAny(provider.FieldPrefix, "\n\r/") {
		ve.add("%s.field_prefix contains unsupported characters", path)
	}
	if strings.TrimSpace(provider.Project) != "" {
		ve.add("%s.project cannot be set when type is \"1password\"", path)
	}
	if strings.TrimSpace(provider.Config) != "" {
		ve.add("%s.config cannot be set when type is \"1password\"", path)
	}
}

func validateDopplerSecretProvider(ve *ValidationError, path string, provider SecretProvider) {
	validateDopplerSelector(ve, path+".project", provider.Project)
	validateDopplerSelector(ve, path+".config", provider.Config)

	for _, field := range []struct {
		name  string
		value string
	}{
		{name: "account", value: provider.Account},
		{name: "vault", value: provider.Vault},
		{name: "item", value: provider.Item},
		{name: "field_prefix", value: provider.FieldPrefix},
	} {
		if strings.TrimSpace(field.value) != "" {
			ve.add("%s.%s cannot be set when type is \"doppler\"", path, field.name)
		}
	}
}

func validateDopplerSelector(ve *ValidationError, path string, value string) {
	if strings.TrimSpace(value) == "" {
		ve.add("%s is required", path)
		return
	}
	if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\n\r\t ") {
		ve.add("%s contains unsupported whitespace or control characters", path)
	}
}
