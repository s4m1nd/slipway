package config

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	safeNameRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	envNameRE       = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	buildPlatformRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)?$`)
)

// ValidationError collects configuration problems so validate can show all
// actionable issues at once.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return strings.Join(e.Problems, "\n")
}

func (e *ValidationError) add(format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf(format, args...))
}

func (e *ValidationError) empty() bool { return len(e.Problems) == 0 }

func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

func Validate(cfg Config) error {
	ve := &ValidationError{}

	validateSafeName(ve, "project", cfg.Project)
	validateRetention(ve, "retention", cfg.Retention)
	if strings.TrimSpace(cfg.Defaults.Root) == "" {
		ve.add("internal root default is required")
	}

	validateSecrets(ve, "secrets", cfg.Secrets)
	declaredSecrets := secretSet(cfg.Secrets.Names)
	validateRegistry(ve, cfg.Registry, declaredSecrets)

	if len(cfg.Environments) == 0 {
		ve.add("at least one environment is required")
	}
	for envName, env := range cfg.Environments {
		validateSafeName(ve, "environments."+envName, envName)
		validateEnvironment(ve, envName, env, declaredSecrets)
	}

	if ve.empty() {
		return nil
	}
	return ve
}

func ValidateEnv(cfg Config, envName string) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	if _, ok := cfg.Environments[envName]; !ok {
		return &ValidationError{Problems: []string{fmt.Sprintf("environment %q was not found", envName)}}
	}
	return nil
}

func ValidateSecretsSelection(secrets Secrets, names []string) error {
	ve := &ValidationError{}
	validateSecrets(ve, "secrets", secrets)
	validateSecretReferences(ve, "requested secrets", names, secretSet(secrets.Names))
	if ve.empty() {
		return nil
	}
	return ve
}

func ApplyDefaults(cfg Config) Config {
	cfg.Registry = applyRegistryDefaults(cfg.Registry)
	if !cfg.Retention.releasesSet {
		cfg.Retention.Releases = 5
	}
	if cfg.Defaults.Root == "" {
		cfg.Defaults.Root = "/opt/slipway"
	}
	for envName, env := range cfg.Environments {
		if !env.Retention.releasesSet {
			env.Retention = cfg.Retention
		}
		if env.Proxy.ListenHTTP == "" {
			env.Proxy.ListenHTTP = ":80"
		}
		if env.Proxy.ListenHTTPS == "" {
			env.Proxy.ListenHTTPS = ":443"
		}
		for serverName, server := range env.Servers {
			if server.SSHPort == 0 {
				server.SSHPort = 22
			}
			env.Servers[serverName] = server
		}
		defaultHosts := singleServerName(env.Servers)
		for serviceName, service := range env.Services {
			if len(service.Hosts) == 0 && defaultHosts != "" {
				service.Hosts = []string{defaultHosts}
			}
			service = applyServiceDefaults(service)
			env.Services[serviceName] = service
		}
		cfg.Environments[envName] = env
	}
	return cfg
}

func applyRegistryDefaults(registry Registry) Registry {
	if strings.TrimSpace(registry.PasswordSecret) == "" && len(registry.Password) == 1 {
		registry.PasswordSecret = registry.Password[0]
		registry.Password = nil
	}
	return registry
}

func applyServiceDefaults(service Service) Service {
	if service.HealthCheck.Port == 0 && service.InternalPort > 0 {
		service.HealthCheck.Port = service.InternalPort
	}
	service.HealthCheck = applyHealthCheckDefaults(service.HealthCheck)
	return service
}

func applyHealthCheckDefaults(health HealthCheck) HealthCheck {
	if health.Interval == "" {
		health.Interval = "5s"
	}
	if health.Timeout == "" {
		health.Timeout = "2s"
	}
	if health.Retries == 0 {
		health.Retries = 12
	}
	return health
}

func validateRegistry(ve *ValidationError, registry Registry, declaredSecrets map[string]bool) {
	if strings.TrimSpace(registry.Server) == "" {
		ve.add("registry.server is required")
	}
	if strings.TrimSpace(registry.Username) == "" {
		ve.add("registry.username is required")
	}

	passwordSecret := strings.TrimSpace(registry.PasswordSecret)
	if passwordSecret != "" && len(registry.Password) > 0 {
		ve.add("registry.password and registry.password_secret cannot both be set")
		return
	}
	if passwordSecret == "" {
		switch len(registry.Password) {
		case 0:
			ve.add("registry.password_secret or registry.password is required")
			return
		case 1:
			passwordSecret = strings.TrimSpace(registry.Password[0])
		default:
			ve.add("registry.password must contain exactly one secret name")
			return
		}
	}
	validateSecretReferences(ve, "registry password", []string{passwordSecret}, declaredSecrets)
}

func validateRetention(ve *ValidationError, path string, retention Retention) {
	if retention.Releases < 2 {
		ve.add("%s.releases must be at least 2", path)
	}
}

func validateEnvironment(ve *ValidationError, envName string, env Environment, declaredSecrets map[string]bool) {
	path := fmt.Sprintf("environments.%s", envName)
	validateRetention(ve, path+".retention", env.Retention)
	if len(env.Servers) == 0 {
		ve.add("%s.servers must contain at least one server", path)
	}
	for serverName, server := range env.Servers {
		serverPath := fmt.Sprintf("%s.servers.%s", path, serverName)
		validateSafeName(ve, serverPath, serverName)
		if strings.TrimSpace(server.Host) == "" {
			ve.add("%s.host is required", serverPath)
		}
		if server.SSHPort <= 0 || server.SSHPort > 65535 {
			ve.add("%s.host_ssh_port must be between 1 and 65535", serverPath)
		}
	}

	serverNames := serverSet(env.Servers)
	if len(env.Services) == 0 {
		ve.add("%s.services must contain at least one service", path)
	}
	for serviceName, service := range env.Services {
		validateService(ve, path+".services."+serviceName, serviceName, service, serverNames, declaredSecrets)
	}
	validateProxy(ve, path, env)
}

func validateProxy(ve *ValidationError, path string, env Environment) {
	if _, err := ParseListenPort(env.Proxy.ListenHTTP); err != nil {
		ve.add("%s.proxy.listen_http must be a valid listen address, got %q", path, env.Proxy.ListenHTTP)
	}
	if _, err := ParseListenPort(env.Proxy.ListenHTTPS); err != nil {
		ve.add("%s.proxy.listen_https must be a valid listen address, got %q", path, env.Proxy.ListenHTTPS)
	}
	for i, route := range env.Proxy.Routes {
		routePath := fmt.Sprintf("%s.proxy.routes[%d]", path, i)
		if strings.TrimSpace(route.Host) == "" {
			ve.add("%s.host is required", routePath)
		} else if containsConfigUnsafeChars(route.Host) || strings.ContainsAny(route.Host, "{}\"`\\;") {
			ve.add("%s.host contains unsupported characters", routePath)
		}
		if strings.TrimSpace(route.Service) == "" {
			ve.add("%s.service is required", routePath)
			continue
		}
		service, ok := env.Services[route.Service]
		if !ok {
			ve.add("%s.service references unknown service %q", routePath, route.Service)
			continue
		}
		if service.InternalPort <= 0 {
			ve.add("%s.service %q must set internal_port when routed through the proxy", routePath, route.Service)
		}
		if !hasHealthCheck(service.HealthCheck) {
			ve.add("%s.service %q must set health_check.path before proxy traffic can switch", routePath, route.Service)
		}
	}
}

func validateService(ve *ValidationError, path string, name string, service Service, serverNames map[string]bool, declaredSecrets map[string]bool) {
	validateSafeName(ve, path, name)
	if strings.TrimSpace(service.Image) == "" {
		ve.add("%s.image is required", path)
	}
	if strings.TrimSpace(service.Build.Context) == "" {
		ve.add("%s.build.context is required", path)
	}
	validateBuild(ve, path+".build", service.Build)
	validateHostReferences(ve, path+".hosts", service.Hosts, serverNames)
	if service.InternalPort < 0 {
		ve.add("%s.internal_port must not be negative", path)
	}
	validateHealthCheck(ve, path+".health_check", service.HealthCheck)
	validateEnvVars(ve, path+".env", service.Env)
	validateEnvSecretDuplicates(ve, path, service)
	validateSecretReferences(ve, path+".secrets", service.Secrets, declaredSecrets)
}

func validateBuild(ve *ValidationError, path string, build Build) {
	platform := strings.TrimSpace(build.Platform)
	if platform == "" {
		return
	}
	if !buildPlatformRE.MatchString(platform) {
		ve.add("%s.platform must use os/arch or os/arch/variant format, got %q", path, build.Platform)
	}
}

func validateHostReferences(ve *ValidationError, path string, hosts []string, serverNames map[string]bool) {
	if len(hosts) == 0 {
		ve.add("%s must contain at least one server name", path)
		return
	}
	for _, host := range hosts {
		validateSafeName(ve, path+"."+host, host)
		if !serverNames[host] {
			ve.add("%s references unknown server %q", path, host)
		}
	}
}

func validateHealthCheck(ve *ValidationError, path string, health HealthCheck) {
	if strings.TrimSpace(health.Path) != "" {
		if !strings.HasPrefix(health.Path, "/") {
			ve.add("%s.path must start with /", path)
		}
		if containsConfigUnsafeChars(health.Path) || strings.ContainsAny(health.Path, "\"`\\") {
			ve.add("%s.path contains unsupported characters", path)
		}
	}
	if health.Port < 0 {
		ve.add("%s.port must not be negative", path)
	}
	if health.Retries < 1 {
		ve.add("%s.retries must be at least 1", path)
	}
	validateDuration(ve, path+".interval", health.Interval)
	validateDuration(ve, path+".timeout", health.Timeout)
}

func validateSecrets(ve *ValidationError, path string, secrets Secrets) {
	validateSecretProvider(ve, path, secrets)
	for _, name := range secrets.Names {
		if !envNameRE.MatchString(name) {
			ve.add("%s.names contains invalid secret name %q", path, name)
		}
	}
}

func validateSecretProvider(ve *ValidationError, path string, secrets Secrets) {
	provider := secrets.Provider
	providerType := strings.TrimSpace(provider.Type)
	if strings.TrimSpace(secrets.Fetch) != "" && providerType != "" {
		ve.add("%s.fetch and %s.provider cannot both be set", path, path)
		return
	}
	if providerType == "" {
		return
	}
	if providerType != "1password" {
		ve.add("%s.provider.type must be \"1password\", got %q", path, provider.Type)
		return
	}
	providerPath := path + ".provider"
	if strings.TrimSpace(provider.Account) == "" {
		ve.add("%s.account is required", providerPath)
	}
	if strings.TrimSpace(provider.Vault) == "" {
		ve.add("%s.vault is required", providerPath)
	}
	if strings.TrimSpace(provider.Item) == "" {
		ve.add("%s.item is required", providerPath)
	}
	if containsConfigUnsafeChars(provider.Account) || strings.ContainsAny(provider.Account, "\"`\\;/") {
		ve.add("%s.account contains unsupported characters", providerPath)
	}
	if containsConfigUnsafeChars(provider.Vault) || strings.ContainsAny(provider.Vault, "\"`\\;") {
		ve.add("%s.vault contains unsupported characters", providerPath)
	}
	if containsConfigUnsafeChars(provider.Item) || strings.ContainsAny(provider.Item, "\"`\\;") {
		ve.add("%s.item contains unsupported characters", providerPath)
	}
	if strings.ContainsAny(provider.FieldPrefix, "\n\r/") {
		ve.add("%s.field_prefix contains unsupported characters", providerPath)
	}
}

func validateSecretReferences(ve *ValidationError, path string, refs []string, declaredSecrets map[string]bool) {
	for _, secret := range refs {
		if !envNameRE.MatchString(secret) {
			ve.add("%s contains invalid secret name %q", path, secret)
			continue
		}
		if !declaredSecrets[secret] {
			ve.add("%s references undefined top-level secrets.names entry %q", path, secret)
		}
	}
}

func validateEnvVars(ve *ValidationError, path string, env map[string]string) {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !envNameRE.MatchString(key) {
			ve.add("%s contains invalid environment variable name %q", path, key)
			continue
		}
		if strings.ContainsAny(env[key], "\n\r") {
			ve.add("%s.%s contains unsupported newline", path, key)
		}
	}
}

func validateEnvSecretDuplicates(ve *ValidationError, path string, service Service) {
	if len(service.Env) == 0 || len(service.Secrets) == 0 {
		return
	}
	envNames := map[string]bool{}
	for key := range service.Env {
		envNames[key] = true
	}
	for _, secret := range service.Secrets {
		if envNames[secret] {
			ve.add("%s defines %q in both env and secrets", path, secret)
		}
	}
}

func validateSafeName(ve *ValidationError, path string, name string) {
	if strings.TrimSpace(name) == "" {
		ve.add("%s is required", path)
		return
	}
	if !safeNameRE.MatchString(name) {
		ve.add("%s must start with a lowercase letter or number and contain only lowercase letters, numbers, '-' or '_'; got %q", path, name)
	}
}

func containsConfigUnsafeChars(value string) bool {
	return strings.ContainsAny(value, "\n\r\t$") || strings.Contains(value, " ")
}

func validateDuration(ve *ValidationError, path string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		ve.add("%s must be a valid duration, got %q", path, value)
		return
	}
	if duration <= 0 {
		ve.add("%s must be greater than 0, got %q", path, value)
	}
}

func secretSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		out[name] = true
	}
	return out
}

func serverSet(servers map[string]Server) map[string]bool {
	out := map[string]bool{}
	for name := range servers {
		out[name] = true
	}
	return out
}

func singleServerName(servers map[string]Server) string {
	if len(servers) != 1 {
		return ""
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0]
}

func hasHealthCheck(health HealthCheck) bool {
	return strings.TrimSpace(health.Path) != ""
}

func ParseListenPort(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty listen address")
	}
	if strings.HasPrefix(value, ":") {
		return parsePort(strings.TrimPrefix(value, ":"))
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		_ = host
		return parsePort(port)
	}
	return parsePort(value)
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	return port, nil
}

func (p Proxy) HTTPPort() int {
	port, err := ParseListenPort(p.ListenHTTP)
	if err != nil {
		return 80
	}
	return port
}

func (p Proxy) HTTPSPort() int {
	port, err := ParseListenPort(p.ListenHTTPS)
	if err != nil {
		return 443
	}
	return port
}
