package planner

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/s4m1nd/slipway/internal/accessory"
	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/proxy"
	"github.com/s4m1nd/slipway/internal/release"
	"github.com/s4m1nd/slipway/internal/remote"
	iruntime "github.com/s4m1nd/slipway/internal/runtime"
)

type Planner struct {
	Config  config.Config
	EnvName string
	Env     config.Environment
	Secrets map[string]string

	Runtime   iruntime.Runtime
	Proxy     proxy.Manager
	Accessory accessory.Manager
}

type StatusTarget struct {
	HostName    string
	Server      config.Server
	ServiceName string
	Command     remote.Command
}

type LogsOptions struct {
	ServiceName string
	HostName    string
	Color       string
	Tail        int
	Follow      bool
}

type LogsTarget struct {
	HostName    string
	Server      config.Server
	ServiceName string
	Color       string
	Command     remote.Command
}

type DeployLockCommands struct {
	Acquire []remote.Command
	Release []remote.Command
}

func New(cfg config.Config, envName string) (*Planner, error) {
	if err := config.ValidateEnv(cfg, envName); err != nil {
		return nil, err
	}
	env := cfg.Environments[envName]
	root := path.Join(cfg.Defaults.Root, cfg.Project, envName)
	network := safe(cfg.Project + "_" + envName)
	return &Planner{
		Config:  cfg,
		EnvName: envName,
		Env:     env,
		Runtime: iruntime.Docker{
			Project: cfg.Project,
			EnvName: envName,
			Root:    root,
			Network: network,
		},
		Proxy: proxy.Caddy{
			Project:     cfg.Project,
			EnvName:     envName,
			Root:        root,
			Network:     network,
			ListenHTTP:  env.Proxy.HTTPPort(),
			ListenHTTPS: env.Proxy.HTTPSPort(),
		},
		Accessory: accessory.NewDocker(cfg.Project, envName, cfg.Defaults.Root),
	}, nil
}

func (p *Planner) WithSecrets(secrets map[string]string) *Planner {
	p.Secrets = secrets
	return p
}

func (p *Planner) DeploySecretNames() []string {
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			seen[name] = true
		}
	}
	add(p.Config.Registry.PasswordSecret)
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		for _, name := range p.Env.Services[serviceName].Secrets {
			add(name)
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p *Planner) Provision() remote.Plan {
	var commands []remote.Command
	for _, server := range p.servers() {
		commands = append(commands, p.Runtime.ProvisionHost(server)...)
	}
	for _, server := range p.proxyServers() {
		commands = append(commands, p.Proxy.Provision(server)...)
	}
	return remote.Plan{Title: fmt.Sprintf("Provision %s/%s", p.Config.Project, p.EnvName), Commands: commands}
}

func (p *Planner) Deploy(now time.Time) (remote.Plan, error) {
	rel := release.New(now)
	commands := p.accessoryDependencyCommands()
	registryPassword := p.Secrets[p.Config.Registry.PasswordSecret]

	commands = append(commands, p.Runtime.LoginLocal(p.Config.Registry, registryPassword))
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		image := imageForRelease(service.Image, rel.ID)
		commands = append(commands, p.Runtime.BuildService(serviceName, service, image))
		commands = append(commands, p.Runtime.PushService(serviceName, image))
	}

	loggedInHosts := map[string]bool{}
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		image := imageForRelease(service.Image, rel.ID)
		for _, server := range p.serversForHosts(service.Hosts) {
			hostKey := serverKey(server)
			if !loggedInHosts[hostKey] {
				commands = append(commands, p.Runtime.LoginRemote(server, p.Config.Registry, registryPassword))
				loggedInHosts[hostKey] = true
			}
			envText, err := renderEnv(service, p.Secrets)
			if err != nil {
				return remote.Plan{}, fmt.Errorf("render env for service %s: %w", serviceName, err)
			}
			commands = append(commands, p.Runtime.UploadEnv(server, serviceName, rel.ID, envText))
			commands = append(commands, p.Runtime.StartService(server, serviceName, service, image, rel.ID))
		}
	}

	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		if strings.TrimSpace(service.HealthCheck.Path) == "" {
			continue
		}
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.WaitHealthy(server, serviceName, service))
		}
	}

	for _, hostName := range p.proxyHostNames() {
		server, ok := p.Env.Servers[hostName]
		if !ok {
			continue
		}
		routes := p.routesForHost(hostName)
		if len(routes) == 0 {
			continue
		}
		commands = append(commands, p.Proxy.Switch(server, proxy.SwitchInput{
			Services: p.Env.Services,
			Routes:   routes,
		})...)
	}

	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		image := imageForRelease(service.Image, rel.ID)
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.RecordActive(server, serviceName, image, rel.ID))
		}
	}
	routedServices := p.routedServiceNames()
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		if routedServices[serviceName] {
			continue
		}
		service := p.Env.Services[serviceName]
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.StopPrevious(server, serviceName))
		}
	}
	commands = append(commands, p.cleanupCommands()...)
	return remote.Plan{
		Title:    fmt.Sprintf("Deploy %s/%s", p.Config.Project, p.EnvName),
		Subtitle: "Release " + rel.ID,
		Commands: commands,
	}, nil
}

func (p *Planner) accessoryDependencyCommands() []remote.Command {
	if p.Accessory == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		for _, name := range p.Env.Services[serviceName].DependsOn {
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	commands := make([]remote.Command, 0, len(names))
	for _, name := range names {
		configured, ok := p.Env.Accessories[name]
		if !ok {
			continue
		}
		server, ok := p.Env.Servers[configured.Host]
		if !ok {
			continue
		}
		commands = append(commands, p.Accessory.Verify(server, name, configured))
	}
	return commands
}

func (p *Planner) Cleanup() remote.Plan {
	return remote.Plan{Title: fmt.Sprintf("Cleanup %s/%s", p.Config.Project, p.EnvName), Commands: p.cleanupCommands()}
}

func (p *Planner) WithDeployLock(plan remote.Plan, operation string, timeout time.Duration) remote.Plan {
	locks := p.DeployLockCommands(plan.Commands, operation, timeout)
	if len(locks.Acquire) == 0 {
		return plan
	}
	insertAt := firstRemoteCommandIndex(plan.Commands)
	commands := make([]remote.Command, 0, len(plan.Commands)+len(locks.Acquire)+len(locks.Release))
	commands = append(commands, plan.Commands[:insertAt]...)
	commands = append(commands, locks.Acquire...)
	commands = append(commands, plan.Commands[insertAt:]...)
	commands = append(commands, locks.Release...)

	plan.Commands = commands
	return plan
}

func (p *Planner) DeployLockCommands(commands []remote.Command, operation string, timeout time.Duration) DeployLockCommands {
	servers := lockServers(commands)
	if len(servers) == 0 {
		return DeployLockCommands{}
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	operation = strings.TrimSpace(operation)
	if operation == "" {
		operation = "mutating command"
	}
	owner := lockOwner(operation)

	locks := DeployLockCommands{
		Acquire: make([]remote.Command, 0, len(servers)),
		Release: make([]remote.Command, 0, len(servers)),
	}
	lockIDs := map[string]string{}
	for _, server := range servers {
		key := serverKey(server)
		id := "deploy-lock:" + key
		lockIDs[key] = id
		command := p.Runtime.AcquireLock(server, iruntime.LockOptions{
			Operation: operation,
			Timeout:   timeout,
			Owner:     owner,
		})
		command.ID = id
		locks.Acquire = append(locks.Acquire, command)
	}

	for i := len(servers) - 1; i >= 0; i-- {
		server := servers[i]
		command := p.Runtime.ReleaseLock(server, iruntime.LockOptions{
			Operation: operation,
			Timeout:   timeout,
			Owner:     owner,
		})
		command.Always = true
		command.RunIfSucceeded = lockIDs[serverKey(server)]
		locks.Release = append(locks.Release, command)
	}

	return locks
}

func (p *Planner) SyncProxy() remote.Plan {
	var commands []remote.Command
	for _, hostName := range p.proxyHostNames() {
		server, ok := p.Env.Servers[hostName]
		if !ok {
			continue
		}
		routes := p.routesForHost(hostName)
		if len(routes) == 0 {
			continue
		}
		commands = append(commands, p.Proxy.Sync(server, proxy.SwitchInput{
			Services: p.Env.Services,
			Routes:   routes,
		})...)
	}
	return remote.Plan{Title: fmt.Sprintf("Sync proxy routes %s/%s", p.Config.Project, p.EnvName), Commands: commands}
}

func (p *Planner) Rollback() remote.Plan {
	var commands []remote.Command
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.StartPrevious(server, serviceName, service))
		}
	}

	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		if strings.TrimSpace(service.HealthCheck.Path) == "" {
			continue
		}
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.WaitHealthy(server, serviceName, service))
		}
	}

	for _, hostName := range p.proxyHostNames() {
		server, ok := p.Env.Servers[hostName]
		if !ok {
			continue
		}
		routes := p.routesForHost(hostName)
		if len(routes) == 0 {
			continue
		}
		commands = append(commands, p.Proxy.Switch(server, proxy.SwitchInput{
			Services: p.Env.Services,
			Routes:   routes,
		})...)
	}

	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.RollbackState(server, serviceName))
		}
	}

	routedServices := p.routedServiceNames()
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		if routedServices[serviceName] {
			continue
		}
		service := p.Env.Services[serviceName]
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.StopPrevious(server, serviceName))
		}
	}

	return remote.Plan{Title: fmt.Sprintf("Rollback %s/%s", p.Config.Project, p.EnvName), Commands: commands}
}

func (p *Planner) StatusTargets() []StatusTarget {
	var targets []StatusTarget
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		for _, hostName := range p.hostNamesForService(service) {
			server, ok := p.Env.Servers[hostName]
			if !ok {
				continue
			}
			targets = append(targets, StatusTarget{
				HostName:    hostName,
				Server:      server,
				ServiceName: serviceName,
				Command:     p.Runtime.InspectService(server, serviceName, service),
			})
		}
	}
	return targets
}

func (p *Planner) StatusPlan() remote.Plan {
	targets := p.StatusTargets()
	commands := make([]remote.Command, 0, len(targets))
	for _, target := range targets {
		commands = append(commands, target.Command)
	}
	return remote.Plan{Title: fmt.Sprintf("Status %s/%s", p.Config.Project, p.EnvName), Commands: commands}
}

func (p *Planner) Logs(options LogsOptions) (remote.Plan, error) {
	targets, err := p.LogsTargets(options)
	if err != nil {
		return remote.Plan{}, err
	}
	commands := make([]remote.Command, 0, len(targets))
	for _, target := range targets {
		commands = append(commands, target.Command)
	}
	return remote.Plan{Title: fmt.Sprintf("Logs %s/%s service %s", p.Config.Project, p.EnvName, strings.TrimSpace(options.ServiceName)), Commands: commands}, nil
}

func (p *Planner) LogsTargets(options LogsOptions) ([]LogsTarget, error) {
	serviceName := strings.TrimSpace(options.ServiceName)
	service, ok := p.Env.Services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service %q was not found", serviceName)
	}
	color, err := normalizeLogsColor(options.Color)
	if err != nil {
		return nil, err
	}
	if options.Tail < 0 {
		return nil, fmt.Errorf("tail must be >= 0")
	}

	hostNames := p.hostNamesForService(service)
	if options.HostName != "" {
		hostName := strings.TrimSpace(options.HostName)
		if _, ok := p.Env.Servers[hostName]; !ok {
			return nil, fmt.Errorf("host %q was not found", hostName)
		}
		if !serviceRunsOnHost(service, hostName) {
			return nil, fmt.Errorf("service %q does not run on host %q", serviceName, hostName)
		}
		hostNames = []string{hostName}
	}
	if options.Follow && len(hostNames) > 1 {
		return nil, fmt.Errorf("--follow with multiple hosts requires --host")
	}

	targets := make([]LogsTarget, 0, len(hostNames))
	for _, hostName := range hostNames {
		server, ok := p.Env.Servers[hostName]
		if !ok {
			continue
		}
		targets = append(targets, LogsTarget{
			HostName:    hostName,
			Server:      server,
			ServiceName: serviceName,
			Color:       color,
			Command: p.Runtime.LogsService(server, serviceName, service, iruntime.LogsOptions{
				Color:  color,
				Tail:   options.Tail,
				Follow: options.Follow,
			}),
		})
	}
	return targets, nil
}

func (p *Planner) cleanupCommands() []remote.Command {
	var commands []remote.Command
	for _, serviceName := range sortedServiceNames(p.Env.Services) {
		service := p.Env.Services[serviceName]
		for _, server := range p.serversForHosts(service.Hosts) {
			commands = append(commands, p.Runtime.CleanupService(server, serviceName, service, iruntime.CleanupOptions{
				KeepReleases: p.Env.Retention.Releases,
			}))
		}
	}
	return commands
}

func renderEnv(service config.Service, secrets map[string]string) (string, error) {
	var lines []string
	for key, value := range service.Env {
		line, err := envFileLine(key, value)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	for _, name := range service.Secrets {
		line, err := envFileLine(name, secrets[name])
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n", nil
}

func envFileLine(key string, value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("env file value for %s contains unsupported newline", key)
	}
	return key + "=" + value, nil
}

func imageForRelease(image string, releaseID string) string {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		image = image[:lastColon]
	}
	return image + ":" + releaseID
}

func (p *Planner) proxyServers() []config.Server {
	if len(p.Env.Proxy.Routes) == 0 {
		return nil
	}
	hostNames := p.proxyHostNames()
	if len(hostNames) > 0 {
		return p.serversForHosts(hostNames)
	}
	return nil
}

func (p *Planner) proxyHostNames() []string {
	seen := map[string]bool{}
	for _, route := range p.Env.Proxy.Routes {
		service, ok := p.Env.Services[route.Service]
		if !ok {
			continue
		}
		for _, host := range service.Hosts {
			seen[host] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (p *Planner) routesForHost(hostName string) []config.ProxyRoute {
	var routes []config.ProxyRoute
	for _, route := range p.Env.Proxy.Routes {
		service, ok := p.Env.Services[route.Service]
		if !ok {
			continue
		}
		if serviceRunsOnHost(service, hostName) {
			routes = append(routes, route)
		}
	}
	return routes
}

func (p *Planner) routedServiceNames() map[string]bool {
	names := map[string]bool{}
	for _, route := range p.Env.Proxy.Routes {
		names[route.Service] = true
	}
	return names
}

func serviceRunsOnHost(service config.Service, hostName string) bool {
	for _, host := range service.Hosts {
		if host == hostName {
			return true
		}
	}
	return false
}

func normalizeLogsColor(color string) (string, error) {
	color = strings.TrimSpace(color)
	if color == "" {
		return "active", nil
	}
	switch color {
	case "active", "previous", "blue", "green":
		return color, nil
	default:
		return "", fmt.Errorf("color must be active, previous, blue, or green")
	}
}

func (p *Planner) servers() []config.Server {
	names := sortedServerNames(p.Env.Servers)
	servers := make([]config.Server, 0, len(names))
	for _, name := range names {
		servers = append(servers, p.Env.Servers[name])
	}
	return servers
}

func (p *Planner) serversForHosts(hosts []string) []config.Server {
	if len(hosts) == 0 {
		return p.servers()
	}
	var servers []config.Server
	for _, host := range sortedHostNames(hosts) {
		if server, ok := p.Env.Servers[host]; ok {
			servers = append(servers, server)
		}
	}
	return servers
}

func (p *Planner) hostNamesForService(service config.Service) []string {
	if len(service.Hosts) == 0 {
		return sortedServerNames(p.Env.Servers)
	}
	return sortedHostNames(service.Hosts)
}

func sortedServiceNames(services map[string]config.Service) []string {
	keys := make([]string, 0, len(services))
	for key := range services {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedHostNames(hosts []string) []string {
	out := append([]string(nil), hosts...)
	sort.Strings(out)
	return out
}

func sortedServerNames(servers map[string]config.Server) []string {
	keys := make([]string, 0, len(servers))
	for key := range servers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func serverKey(server config.Server) string {
	return fmt.Sprintf("%s@%s:%d", server.SSHUser, server.Host, server.SSHPort)
}

func lockServers(commands []remote.Command) []config.Server {
	seen := map[string]bool{}
	var servers []config.Server
	for _, command := range commands {
		if command.Host == "" {
			continue
		}
		server := config.Server{Host: command.Host, SSHUser: command.SSHUser, SSHPort: command.SSHPort}
		key := serverKey(server)
		if seen[key] {
			continue
		}
		seen[key] = true
		servers = append(servers, server)
	}
	return servers
}

func firstRemoteCommandIndex(commands []remote.Command) int {
	for i, command := range commands {
		if command.Host != "" {
			return i
		}
	}
	return len(commands)
}

func lockOwner(operation string) string {
	return fmt.Sprintf("%s-%d", safe(operation), time.Now().UnixNano())
}

func safe(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", ".", "_", "-", "_")
	return replacer.Replace(value)
}
