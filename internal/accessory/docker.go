package accessory

import (
	"crypto/sha256"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

type Docker struct {
	Project string
	EnvName string
	Root    string
	Network string
}

func NewDocker(project string, envName string, baseRoot string) Docker {
	return Docker{
		Project: project,
		EnvName: envName,
		Root:    path.Join(baseRoot, project, envName),
		Network: safe(project + "_" + envName),
	}
}

func (d Docker) Apply(server config.Server, name string, accessory config.Accessory, secrets map[string]string) ([]remote.Command, error) {
	preset := presetFor(accessory.Type)
	if preset.dataDirectory == "" {
		return nil, fmt.Errorf("unsupported accessory type %q", accessory.Type)
	}
	passwordSecret := ""
	switch accessory.Type {
	case "redis":
		passwordSecret = "REDIS_PASSWORD"
	case "postgres":
		passwordSecret = "POSTGRES_PASSWORD"
	}
	if passwordSecret != "" && secrets[passwordSecret] == "" {
		return nil, fmt.Errorf("%s must not be empty", passwordSecret)
	}
	envText, err := renderEnv(accessory, secrets)
	if err != nil {
		return nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(configFingerprint(accessory))))
	upload := d.sensitiveRemoteCommand(server, "upload env for accessory "+name, d.uploadEnvScript(name), envText)
	postgresMajor := ""
	if accessory.Type == "postgres" {
		var ok bool
		postgresMajor, ok = config.PostgresMajor(accessory.Image)
		if !ok {
			return nil, fmt.Errorf("postgres image %q does not declare a major version", accessory.Image)
		}
	}
	apply := d.remoteCommand(server, "apply accessory "+name, d.applyScript(name, accessory, preset, hash, postgresMajor))
	apply.OutputMode = remote.OutputQuietOnSuccess
	return []remote.Command{upload, apply, d.waitHealthyCommand(server, name, accessory)}, nil
}

func (d Docker) Inspect(server config.Server, name string, accessory config.Accessory) remote.Command {
	return d.remoteCommand(server, "inspect accessory "+name, d.inspectScript(name, accessory))
}

func (d Docker) Logs(server config.Server, name string, configured config.Accessory, options LogsOptions) remote.Command {
	tail := options.Tail
	if tail < 0 {
		tail = 0
	}
	args := []string{"docker", "logs", "--tail", fmt.Sprintf("%d", tail)}
	if options.Follow {
		args = append(args, "--follow")
	}
	args = append(args, d.containerName(name))
	return d.remoteCommand(server, "logs for accessory "+name, d.managedGuardScript(name, configured.Type)+"\n"+shellJoin(args))
}

func (d Docker) Restart(server config.Server, name string, configured config.Accessory) []remote.Command {
	restart := d.remoteCommand(server, "restart accessory "+name, d.managedGuardScript(name, configured.Type)+"\ndocker restart "+shell(d.containerName(name)))
	restart.OutputMode = remote.OutputQuietOnSuccess
	return []remote.Command{restart, d.waitHealthyCommand(server, name, configured)}
}

func (d Docker) Exec(server config.Server, name string, configured config.Accessory, args []string) (remote.Command, error) {
	if len(args) == 0 {
		return remote.Command{}, fmt.Errorf("accessory exec requires a command after --")
	}
	command := append([]string{"docker", "exec", "-i", d.containerName(name)}, args...)
	return d.remoteCommand(server, "exec in accessory "+name, d.managedGuardScript(name, configured.Type)+"\n"+shellJoin(command)), nil
}

func (d Docker) Verify(server config.Server, name string, configured config.Accessory) remote.Command {
	script := d.managedGuardScript(name, configured.Type) + fmt.Sprintf(`
RUNNING=$(docker inspect --format '{{.State.Running}}' "$CONTAINER")
if [ "$RUNNING" != true ]; then
  echo "required accessory %s is not running" >&2
  exit 1
fi
HEALTH=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER")
if [ "$HEALTH" != healthy ]; then
  echo "required accessory %s is not healthy (status: $HEALTH)" >&2
  exit 1
fi`, name, name)
	return d.remoteCommand(server, "verify accessory "+name+" is healthy", script)
}

func (d Docker) uploadEnvScript(name string) string {
	envFile := path.Join(d.Root, "accessories", name+".env")
	changedFile := envFile + ".changed"
	return fmt.Sprintf(`set -eu
ENV_FILE=%s
CHANGED_FILE=%s
mkdir -p %s
umask 077
trap 'rm -f "$ENV_FILE.tmp.$$"' EXIT
cat > "$ENV_FILE.tmp.$$"
chmod 600 "$ENV_FILE.tmp.$$"
if [ -f "$ENV_FILE" ] && cmp -s "$ENV_FILE.tmp.$$" "$ENV_FILE"; then
  rm -f "$CHANGED_FILE"
else
  : > "$CHANGED_FILE"
  chmod 600 "$CHANGED_FILE"
fi
mv "$ENV_FILE.tmp.$$" "$ENV_FILE"`, shell(envFile), shell(changedFile), shell(path.Dir(envFile)))
}

func (d Docker) applyScript(name string, accessory config.Accessory, preset preset, configHash string, postgresMajor string) string {
	container := d.containerName(name)
	envFile := path.Join(d.Root, "accessories", name+".env")
	changedFile := envFile + ".changed"
	labels := []string{
		"slipway.project=" + d.Project,
		"slipway.env=" + d.EnvName,
		"slipway.accessory=" + name,
		"slipway.accessory.type=" + accessory.Type,
		"slipway.accessory.config_hash=" + configHash,
		"slipway.accessory.volume=" + accessory.Storage.Volume,
	}
	if postgresMajor != "" {
		labels = append(labels, "slipway.accessory.postgres_major="+postgresMajor)
	}
	runArgs := []string{
		"docker", "run", "-d",
		"--restart", "unless-stopped",
		"--name", container,
		"--network", d.Network,
		"--network-alias", name,
		"--env-file", envFile,
		"--mount", fmt.Sprintf("type=volume,src=%s,dst=%s", accessory.Storage.Volume, preset.dataDirectory),
	}
	if preset.user != "" {
		runArgs = append(runArgs, "--user", preset.user)
	}
	for _, label := range labels {
		runArgs = append(runArgs, "--label", label)
	}
	runArgs = append(runArgs,
		"--health-cmd", preset.healthCommand,
		"--health-interval", "5s",
		"--health-timeout", "3s",
		"--health-retries", "20",
		accessory.Image,
	)
	runArgs = append(runArgs, preset.startArgs...)
	postgresGuard := ""
	if postgresMajor != "" {
		postgresGuard = fmt.Sprintf(`
  RUNNING_MAJOR=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.postgres_major"}}{{.}}{{end}}' "$CONTAINER")
  if [ -z "$RUNNING_MAJOR" ]; then
    echo "Refusing to replace %s automatically." >&2
    echo "The running PostgreSQL major version cannot be verified." >&2
    exit 1
  fi
  if [ "$RUNNING_MAJOR" != %s ]; then
    echo "Refusing to replace %s automatically." >&2
    echo "Configured image: $IMAGE" >&2
    echo "Running image: $RUNNING_IMAGE" >&2
    echo "PostgreSQL major upgrades require a database migration procedure." >&2
    exit 1
  fi`, name, shell(postgresMajor), name)
	}

	return fmt.Sprintf(`set -eu
CONTAINER=%s
IMAGE=%s
VOLUME=%s
NETWORK=%s
CONFIG_HASH=%s
ENV_CHANGED_FILE=%s
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  RUNNING_PROJECT=$(docker inspect --format '{{with index .Config.Labels "slipway.project"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_ENV=$(docker inspect --format '{{with index .Config.Labels "slipway.env"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_ACCESSORY=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_TYPE=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.type"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_VOLUME=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.volume"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_IMAGE=$(docker inspect --format '{{.Config.Image}}' "$CONTAINER")
  if [ "$RUNNING_PROJECT" != %s ] || [ "$RUNNING_ENV" != %s ] || [ "$RUNNING_ACCESSORY" != %s ]; then
    echo "refusing to replace unmanaged or differently owned container $CONTAINER" >&2
    exit 1
  fi
  if [ "$RUNNING_TYPE" != %s ]; then
    echo "refusing to change accessory %s from type $RUNNING_TYPE to %s" >&2
    exit 1
  fi
  if [ -n "$RUNNING_VOLUME" ] && [ "$RUNNING_VOLUME" != "$VOLUME" ]; then
    echo "refusing to replace persistent volume $RUNNING_VOLUME with $VOLUME for accessory %s" >&2
    exit 1
  fi
%s
fi
docker pull "$IMAGE"
PULLED_IMAGE_ID=$(docker image inspect --format '{{.Id}}' "$IMAGE")
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  RUNNING_HASH=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.config_hash"}}{{.}}{{end}}' "$CONTAINER")
  RUNNING_IMAGE=$(docker inspect --format '{{.Config.Image}}' "$CONTAINER")
  RUNNING_IMAGE_ID=$(docker inspect --format '{{.Image}}' "$CONTAINER")
  if [ "$RUNNING_HASH" = "$CONFIG_HASH" ] && [ "$RUNNING_IMAGE" = "$IMAGE" ] && [ "$RUNNING_IMAGE_ID" = "$PULLED_IMAGE_ID" ] && [ ! -f "$ENV_CHANGED_FILE" ]; then
    [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER")" = true ] || docker start "$CONTAINER" >/dev/null
    exit 0
  fi
  echo "WARNING: explicitly recreating accessory %s; persistent volume $VOLUME is preserved" >&2
fi
docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
docker volume inspect "$VOLUME" >/dev/null 2>&1 || docker volume create --label %s --label %s "$VOLUME" >/dev/null
if docker inspect "$CONTAINER" >/dev/null 2>&1; then
  docker rm -f "$CONTAINER" >/dev/null
fi
%s
rm -f "$ENV_CHANGED_FILE"`,
		shell(container),
		shell(accessory.Image),
		shell(accessory.Storage.Volume),
		shell(d.Network),
		shell(configHash),
		shell(changedFile),
		shell(d.Project),
		shell(d.EnvName),
		shell(name),
		shell(accessory.Type),
		name,
		accessory.Type,
		name,
		postgresGuard,
		name,
		shell("slipway.project="+d.Project),
		shell("slipway.env="+d.EnvName),
		shellJoin(runArgs),
	)
}

func (d Docker) inspectScript(name string, accessory config.Accessory) string {
	container := d.containerName(name)
	return fmt.Sprintf(`set -eu
CONTAINER=%s
if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  printf 'managed=true\nstatus=missing\nhealth=missing\nimage=\nconfig_hash=\nvolume=\n'
  exit 0
fi
RUNNING_PROJECT=$(docker inspect --format '{{with index .Config.Labels "slipway.project"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_ENV=$(docker inspect --format '{{with index .Config.Labels "slipway.env"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_ACCESSORY=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_TYPE=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.type"}}{{.}}{{end}}' "$CONTAINER")
MANAGED=false
if [ "$RUNNING_PROJECT" = %s ] && [ "$RUNNING_ENV" = %s ] && [ "$RUNNING_ACCESSORY" = %s ] && [ "$RUNNING_TYPE" = %s ]; then
  MANAGED=true
fi
printf 'managed=%%s\n' "$MANAGED"
printf 'status=%%s\n' "$(docker inspect --format '{{.State.Status}}' "$CONTAINER")"
printf 'health=%%s\n' "$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER")"
printf 'image=%%s\n' "$(docker inspect --format '{{.Config.Image}}' "$CONTAINER")"
printf 'config_hash=%%s\n' "$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.config_hash"}}{{.}}{{end}}' "$CONTAINER")"
printf 'volume=%%s\n' "$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.volume"}}{{.}}{{end}}' "$CONTAINER")"`, shell(container), shell(d.Project), shell(d.EnvName), shell(name), shell(accessory.Type))
}

func (d Docker) waitHealthyCommand(server config.Server, name string, configured config.Accessory) remote.Command {
	script := d.managedGuardScript(name, configured.Type) + fmt.Sprintf(`
for i in $(seq 1 30); do
  HEALTH=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER")
  case "$HEALTH" in
    healthy) exit 0 ;;
    unhealthy) docker logs --tail 80 "$CONTAINER" || true; exit 1 ;;
  esac
  sleep 2
done
docker logs --tail 80 "$CONTAINER" || true
echo "accessory %s did not become healthy" >&2
exit 1`, name)
	return d.remoteCommand(server, "wait for accessory "+name+" health check", script)
}

func (d Docker) managedGuardScript(name string, accessoryType string) string {
	return fmt.Sprintf(`set -eu
CONTAINER=%s
if ! docker inspect "$CONTAINER" >/dev/null 2>&1; then
  echo "required accessory %s does not exist; run slipway accessory apply" >&2
  exit 1
fi
RUNNING_PROJECT=$(docker inspect --format '{{with index .Config.Labels "slipway.project"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_ENV=$(docker inspect --format '{{with index .Config.Labels "slipway.env"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_ACCESSORY=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory"}}{{.}}{{end}}' "$CONTAINER")
RUNNING_TYPE=$(docker inspect --format '{{with index .Config.Labels "slipway.accessory.type"}}{{.}}{{end}}' "$CONTAINER")
if [ "$RUNNING_PROJECT" != %s ] || [ "$RUNNING_ENV" != %s ] || [ "$RUNNING_ACCESSORY" != %s ] || [ "$RUNNING_TYPE" != %s ]; then
  echo "refusing to operate on unmanaged or differently owned container $CONTAINER" >&2
  exit 1
fi`, shell(d.containerName(name)), name, shell(d.Project), shell(d.EnvName), shell(name), shell(accessoryType))
}

func (d Docker) containerName(name string) string {
	return safe(strings.Join([]string{d.Project, d.EnvName, name}, "_"))
}

func (d Docker) remoteCommand(server config.Server, description string, script string) remote.Command {
	return remote.Command{
		Host:        server.Host,
		SSHUser:     server.SSHUser,
		SSHPort:     server.SSHPort,
		Description: description,
		Script:      script,
	}
}

func (d Docker) sensitiveRemoteCommand(server config.Server, description string, script string, stdin string) remote.Command {
	command := d.remoteCommand(server, description, script)
	command.Stdin = stdin
	command.Sensitive = true
	return command
}

func renderEnv(accessory config.Accessory, secrets map[string]string) (string, error) {
	lines := make([]string, 0, len(accessory.Env)+len(accessory.Secrets))
	for key, value := range accessory.Env {
		line, err := envLine(key, value)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	for _, name := range accessory.Secrets {
		value, ok := secrets[name]
		if !ok {
			return "", fmt.Errorf("secret %s was not resolved", name)
		}
		line, err := envLine(name, value)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n", nil
}

func configFingerprint(accessory config.Accessory) string {
	lines := []string{
		"type=" + accessory.Type,
		"image=" + accessory.Image,
		"volume=" + accessory.Storage.Volume,
	}
	for key, value := range accessory.Env {
		lines = append(lines, "env:"+key+"="+value)
	}
	for _, name := range accessory.Secrets {
		lines = append(lines, "secret:"+name)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func envLine(name string, value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", fmt.Errorf("env file value for %s contains unsupported newline", name)
	}
	return name + "=" + value, nil
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shell(arg))
	}
	return strings.Join(quoted, " ")
}

func shell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func safe(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
