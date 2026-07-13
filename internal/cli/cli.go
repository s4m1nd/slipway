package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/s4m1nd/slipway/internal/accessory"
	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/console"
	"github.com/s4m1nd/slipway/internal/planner"
	"github.com/s4m1nd/slipway/internal/remote"
	"github.com/s4m1nd/slipway/internal/secrets"
	"github.com/s4m1nd/slipway/internal/ssh"
	"github.com/s4m1nd/slipway/internal/state"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

const exampleConfig = `project: demo

retention:
  releases: 5

registry:
  server: ghcr.io
  username: demo
  password_secret: REGISTRY_PASSWORD

secrets:
  names:
    - REGISTRY_PASSWORD
    - DATABASE_URL
    - REDIS_URL
    - REDIS_PASSWORD

environments:
  production:
    servers:
      app-1:
        host: 203.0.113.10
        ssh_user: root
        host_ssh_port: 22
      worker-1:
        host: 203.0.113.11
        ssh_user: root
        host_ssh_port: 22
    proxy:
      listen_http: :80
      listen_https: :443
      routes:
        - host: app.example.com
          service: web
          tls: true
    accessories:
      redis:
        type: redis
        image: redis:7-alpine
        host: app-1
        secrets:
          - REDIS_PASSWORD
        storage:
          volume: demo-redis-data
    services:
      web:
        image: ghcr.io/example/demo-web
        build:
          context: .
          dockerfile: Dockerfile
          platform: linux/amd64
        hosts: [app-1]
        depends_on: [redis]
        internal_port: 3000
        health_check:
          path: /healthz
          interval: 5s
          timeout: 2s
          retries: 12
        env:
          RACK_ENV: production
          REDIS_HOST: redis
          REDIS_PORT: "6379"
        secrets:
          - DATABASE_URL
          - REDIS_URL
          - REDIS_PASSWORD
      worker:
        image: ghcr.io/example/demo-worker
        build:
          context: .
          dockerfile: Dockerfile.worker
          platform: linux/amd64
        hosts: [worker-1]
        env:
          RACK_ENV: production
        secrets:
          - DATABASE_URL
          - REDIS_URL
`

func Execute(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}

	cmd := args[0]
	switch cmd {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "version":
		return runVersion(stdout)
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "provision":
		return runProvision(args[1:], stdout, stderr)
	case "deploy":
		return runDeploy(args[1:], stdout, stderr)
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "rollback":
		return runRollback(args[1:], stdout, stderr)
	case "sync-proxy":
		return runSyncProxy(args[1:], stdout, stderr)
	case "logs":
		return runLogs(args[1:], stdout, stderr)
	case "cleanup":
		return runCleanup(args[1:], stdout, stderr)
	case "secrets":
		return runSecrets(args[1:], stdout, stderr)
	case "accessory":
		return runAccessory(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Slipway deploys Dockerized apps over SSH with blue/green releases.

Usage:
  slipway init [-c slipway.yml] [--force]
  slipway validate -c slipway.yml
  slipway provision -c slipway.yml --env production [--dry-run] [--verbose]
  slipway deploy -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway rollback -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway status -c slipway.yml --env production [--dry-run]
  slipway sync-proxy -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway cleanup -c slipway.yml --env production [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway logs -c slipway.yml --env production --service web [--host app-1] [--color active] [--tail 100] [--follow] [--dry-run]
  slipway accessory apply -c slipway.yml --env production [--name <accessory>] [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway accessory status -c slipway.yml --env production [--name <accessory>] [--dry-run]
  slipway accessory logs -c slipway.yml --env production --name <accessory> [--tail 100] [--follow] [--dry-run]
  slipway accessory restart -c slipway.yml --env production --name <accessory> [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway accessory exec -c slipway.yml --env production --name <accessory> [--dry-run] -- command [args...]
  slipway secrets exec -c slipway.yml --secret NAME [--secret NAME ...] [--dry-run] -- command [args...]
  slipway version`)
}

func runVersion(stdout io.Writer) int {
	fmt.Fprintf(stdout, "Slipway version %s\n", Version)
	fmt.Fprintf(stdout, "Commit: %s\n", Commit)
	fmt.Fprintf(stdout, "Date: %s\n", Date)
	fmt.Fprintf(stdout, "Go: %s\n", runtime.Version())
	return 0
}

func runInit(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	force := fs.Bool("force", false, "overwrite existing config")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := os.Stat(*configPath); err == nil && !*force {
		fmt.Fprintf(stderr, "%s already exists; use --force to overwrite\n", *configPath)
		return 1
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "stat %s: %v\n", *configPath, err)
		return 1
	}
	if err := os.MkdirAll(pathForMkdir(*configPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "create parent directory: %v\n", err)
		return 1
	}
	if err := os.WriteFile(*configPath, []byte(exampleConfig), 0o644); err != nil {
		fmt.Fprintf(stderr, "write %s: %v\n", *configPath, err)
		return 1
	}
	fmt.Fprintf(stdout, "created %s\n", *configPath)
	return 0
}

func pathForMkdir(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return "."
	}
	return dir
}

func runValidate(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "optional environment to validate")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if *envName == "" {
		err = config.Validate(cfg)
	} else {
		err = config.ValidateEnv(cfg, *envName)
	}
	if err != nil {
		fmt.Fprintf(stderr, "invalid config:\n%v\n", err)
		return 1
	}
	if *envName == "" {
		fmt.Fprintf(stdout, "%s is valid (%d environment%s)\n", *configPath, len(cfg.Environments), plural(len(cfg.Environments)))
	} else {
		fmt.Fprintf(stdout, "%s is valid for environment %s\n", *configPath, *envName)
	}
	return 0
}

func runProvision(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("provision", args, stderr, false)
	if exitCode != 0 {
		return exitCode
	}
	plan := p.Provision()
	if options.DryRun {
		plan.Print(stdout)
		return 0
	}
	return executePlan(plan, options.Verbose, stdout, stderr)
}

func runDeploy(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("deploy", args, stderr, true)
	if exitCode != 0 {
		return exitCode
	}
	if !options.DryRun {
		resolver, err := secrets.NewResolver(p.Config.Secrets)
		if err != nil {
			fmt.Fprintf(stderr, "create secret resolver: %v\n", err)
			return 1
		}
		resolved, err := resolver.Resolve(context.Background(), p.DeploySecretNames())
		if err != nil {
			fmt.Fprintf(stderr, "resolve secrets: %v\n", err)
			return 1
		}
		p.WithSecrets(resolved)
	}
	deployPlan, err := p.Deploy(time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "plan deploy: %v\n", err)
		return 1
	}
	plan := p.WithDeployLock(deployPlan, "deploy", options.LockTimeout)
	if options.DryRun {
		plan.Print(stdout)
		return 0
	}
	return executeDeploy(p, plan, options.Verbose, stdout, stderr)
}

func runSecrets(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printSecretsUsage(stderr)
		return 2
	}
	switch args[0] {
	case "exec":
		return runSecretsExec(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		printSecretsUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown secrets command %q\n\n", args[0])
		printSecretsUsage(stderr)
		return 2
	}
}

func runAccessory(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		printAccessoryUsage(stdout)
		return 0
	}
	switch args[0] {
	case "help", "-h", "--help":
		printAccessoryUsage(stdout)
		return 0
	case "apply":
		return runAccessoryApply(args[1:], stdout, stderr)
	case "status":
		return runAccessoryStatus(args[1:], stdout, stderr)
	case "logs":
		return runAccessoryLogs(args[1:], stdout, stderr)
	case "restart":
		return runAccessoryRestart(args[1:], stdout, stderr)
	case "exec":
		return runAccessoryExec(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown accessory command %q\n\n", args[0])
		printAccessoryUsage(stderr)
		return 2
	}
}

func printAccessoryUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  slipway accessory apply -c slipway.yml --env production [--name <accessory>] [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway accessory status -c slipway.yml --env production [--name <accessory>] [--dry-run]
  slipway accessory logs -c slipway.yml --env production --name <accessory> [--tail 100] [--follow] [--dry-run]
  slipway accessory restart -c slipway.yml --env production --name <accessory> [--dry-run] [--verbose] [--lock-timeout 30m]
  slipway accessory exec -c slipway.yml --env production --name <accessory> [--dry-run] -- command [args...]`)
}

func runAccessoryApply(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessory apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	name := fs.String("name", "", "optional accessory name")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	verbose := fs.Bool("verbose", false, "show generated non-sensitive commands while running")
	lockTimeout := fs.Duration("lock-timeout", 30*time.Minute, "stale deploy lock timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *lockTimeout <= 0 {
		fmt.Fprintln(stderr, "--lock-timeout must be greater than 0")
		return 2
	}
	cfg, targets, manager, exitCode := loadAccessoryTargets(*configPath, *envName, *name, false, stderr)
	if exitCode != 0 {
		return exitCode
	}
	secretNames := accessorySecretNames(targets)
	resolved := map[string]string{}
	if *dryRun {
		for _, secretName := range secretNames {
			resolved[secretName] = "<redacted>"
		}
	} else if len(secretNames) > 0 {
		resolver, err := secrets.NewResolver(cfg.Secrets)
		if err != nil {
			fmt.Fprintf(stderr, "create secret resolver: %v\n", err)
			return 1
		}
		resolved, err = resolver.Resolve(context.Background(), secretNames)
		if err != nil {
			fmt.Fprintf(stderr, "resolve accessory secrets: %v\n", err)
			return 1
		}
	}
	var commands []remote.Command
	for _, target := range targets {
		planned, err := manager.Apply(target.Server, target.Name, target.Config, resolved)
		if err != nil {
			fmt.Fprintf(stderr, "plan accessory %s: %v\n", target.Name, err)
			return 1
		}
		commands = append(commands, planned...)
	}
	plan := remote.Plan{Title: fmt.Sprintf("Apply accessories %s/%s", cfg.Project, *envName), Commands: commands}
	p, err := planner.New(cfg, *envName)
	if err != nil {
		fmt.Fprintf(stderr, "plan accessory lock: %v\n", err)
		return 1
	}
	plan = p.WithDeployLock(plan, "accessory apply", *lockTimeout)
	if *dryRun {
		plan.Print(stdout)
		return 0
	}
	return executePlan(plan, *verbose, stdout, stderr)
}

func runAccessoryStatus(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessory status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	name := fs.String("name", "", "optional accessory name")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, targets, manager, exitCode := loadAccessoryTargets(*configPath, *envName, *name, false, stderr)
	if exitCode != 0 {
		return exitCode
	}
	commands := make([]remote.Command, 0, len(targets))
	for _, target := range targets {
		commands = append(commands, manager.Inspect(target.Server, target.Name, target.Config))
	}
	if *dryRun {
		remote.Plan{Title: fmt.Sprintf("Accessory status %s/%s", cfg.Project, *envName), Commands: commands}.Print(stdout)
		return 0
	}
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr}
	statuses := make([]accessory.Status, 0, len(targets))
	for i, target := range targets {
		output, err := runner.Output(context.Background(), commands[i])
		if err != nil {
			fmt.Fprintf(stderr, "inspect accessory %s: %v\n", target.Name, err)
			return 1
		}
		status, err := accessory.ParseStatus(target, output)
		if err != nil {
			fmt.Fprintf(stderr, "parse accessory %s status: %v\n", target.Name, err)
			return 1
		}
		statuses = append(statuses, status)
	}
	accessory.RenderStatuses(stdout, cfg.Project, *envName, statuses)
	return 0
}

func runAccessoryLogs(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessory logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	name := fs.String("name", "", "accessory name")
	tail := fs.Int("tail", 100, "number of log lines to show")
	follow := fs.Bool("follow", false, "follow log output")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tail < 0 {
		fmt.Fprintln(stderr, "--tail must be >= 0")
		return 2
	}
	cfg, targets, manager, exitCode := loadAccessoryTargets(*configPath, *envName, *name, true, stderr)
	if exitCode != 0 {
		return exitCode
	}
	target := targets[0]
	command := manager.Logs(target.Server, target.Name, target.Config, accessory.LogsOptions{Tail: *tail, Follow: *follow})
	if *dryRun {
		remote.Plan{Title: fmt.Sprintf("Logs %s/%s accessory %s", cfg.Project, *envName, target.Name), Commands: []remote.Command{command}}.Print(stdout)
		return 0
	}
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr}
	if err := runner.Run(context.Background(), command); err != nil {
		fmt.Fprintf(stderr, "stream logs for accessory %s: %v\n", target.Name, err)
		return 1
	}
	return 0
}

func runAccessoryRestart(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessory restart", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	name := fs.String("name", "", "accessory name")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	verbose := fs.Bool("verbose", false, "show generated non-sensitive commands while running")
	lockTimeout := fs.Duration("lock-timeout", 30*time.Minute, "stale deploy lock timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *lockTimeout <= 0 {
		fmt.Fprintln(stderr, "--lock-timeout must be greater than 0")
		return 2
	}
	cfg, targets, manager, exitCode := loadAccessoryTargets(*configPath, *envName, *name, true, stderr)
	if exitCode != 0 {
		return exitCode
	}
	target := targets[0]
	plan := remote.Plan{Title: fmt.Sprintf("Restart %s/%s accessory %s", cfg.Project, *envName, target.Name), Commands: manager.Restart(target.Server, target.Name, target.Config)}
	p, err := planner.New(cfg, *envName)
	if err != nil {
		fmt.Fprintf(stderr, "plan accessory lock: %v\n", err)
		return 1
	}
	plan = p.WithDeployLock(plan, "accessory restart", *lockTimeout)
	if *dryRun {
		plan.Print(stdout)
		return 0
	}
	return executePlan(plan, *verbose, stdout, stderr)
}

func runAccessoryExec(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("accessory exec", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	name := fs.String("name", "", "accessory name")
	dryRun := fs.Bool("dry-run", false, "print command without running it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintln(stderr, "accessory exec requires a command after --")
		return 2
	}
	cfg, targets, manager, exitCode := loadAccessoryTargets(*configPath, *envName, *name, true, stderr)
	if exitCode != 0 {
		return exitCode
	}
	target := targets[0]
	command, err := manager.Exec(target.Server, target.Name, target.Config, fs.Args())
	if err != nil {
		fmt.Fprintf(stderr, "accessory exec: %v\n", err)
		return 1
	}
	if *dryRun {
		remote.Plan{Title: fmt.Sprintf("Exec %s/%s accessory %s", cfg.Project, *envName, target.Name), Commands: []remote.Command{command}}.Print(stdout)
		return 0
	}
	runner := ssh.Runner{Stdin: os.Stdin, Stdout: stdout, Stderr: stderr}
	if err := runner.Run(context.Background(), command); err != nil {
		fmt.Fprintf(stderr, "exec in accessory %s: %v\n", target.Name, err)
		return 1
	}
	return 0
}

func loadAccessoryTargets(configPath string, envName string, name string, requireName bool, stderr io.Writer) (config.Config, []accessory.Target, accessory.Manager, int) {
	if strings.TrimSpace(envName) == "" {
		fmt.Fprintln(stderr, "--env is required")
		return config.Config{}, nil, nil, 2
	}
	if requireName && strings.TrimSpace(name) == "" {
		fmt.Fprintln(stderr, "--name is required")
		return config.Config{}, nil, nil, 2
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return config.Config{}, nil, nil, 1
	}
	if err := config.ValidateEnv(cfg, envName); err != nil {
		fmt.Fprintf(stderr, "invalid config:\n%v\n", err)
		return config.Config{}, nil, nil, 1
	}
	env := cfg.Environments[envName]
	manager := accessory.NewDocker(cfg.Project, envName, cfg.Defaults.Root)
	selected := strings.TrimSpace(name)
	if selected != "" {
		configured, ok := env.Accessories[selected]
		if !ok {
			fmt.Fprintf(stderr, "accessory %q was not found\n", selected)
			return config.Config{}, nil, nil, 1
		}
		return cfg, []accessory.Target{{HostName: configured.Host, Server: env.Servers[configured.Host], Name: selected, Config: configured}}, manager, 0
	}
	names := make([]string, 0, len(env.Accessories))
	for accessoryName := range env.Accessories {
		names = append(names, accessoryName)
	}
	sort.Strings(names)
	targets := make([]accessory.Target, 0, len(names))
	for _, accessoryName := range names {
		configured := env.Accessories[accessoryName]
		targets = append(targets, accessory.Target{HostName: configured.Host, Server: env.Servers[configured.Host], Name: accessoryName, Config: configured})
	}
	return cfg, targets, manager, 0
}

func accessorySecretNames(targets []accessory.Target) []string {
	seen := map[string]bool{}
	for _, target := range targets {
		for _, name := range target.Config.Secrets {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func printSecretsUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  slipway secrets exec -c slipway.yml --secret NAME [--secret NAME ...] [--dry-run] -- command [args...]`)
}

func runSecretsExec(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("secrets exec", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	dryRun := fs.Bool("dry-run", false, "print the redacted command plan without resolving secrets")
	var selected secretNameFlags
	fs.Var(&selected, "secret", "secret name to inject into the child environment; repeatable")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(selected) == 0 {
		fmt.Fprintln(stderr, "--secret is required")
		return 2
	}
	commandArgs := fs.Args()
	if len(commandArgs) == 0 {
		fmt.Fprintln(stderr, "command is required after --")
		return 2
	}

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	if err := config.ValidateSecretsSelection(cfg.Secrets, []string(selected)); err != nil {
		fmt.Fprintf(stderr, "invalid secrets config:\n%v\n", err)
		return 1
	}

	if *dryRun {
		remote.Plan{
			Title: "Secrets exec",
			Commands: []remote.Command{{
				Description: fmt.Sprintf("resolve %d secret%s and run child command", len(selected), plural(len(selected))),
				Script:      strings.Join(commandArgs, " "),
				Sensitive:   true,
			}},
		}.Print(stdout)
		return 0
	}

	resolver, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		fmt.Fprintf(stderr, "create secret resolver: %v\n", err)
		return 1
	}
	resolved, err := resolver.Resolve(context.Background(), []string(selected))
	if err != nil {
		fmt.Fprintf(stderr, "resolve secrets: %v\n", err)
		return 1
	}
	if err := runChildWithSecrets(context.Background(), commandArgs, resolved, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "run child command: %v\n", err)
		return 1
	}
	return 0
}

type secretNameFlags []string

func (f *secretNameFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *secretNameFlags) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("secret name must not be empty")
	}
	*f = append(*f, value)
	return nil
}

func runChildWithSecrets(ctx context.Context, args []string, resolved map[string]string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	cmd.Env = envWithSecrets(os.Environ(), resolved)
	return cmd.Run()
}

func envWithSecrets(base []string, resolved map[string]string) []string {
	secretNames := map[string]bool{}
	for key := range resolved {
		secretNames[key] = true
	}
	out := make([]string, 0, len(base)+len(resolved))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && secretNames[key] {
			continue
		}
		out = append(out, entry)
	}
	for key, value := range resolved {
		out = append(out, key+"="+value)
	}
	return out
}

func runStatus(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("status", args, stderr, false)
	if exitCode != 0 {
		return exitCode
	}
	if options.DryRun {
		p.StatusPlan().Print(stdout)
		return 0
	}
	return executeStatus(p, stdout, stderr)
}

func runRollback(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("rollback", args, stderr, true)
	if exitCode != 0 {
		return exitCode
	}
	if options.DryRun {
		p.WithDeployLock(p.Rollback(), "rollback", options.LockTimeout).Print(stdout)
		return 0
	}
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr, Verbose: options.Verbose, OutputIndent: "        │ "}
	return executeRollbackWithOptions(p, options.LockTimeout, runner, options.Verbose, stdout, stderr)
}

func runCleanup(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("cleanup", args, stderr, true)
	if exitCode != 0 {
		return exitCode
	}
	plan := p.WithDeployLock(p.Cleanup(), "cleanup", options.LockTimeout)
	if options.DryRun {
		plan.Print(stdout)
		return 0
	}
	return executePlan(plan, options.Verbose, stdout, stderr)
}

func runSyncProxy(args []string, stdout io.Writer, stderr io.Writer) int {
	p, options, exitCode := loadPlannerFromFlags("sync-proxy", args, stderr, true)
	if exitCode != 0 {
		return exitCode
	}
	plan := p.WithDeployLock(p.SyncProxy(), "sync-proxy", options.LockTimeout)
	if options.DryRun {
		plan.Print(stdout)
		return 0
	}
	return executePlan(plan, options.Verbose, stdout, stderr)
}

func runLogs(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	serviceName := fs.String("service", "", "service name")
	hostName := fs.String("host", "", "server name")
	color := fs.String("color", "active", "active, previous, blue, or green")
	tail := fs.Int("tail", 100, "number of log lines to show")
	follow := fs.Bool("follow", false, "follow log output")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*envName) == "" {
		fmt.Fprintln(stderr, "--env is required")
		return 2
	}
	if strings.TrimSpace(*serviceName) == "" {
		fmt.Fprintln(stderr, "--service is required")
		return 2
	}

	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	p, err := planner.New(cfg, *envName)
	if err != nil {
		fmt.Fprintf(stderr, "invalid config:\n%v\n", err)
		return 1
	}
	options := planner.LogsOptions{
		ServiceName: *serviceName,
		HostName:    *hostName,
		Color:       *color,
		Tail:        *tail,
		Follow:      *follow,
	}
	if *dryRun {
		plan, err := p.Logs(options)
		if err != nil {
			fmt.Fprintf(stderr, "logs: %v\n", err)
			return 1
		}
		plan.Print(stdout)
		return 0
	}
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr}
	return executeLogs(p, options, runner, stdout, stderr)
}

type plannerOptions struct {
	DryRun      bool
	Verbose     bool
	LockTimeout time.Duration
}

func loadPlannerFromFlags(command string, args []string, stderr io.Writer, includeLock bool) (*planner.Planner, plannerOptions, int) {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("c", "slipway.yml", "config path")
	envName := fs.String("env", "", "environment name")
	dryRun := fs.Bool("dry-run", false, "print commands without running them")
	verbose := false
	if command != "status" {
		fs.BoolVar(&verbose, "verbose", false, "show generated non-sensitive commands while running")
	}
	var lockTimeout *time.Duration
	if includeLock {
		lockTimeout = fs.Duration("lock-timeout", 30*time.Minute, "stale deploy lock timeout")
	}
	if err := fs.Parse(args); err != nil {
		return nil, plannerOptions{}, 2
	}
	if includeLock && *lockTimeout <= 0 {
		fmt.Fprintln(stderr, "--lock-timeout must be greater than 0")
		return nil, plannerOptions{}, 2
	}
	if strings.TrimSpace(*envName) == "" {
		fmt.Fprintln(stderr, "--env is required")
		return nil, plannerOptions{}, 2
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return nil, plannerOptions{}, 1
	}
	p, err := planner.New(cfg, *envName)
	if err != nil {
		fmt.Fprintf(stderr, "invalid config:\n%v\n", err)
		return nil, plannerOptions{}, 1
	}
	options := plannerOptions{DryRun: *dryRun, Verbose: verbose}
	if includeLock {
		options.LockTimeout = *lockTimeout
	}
	return p, options, 0
}

func executePlan(plan remote.Plan, verbose bool, stdout io.Writer, stderr io.Writer) int {
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr, Verbose: verbose, OutputIndent: "        │ "}
	c := console.New(stdout, stderr)
	c.Verbose = verbose
	if err := remote.ExecuteWithConsole(context.Background(), plan, runner, c); err != nil {
		fmt.Fprintf(stderr, "execute plan: %v\n", err)
		return 1
	}
	return 0
}

func executeDeploy(p *planner.Planner, plan remote.Plan, verbose bool, stdout io.Writer, stderr io.Writer) int {
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr, Verbose: verbose, OutputIndent: "        │ "}
	return executeDeployWithRunner(p, plan, runner, verbose, stdout, stderr)
}

func executeDeployWithRunner(p *planner.Planner, plan remote.Plan, runner rollbackRunner, verbose bool, stdout io.Writer, stderr io.Writer) int {
	c := console.New(stdout, stderr)
	c.Verbose = verbose
	started := time.Now()
	if err := remote.ExecuteWithConsole(context.Background(), plan, runner, c); err != nil {
		fmt.Fprintf(stderr, "execute deploy: %v\n", err)
		return 1
	}

	active := ""
	releaseID := strings.TrimPrefix(plan.Subtitle, "Release ")
	styles := []console.Style(nil)
	if statuses, ok := inspectStatuses(p, runner, stderr); ok {
		active, releaseID, styles = summarizeStatuses(statuses, false)
	} else {
		c.Warning("deployment completed, but final status could not be read")
	}
	c.Completion("Deployment complete",
		console.Field{Label: "active", Value: active, Styles: styles},
		console.Field{Label: "release", Value: releaseID},
		console.Field{Label: "elapsed", Value: console.FormatElapsed(time.Since(started))},
	)
	return 0
}

type rollbackRunner interface {
	remote.ExecutorRunner
	remote.OutputRunner
}

func executeRollback(p *planner.Planner, lockTimeout time.Duration, runner rollbackRunner, stdout io.Writer, stderr io.Writer) int {
	return executeRollbackWithOptions(p, lockTimeout, runner, false, stdout, stderr)
}

func executeRollbackWithOptions(p *planner.Planner, lockTimeout time.Duration, runner rollbackRunner, verbose bool, stdout io.Writer, stderr io.Writer) int {
	started := time.Now()
	ctx := context.Background()
	plan := p.Rollback()
	locks := p.DeployLockCommands(plan.Commands, "rollback", lockTimeout)
	c := console.New(stdout, stderr)
	c.Verbose = verbose
	totalSteps := len(locks.Acquire) + len(plan.Commands) + len(locks.Release)
	succeeded := map[string]bool{}
	nextStep := 1
	if len(locks.Acquire) > 0 {
		var err error
		nextStep, err = remote.ExecuteWithSucceededFromConsole(ctx, remote.Plan{Title: plan.Title, Commands: locks.Acquire}, runner, c, succeeded, nextStep, totalSteps)
		if err != nil {
			_, err = releaseRollbackLocks(ctx, locks, runner, c, succeeded, nextStep, totalSteps, err)
			fmt.Fprintf(stderr, "execute rollback: %v\n", err)
			return 1
		}
	} else if plan.Title != "" {
		c.Title(plan.Title)
	}

	statuses, ok := inspectStatuses(p, runner, stderr)
	if !ok {
		if _, err := releaseRollbackLocks(ctx, locks, runner, c, succeeded, nextStep, totalSteps, nil); err != nil {
			fmt.Fprintf(stderr, "release rollback lock: %v\n", err)
		}
		return 1
	}
	if err := state.ValidateRollbackReady(statuses); err != nil {
		fmt.Fprintf(stderr, "rollback is not ready:\n%v\n", err)
		if _, releaseErr := releaseRollbackLocks(ctx, locks, runner, c, succeeded, nextStep, totalSteps, nil); releaseErr != nil {
			fmt.Fprintf(stderr, "release rollback lock: %v\n", releaseErr)
		}
		return 1
	}
	var err error
	nextStep, err = remote.ExecuteWithSucceededFromConsole(ctx, remote.Plan{Commands: plan.Commands}, runner, c, succeeded, nextStep, totalSteps)
	if err != nil {
		_, err = releaseRollbackLocks(ctx, locks, runner, c, succeeded, nextStep, totalSteps, err)
		fmt.Fprintf(stderr, "execute rollback: %v\n", err)
		return 1
	}
	if _, err := releaseRollbackLocks(ctx, locks, runner, c, succeeded, nextStep, totalSteps, nil); err != nil {
		fmt.Fprintf(stderr, "release rollback lock: %v\n", err)
		return 1
	}
	active, releaseID, styles := summarizeStatuses(statuses, true)
	c.Completion("Rollback complete",
		console.Field{Label: "active", Value: active, Styles: styles},
		console.Field{Label: "release", Value: releaseID},
		console.Field{Label: "elapsed", Value: console.FormatElapsed(time.Since(started))},
	)
	return 0
}

func releaseRollbackLocks(ctx context.Context, locks planner.DeployLockCommands, runner remote.ExecutorRunner, c console.Console, succeeded map[string]bool, startStep int, totalSteps int, prior error) (int, error) {
	if len(locks.Release) == 0 {
		return startStep, prior
	}
	nextStep, err := remote.ExecuteWithSucceededFromConsole(ctx, remote.Plan{Commands: locks.Release}, runner, c, succeeded, startStep, totalSteps)
	if prior != nil && err != nil {
		return nextStep, fmt.Errorf("%w; release deploy lock failed: %v", prior, err)
	}
	if prior != nil {
		return nextStep, prior
	}
	return nextStep, err
}

func executeLogs(p *planner.Planner, options planner.LogsOptions, runner remote.ExecutorRunner, stdout io.Writer, stderr io.Writer) int {
	targets, err := p.LogsTargets(options)
	if err != nil {
		fmt.Fprintf(stderr, "logs: %v\n", err)
		return 1
	}
	for _, target := range targets {
		if len(targets) > 1 {
			fmt.Fprintf(stdout, "==> %s on %s/%s %s <==\n", target.ServiceName, target.HostName, logHost(target.Server), target.Color)
		}
		if err := runner.Run(context.Background(), target.Command); err != nil {
			fmt.Fprintf(stderr, "stream logs for %s on %s: %v\n", target.ServiceName, target.HostName, err)
			return 1
		}
	}
	return 0
}

func logHost(server config.Server) string {
	if server.SSHUser == "" {
		return server.Host
	}
	return server.SSHUser + "@" + server.Host
}

func executeStatus(p *planner.Planner, stdout io.Writer, stderr io.Writer) int {
	runner := ssh.Runner{Stdout: stdout, Stderr: stderr}
	statuses, ok := inspectStatuses(p, runner, stderr)
	if !ok {
		return 1
	}
	state.RenderReportWithConsole(console.New(stdout, stderr), p.Config.Project, p.EnvName, statuses)
	return 0
}

func inspectStatuses(p *planner.Planner, runner remote.OutputRunner, stderr io.Writer) ([]state.ServiceStatus, bool) {
	targets := p.StatusTargets()
	statuses := make([]state.ServiceStatus, 0, len(targets))
	for _, target := range targets {
		output, err := runner.Output(context.Background(), target.Command)
		if err != nil {
			fmt.Fprintf(stderr, "inspect status for %s on %s: %v\n", target.ServiceName, target.HostName, err)
			return nil, false
		}
		status, err := state.ParseServiceStatus(state.Target{
			HostName: target.HostName,
			Host:     target.Server.Host,
			SSHUser:  target.Server.SSHUser,
			SSHPort:  target.Server.SSHPort,
			Service:  target.ServiceName,
		}, output)
		if err != nil {
			fmt.Fprintf(stderr, "parse status for %s on %s: %v\n", target.ServiceName, target.HostName, err)
			return nil, false
		}
		statuses = append(statuses, status)
	}
	return statuses, true
}

func summarizeStatuses(statuses []state.ServiceStatus, previous bool) (string, string, []console.Style) {
	colors := map[string]bool{}
	releases := map[string]bool{}
	assignments := make([]string, 0, len(statuses))
	for _, status := range statuses {
		release := status.Active
		if previous {
			release = status.Previous
		}
		if release.Color != "" {
			colors[release.Color] = true
			label := status.Service
			if status.HostName != "" {
				label += "@" + status.HostName
			}
			assignments = append(assignments, label+"="+release.Color)
		}
		if release.Release != "" {
			releases[release.Release] = true
		}
	}

	active := ""
	styles := []console.Style(nil)
	if len(colors) == 1 {
		for color := range colors {
			active = color
			switch color {
			case "blue":
				styles = []console.Style{console.StyleBlue}
			case "green":
				styles = []console.Style{console.StyleGreen}
			}
		}
	} else if len(assignments) > 0 {
		sort.Strings(assignments)
		active = strings.Join(assignments, ", ")
	}

	releaseID := ""
	if len(releases) == 1 {
		for release := range releases {
			releaseID = release
		}
	} else if len(releases) > 1 {
		releaseID = "mixed"
	}
	return active, releaseID, styles
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
