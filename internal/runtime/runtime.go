package runtime

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

type Runtime interface {
	ProvisionHost(config.Server) []remote.Command
	AcquireLock(config.Server, LockOptions) remote.Command
	ReleaseLock(config.Server, LockOptions) remote.Command
	LoginLocal(config.Registry, string) remote.Command
	LoginRemote(config.Server, config.Registry, string) remote.Command
	BuildService(string, config.Service, string) remote.Command
	PushService(string, string) remote.Command
	UploadEnv(config.Server, string, string, string) remote.Command
	StartService(config.Server, string, config.Service, string, string) remote.Command
	WaitHealthy(config.Server, string, config.Service) remote.Command
	RecordActive(config.Server, string, string, string) remote.Command
	StartPrevious(config.Server, string, config.Service) remote.Command
	RollbackState(config.Server, string) remote.Command
	StopPrevious(config.Server, string) remote.Command
	CleanupService(config.Server, string, config.Service, CleanupOptions) remote.Command
	InspectService(config.Server, string, config.Service) remote.Command
	LogsService(config.Server, string, config.Service, LogsOptions) remote.Command
}

type CleanupOptions struct {
	KeepReleases int
}

type LockOptions struct {
	Operation string
	Timeout   time.Duration
	Owner     string
}

type LogsOptions struct {
	Color  string
	Tail   int
	Follow bool
}

type Docker struct {
	Project string
	EnvName string
	Root    string
	Network string
}

func (d Docker) ProvisionHost(server config.Server) []remote.Command {
	return []remote.Command{
		d.remoteCommand(server, "install Docker if needed", "command -v docker >/dev/null 2>&1 || curl -fsSL https://get.docker.com | sh"),
		d.remoteCommand(server, "prepare Slipway directories and Docker network", fmt.Sprintf("set -eu\nROOT=%s\nmkdir -p \"$ROOT\"/state \"$ROOT\"/secrets \"$ROOT\"/proxy\ndocker network inspect %s >/dev/null 2>&1 || docker network create %s", shell(d.Root), shell(d.Network), shell(d.Network))),
	}
}

func (d Docker) AcquireLock(server config.Server, options LockOptions) remote.Command {
	return d.remoteCommand(server, "acquire deploy lock", d.acquireLockScript(options))
}

func (d Docker) ReleaseLock(server config.Server, options LockOptions) remote.Command {
	return d.remoteCommand(server, "release deploy lock", d.releaseLockScript(options))
}

func (d Docker) LoginLocal(registry config.Registry, password string) remote.Command {
	return remote.Command{
		Description: "log in to registry locally",
		Script:      fmt.Sprintf("docker login %s -u %s --password-stdin", shell(registry.Server), shell(registry.Username)),
		Stdin:       password,
		Sensitive:   true,
		OutputMode:  remote.OutputQuietOnSuccess,
	}
}

func (d Docker) LoginRemote(server config.Server, registry config.Registry, password string) remote.Command {
	command := d.sensitiveRemoteCommand(server, "log in to registry on host", fmt.Sprintf("docker login %s -u %s --password-stdin", shell(registry.Server), shell(registry.Username)), password)
	command.OutputMode = remote.OutputQuietOnSuccess
	return command
}

func (d Docker) BuildService(serviceName string, service config.Service, image string) remote.Command {
	return remote.Command{
		Description: fmt.Sprintf("build image for %s", serviceName),
		Script:      dockerBuildScript(service, image),
	}
}

func (d Docker) PushService(serviceName string, image string) remote.Command {
	return remote.Command{
		Description: fmt.Sprintf("push image for %s", serviceName),
		Script:      fmt.Sprintf("docker push %s", shell(image)),
	}
}

func (d Docker) UploadEnv(server config.Server, serviceName string, releaseID string, envText string) remote.Command {
	return d.sensitiveRemoteCommand(server, fmt.Sprintf("upload env for %s", serviceName), d.uploadEnvScript(serviceName, releaseID), envText)
}

func (d Docker) StartService(server config.Server, serviceName string, service config.Service, image string, releaseID string) remote.Command {
	command := d.remoteCommand(server, fmt.Sprintf("start inactive %s release", serviceName), d.startServiceScript(serviceName, service, image, releaseID))
	command.OutputMode = remote.OutputQuietOnSuccess
	return command
}

func (d Docker) WaitHealthy(server config.Server, serviceName string, service config.Service) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("wait for %s health check", serviceName), d.healthScript(serviceName, service))
}

func (d Docker) RecordActive(server config.Server, serviceName string, image string, releaseID string) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("record active %s release", serviceName), d.recordStateScript(serviceName, image, releaseID))
}

func (d Docker) StartPrevious(server config.Server, serviceName string, _ config.Service) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("start previous %s release", serviceName), d.startPreviousScript(serviceName))
}

func (d Docker) RollbackState(server config.Server, serviceName string) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("rollback state for %s", serviceName), d.rollbackStateScript(serviceName))
}

func (d Docker) StopPrevious(server config.Server, serviceName string) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("stop previous %s release", serviceName), d.stopPreviousScript(serviceName))
}

func (d Docker) CleanupService(server config.Server, serviceName string, service config.Service, options CleanupOptions) remote.Command {
	if options.KeepReleases < 2 {
		options.KeepReleases = 2
	}
	return d.remoteCommand(server, fmt.Sprintf("cleanup old %s release artifacts", serviceName), d.cleanupServiceScript(serviceName, service, options))
}

func (d Docker) InspectService(server config.Server, serviceName string, service config.Service) remote.Command {
	return d.remoteCommand(server, fmt.Sprintf("inspect status for %s", serviceName), d.inspectServiceScript(serviceName))
}

func (d Docker) LogsService(server config.Server, serviceName string, _ config.Service, options LogsOptions) remote.Command {
	color := options.Color
	if color == "" {
		color = "active"
	}
	return d.remoteCommand(server, fmt.Sprintf("logs for %s %s", serviceName, color), d.logsServiceScript(serviceName, options))
}

func (d Docker) startServiceScript(serviceName string, service config.Service, image string, releaseID string) string {
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	labels := []string{
		"--label slipway.project=" + shell(d.Project),
		"--label slipway.env=" + shell(d.EnvName),
		"--label slipway.service=" + shell(serviceName),
		"--label slipway.release=" + shell(releaseID),
	}
	return fmt.Sprintf("set -eu\nROOT=%s\nSERVICE=%s\nRELEASE=%s\nCOLOR=$(%s)\nCONTAINER=%s\"_$COLOR\"\nENV_FILE=\"$ROOT/secrets/${SERVICE}-$COLOR-${RELEASE}.env\"\ndocker pull %s\ndocker rm -f \"$CONTAINER\" >/dev/null 2>&1 || true\ndocker run -d --restart unless-stopped --name \"$CONTAINER\" --network %s --network-alias \"$CONTAINER\" --env-file \"$ENV_FILE\" %s %s", shell(d.Root), shell(serviceName), shell(releaseID), targetColorScript(state), shell(base), shell(image), shell(d.Network), strings.Join(labels, " "), shell(image))
}

func (d Docker) healthScript(serviceName string, service config.Service) string {
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	timeout := durationSeconds(service.HealthCheck.Timeout)
	return fmt.Sprintf("set -eu\nCOLOR=$(%s)\nURL=\"http://%s_$COLOR:%d%s\"\nfor i in $(seq 1 %d); do docker run --rm --network %s curlimages/curl:8.11.1 -fsS --max-time %d \"$URL\" >/dev/null && exit 0; sleep %s; done\ndocker logs --tail 80 %s\"_$COLOR\" || true\nexit 1", targetColorScript(state), base, service.HealthCheck.Port, service.HealthCheck.Path, service.HealthCheck.Retries, shell(d.Network), timeout, shell(service.HealthCheck.Interval), shell(base))
}

func (d Docker) recordStateScript(serviceName string, image string, releaseID string) string {
	state := d.stateFile(serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\nmkdir -p %s\nCOLOR=$(%s)\nPREVIOUS_RELEASE=\nPREVIOUS_COLOR=\nPREVIOUS_IMAGE=\njson_field() {\n  key=$1\n  file=$2\n  sed -n \"s/.*\\\"$key\\\"[[:space:]]*:[[:space:]]*\\\"\\([^\\\"]*\\)\\\".*/\\1/p\" \"$file\" | head -n 1\n}\nif [ -f \"$STATE\" ]; then\n  PREVIOUS_RELEASE=$(json_field release \"$STATE\" || true)\n  PREVIOUS_COLOR=$(json_field color \"$STATE\" || true)\n  PREVIOUS_IMAGE=$(json_field image \"$STATE\" || true)\nfi\nUPDATED_AT=$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\nprintf '{\"project\":%s,\"environment\":%s,\"service\":%s,\"release\":%s,\"color\":\"%%s\",\"image\":%s,\"previous_release\":\"%%s\",\"previous_color\":\"%%s\",\"previous_image\":\"%%s\",\"updated_at\":\"%%s\"}\\n' \"$COLOR\" \"$PREVIOUS_RELEASE\" \"$PREVIOUS_COLOR\" \"$PREVIOUS_IMAGE\" \"$UPDATED_AT\" > \"$STATE.tmp.$$\"\nmv \"$STATE.tmp.$$\" \"$STATE\"", shell(state), shell(path.Dir(state)), targetColorScript(state), shellJSON(d.Project), shellJSON(d.EnvName), shellJSON(serviceName), shellJSON(releaseID), shellJSON(image))
}

func (d Docker) startPreviousScript(serviceName string) string {
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\nBASE=%s\n%s\nPREVIOUS_COLOR=$(json_field previous_color \"$STATE\")\ncase \"$PREVIOUS_COLOR\" in blue|green) ;; *) echo \"invalid previous_color for %s\" >&2; exit 1 ;; esac\ndocker start \"${BASE}_${PREVIOUS_COLOR}\"", shell(state), shell(base), jsonFieldFunction(), serviceName)
}

func (d Docker) rollbackStateScript(serviceName string) string {
	state := d.stateFile(serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\n%s\nACTIVE_RELEASE=$(json_field release \"$STATE\")\nACTIVE_COLOR=$(json_field color \"$STATE\")\nACTIVE_IMAGE=$(json_field image \"$STATE\")\nPREVIOUS_RELEASE=$(json_field previous_release \"$STATE\")\nPREVIOUS_COLOR=$(json_field previous_color \"$STATE\")\nPREVIOUS_IMAGE=$(json_field previous_image \"$STATE\")\ncase \"$ACTIVE_COLOR\" in blue|green) ;; *) echo \"invalid active_color for %s\" >&2; exit 1 ;; esac\ncase \"$PREVIOUS_COLOR\" in blue|green) ;; *) echo \"invalid previous_color for %s\" >&2; exit 1 ;; esac\nUPDATED_AT=$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\nprintf '{\"project\":%s,\"environment\":%s,\"service\":%s,\"release\":\"%%s\",\"color\":\"%%s\",\"image\":\"%%s\",\"previous_release\":\"%%s\",\"previous_color\":\"%%s\",\"previous_image\":\"%%s\",\"updated_at\":\"%%s\"}\\n' \"$PREVIOUS_RELEASE\" \"$PREVIOUS_COLOR\" \"$PREVIOUS_IMAGE\" \"$ACTIVE_RELEASE\" \"$ACTIVE_COLOR\" \"$ACTIVE_IMAGE\" \"$UPDATED_AT\" > \"$STATE.tmp.$$\"\nmv \"$STATE.tmp.$$\" \"$STATE\"", shell(state), jsonFieldFunction(), serviceName, serviceName, shellJSON(d.Project), shellJSON(d.EnvName), shellJSON(serviceName))
}

func (d Docker) stopPreviousScript(serviceName string) string {
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\nBASE=%s\n%s\nPREVIOUS_COLOR=$(json_field previous_color \"$STATE\")\ncase \"$PREVIOUS_COLOR\" in blue|green) ;; *) echo \"invalid previous_color for %s\" >&2; exit 1 ;; esac\nCONTAINER=\"${BASE}_${PREVIOUS_COLOR}\"\nif docker inspect \"$CONTAINER\" >/dev/null 2>&1; then\n  docker stop \"$CONTAINER\"\nfi", shell(state), shell(base), jsonFieldFunction(), serviceName)
}

func (d Docker) cleanupServiceScript(serviceName string, service config.Service, options CleanupOptions) string {
	state := d.stateFile(serviceName)
	imageRepo := imageRepository(service.Image)
	return fmt.Sprintf(`set -eu
ROOT=%s
STATE=%s
SECRETS_DIR="$ROOT/secrets"
SERVICE=%s
IMAGE_REPO=%s
KEEP_RELEASES=%d
%s
if [ ! -f "$STATE" ]; then
  echo "missing state for $SERVICE; refusing cleanup" >&2
  exit 1
fi
ACTIVE_RELEASE=$(json_field release "$STATE" || true)
ACTIVE_IMAGE=$(json_field image "$STATE" || true)
PREVIOUS_RELEASE=$(json_field previous_release "$STATE" || true)
PREVIOUS_IMAGE=$(json_field previous_image "$STATE" || true)
if [ -z "$ACTIVE_RELEASE" ] || [ -z "$ACTIVE_IMAGE" ]; then
  echo "missing active release metadata for $SERVICE; refusing cleanup" >&2
  exit 1
fi
release_in_list() {
  needle=$1
  list=$2
  [ -n "$needle" ] || return 1
  printf '%%s\n' "$list" | grep -Fx -- "$needle" >/dev/null 2>&1
}
should_keep_release() {
  release=$1
  newest=$2
  if [ "$release" = "$ACTIVE_RELEASE" ]; then return 0; fi
  if [ -n "$PREVIOUS_RELEASE" ] && [ "$release" = "$PREVIOUS_RELEASE" ]; then return 0; fi
  release_in_list "$release" "$newest"
}
is_release_tag() {
  printf '%%s\n' "$1" | grep -Eq '^[0-9]{8}T[0-9]{6}(\.[0-9]{9})?Z(-[A-Za-z0-9_.-]+)?$'
}
list_env_releases() {
  [ -d "$SECRETS_DIR" ] || return 0
  find "$SECRETS_DIR" -type f \( -name "${SERVICE}-blue-*.env" -o -name "${SERVICE}-green-*.env" \) -exec basename {} \; |
  while IFS= read -r name; do
    case "$name" in
      "$SERVICE"-blue-*.env)
        release=${name#"${SERVICE}-blue-"}
        release=${release%%.env}
        printf '%%s\n' "$release"
        ;;
      "$SERVICE"-green-*.env)
        release=${name#"${SERVICE}-green-"}
        release=${release%%.env}
        printf '%%s\n' "$release"
        ;;
    esac
  done
}
cleanup_env_files() {
  [ -d "$SECRETS_DIR" ] || return 0
  newest=$(list_env_releases | sort -ur | awk -v keep="$KEEP_RELEASES" 'NF && NR <= keep {print}')
  find "$SECRETS_DIR" -type f \( -name "${SERVICE}-blue-*.env" -o -name "${SERVICE}-green-*.env" \) |
  while IFS= read -r file; do
    name=$(basename "$file")
    case "$name" in
      "$SERVICE"-blue-*.env)
        release=${name#"${SERVICE}-blue-"}
        ;;
      "$SERVICE"-green-*.env)
        release=${name#"${SERVICE}-green-"}
        ;;
      *)
        continue
        ;;
    esac
    release=${release%%.env}
    if should_keep_release "$release" "$newest"; then
      continue
    fi
    rm -f -- "$file"
  done
}
list_image_tags() {
  [ -n "$IMAGE_REPO" ] || return 0
  docker image ls --format '{{.Repository}}:{{.Tag}}' 2>/dev/null |
  while IFS= read -r ref; do
    case "$ref" in
      "$IMAGE_REPO":*)
        tag=${ref#"${IMAGE_REPO}:"}
        if [ "$tag" = "<none>" ]; then
          continue
        fi
        if is_release_tag "$tag"; then
          printf '%%s\n' "$tag"
        fi
        ;;
    esac
  done
}
cleanup_images() {
  [ -n "$IMAGE_REPO" ] || return 0
  newest=$(list_image_tags | sort -ur | awk -v keep="$KEEP_RELEASES" 'NF && NR <= keep {print}')
  list_image_tags | sort -u |
  while IFS= read -r tag; do
    ref="${IMAGE_REPO}:${tag}"
    if [ "$ref" = "$ACTIVE_IMAGE" ]; then continue; fi
    if [ -n "$PREVIOUS_IMAGE" ] && [ "$ref" = "$PREVIOUS_IMAGE" ]; then continue; fi
    if release_in_list "$tag" "$newest"; then continue; fi
    docker image rm "$ref" >/dev/null 2>&1 || true
  done
}
cleanup_env_files
cleanup_images`, shell(d.Root), shell(state), shell(serviceName), shell(imageRepo), options.KeepReleases, jsonFieldFunction())
}

func (d Docker) uploadEnvScript(serviceName string, releaseID string) string {
	return fmt.Sprintf("set -eu\nROOT=%s\nSERVICE=%s\nRELEASE=%s\nmkdir -p \"$ROOT/secrets\"\nCOLOR=$(%s)\numask 077\ncat > \"$ROOT/secrets/${SERVICE}-$COLOR-${RELEASE}.env\"", shell(d.Root), shell(serviceName), shell(releaseID), targetColorScript(d.stateFile(serviceName)))
}

func (d Docker) inspectServiceScript(serviceName string) string {
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\nPROJECT=%s\nENVIRONMENT=%s\nSERVICE=%s\nBASE=%s\njson_field() {\n  key=$1\n  file=$2\n  sed -n \"s/.*\\\"$key\\\"[[:space:]]*:[[:space:]]*\\\"\\([^\\\"]*\\)\\\".*/\\1/p\" \"$file\" | head -n 1\n}\nemit() {\n  printf '%%s=%%s\\n' \"$1\" \"$2\"\n}\nif [ -f \"$STATE\" ]; then\n  emit state present\n  emit project \"$(json_field project \"$STATE\")\"\n  emit environment \"$(json_field environment \"$STATE\")\"\n  emit service \"$(json_field service \"$STATE\")\"\n  emit active_color \"$(json_field color \"$STATE\")\"\n  emit active_release \"$(json_field release \"$STATE\")\"\n  emit active_image \"$(json_field image \"$STATE\")\"\n  emit previous_color \"$(json_field previous_color \"$STATE\")\"\n  emit previous_release \"$(json_field previous_release \"$STATE\")\"\n  emit previous_image \"$(json_field previous_image \"$STATE\")\"\nelse\n  emit state missing\n  emit project \"$PROJECT\"\n  emit environment \"$ENVIRONMENT\"\n  emit service \"$SERVICE\"\n  emit active_color \"\"\n  emit active_release \"\"\n  emit active_image \"\"\n  emit previous_color \"\"\n  emit previous_release \"\"\n  emit previous_image \"\"\nfi\ninspect_container() {\n  color=$1\n  container=\"${BASE}_${color}\"\n  if docker inspect \"$container\" >/dev/null 2>&1; then\n    running=$(docker inspect -f '{{.State.Running}}' \"$container\" 2>/dev/null || printf false)\n    release=$(docker inspect -f '{{index .Config.Labels \"slipway.release\"}}' \"$container\" 2>/dev/null || true)\n    image=$(docker inspect -f '{{.Config.Image}}' \"$container\" 2>/dev/null || true)\n    emit \"${color}_exists\" true\n    emit \"${color}_running\" \"$running\"\n    emit \"${color}_release\" \"$release\"\n    emit \"${color}_image\" \"$image\"\n  else\n    emit \"${color}_exists\" false\n    emit \"${color}_running\" false\n    emit \"${color}_release\" \"\"\n    emit \"${color}_image\" \"\"\n  fi\n}\ninspect_container blue\ninspect_container green", shell(state), shell(d.Project), shell(d.EnvName), shell(serviceName), shell(base))
}

func (d Docker) logsServiceScript(serviceName string, options LogsOptions) string {
	color := options.Color
	if color == "" {
		color = "active"
	}
	follow := ""
	if options.Follow {
		follow = " -f"
	}
	state := d.stateFile(serviceName)
	base := containerBase(d.Project, d.EnvName, serviceName)
	return fmt.Sprintf("set -eu\nSTATE=%s\nBASE=%s\n%s\n%s\nCONTAINER=\"${BASE}_${COLOR}\"\nif ! docker inspect \"$CONTAINER\" >/dev/null 2>&1; then\n  echo \"container $CONTAINER does not exist\" >&2\n  exit 1\nfi\nexec docker logs --tail %d%s \"$CONTAINER\"", shell(state), shell(base), jsonFieldFunction(), logsColorScript(serviceName, color), options.Tail, follow)
}

func (d Docker) acquireLockScript(options LockOptions) string {
	timeoutSeconds := int(options.Timeout.Seconds())
	if timeoutSeconds < 1 {
		timeoutSeconds = int((30 * time.Minute).Seconds())
	}
	operation := strings.TrimSpace(options.Operation)
	if operation == "" {
		operation = "mutating command"
	}
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		owner = "slipway"
	}
	return fmt.Sprintf(`set -eu
LOCK_DIR=%s
LOCK_TIMEOUT_SECONDS=%d
LOCK_OWNER=%s
LOCK_COMMAND_LINE=%s
umask 077
mkdir -p "$(dirname "$LOCK_DIR")"
now_epoch() {
  date +%%s
}
write_metadata() {
  {
    printf 'owner=%%s\n' "$LOCK_OWNER"
    printf '%%s\n' "$LOCK_COMMAND_LINE"
    printf 'user=%%s\n' "$(id -un 2>/dev/null || whoami 2>/dev/null || printf unknown)"
    printf 'host=%%s\n' "$(hostname 2>/dev/null || printf unknown)"
    printf 'pid=%%s\n' "$$"
    printf 'created_at_epoch=%%s\n' "$(now_epoch)"
    printf 'timestamp=%%s\n' "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)"
  } > "$LOCK_DIR/metadata"
}
try_acquire() {
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    write_metadata
    exit 0
  fi
}
try_acquire
created=0
if [ -f "$LOCK_DIR/metadata" ]; then
  created=$(sed -n 's/^created_at_epoch=//p' "$LOCK_DIR/metadata" | head -n 1 || true)
fi
case "$created" in
  ''|*[!0-9]*) created=0 ;;
esac
if [ "$created" -gt 0 ]; then
  age=$(( $(now_epoch) - created ))
  if [ "$age" -ge "$LOCK_TIMEOUT_SECONDS" ]; then
    echo "removing stale deploy lock at $LOCK_DIR after ${age}s" >&2
    rm -rf -- "$LOCK_DIR"
    try_acquire
  fi
fi
echo "deploy lock is held at $LOCK_DIR" >&2
if [ -f "$LOCK_DIR/metadata" ]; then
  sed 's/^/  /' "$LOCK_DIR/metadata" >&2 || true
fi
exit 1`, shell(d.lockDir()), timeoutSeconds, shell(owner), shell("command="+operation))
}

func (d Docker) releaseLockScript(options LockOptions) string {
	owner := strings.TrimSpace(options.Owner)
	if owner == "" {
		owner = "slipway"
	}
	return fmt.Sprintf(`set -eu
LOCK_DIR=%s
LOCK_OWNER=%s
if [ ! -d "$LOCK_DIR" ]; then
  exit 0
fi
current_owner=
if [ -f "$LOCK_DIR/metadata" ]; then
  current_owner=$(sed -n 's/^owner=//p' "$LOCK_DIR/metadata" | head -n 1 || true)
fi
if [ "$current_owner" != "$LOCK_OWNER" ]; then
  echo "refusing to release deploy lock not owned by this Slipway run: $LOCK_DIR" >&2
  exit 1
fi
rm -rf -- "$LOCK_DIR"`, shell(d.lockDir()), shell(owner))
}

func (d Docker) stateFile(serviceName string) string {
	return path.Join(d.Root, "state", serviceName+".json")
}

func (d Docker) lockDir() string {
	return path.Join(d.Root, "locks", "deploy.lock")
}

func (d Docker) remoteCommand(server config.Server, description string, script string) remote.Command {
	return remote.Command{Host: server.Host, SSHUser: server.SSHUser, SSHPort: server.SSHPort, Description: description, Script: script}
}

func (d Docker) sensitiveRemoteCommand(server config.Server, description string, script string, stdin string) remote.Command {
	cmd := d.remoteCommand(server, description, script)
	cmd.Stdin = stdin
	cmd.Sensitive = true
	return cmd
}

func dockerBuildScript(service config.Service, image string) string {
	dockerfile := dockerfilePath(service.Build)
	args := []string{"docker build", "-t " + shell(image), "-f " + shell(dockerfile)}
	if service.Build.Target != "" {
		args = append(args, "--target "+shell(service.Build.Target))
	}
	if service.Build.Platform != "" {
		args = append(args, "--platform "+shell(service.Build.Platform))
	}
	for _, arg := range service.Build.Args {
		args = append(args, "--build-arg "+shell(arg))
	}
	args = append(args, shell(service.Build.Context))
	return strings.Join(args, " ")
}

func dockerfilePath(build config.Build) string {
	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if build.Context == "" || filepath.IsAbs(dockerfile) {
		return dockerfile
	}
	return filepath.Join(build.Context, dockerfile)
}

func imageRepository(image string) string {
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon]
	}
	return image
}

func targetColorScript(stateFile string) string {
	return fmt.Sprintf("if [ -f %s ] && grep -Eq '\"color\"[[:space:]]*:[[:space:]]*\"blue\"' %s; then printf green; else printf blue; fi", shell(stateFile), shell(stateFile))
}

func jsonFieldFunction() string {
	return "json_field() {\n  key=$1\n  file=$2\n  sed -n \"s/.*\\\"$key\\\"[[:space:]]*:[[:space:]]*\\\"\\([^\\\"]*\\)\\\".*/\\1/p\" \"$file\" | head -n 1\n}"
}

func logsColorScript(serviceName string, color string) string {
	switch color {
	case "previous":
		return fmt.Sprintf("if [ ! -f \"$STATE\" ]; then\n  echo \"missing state for %s; cannot resolve previous color\" >&2\n  exit 1\nfi\nCOLOR=$(json_field previous_color \"$STATE\")\ncase \"$COLOR\" in\n  blue|green) ;;\n  \"\") echo \"missing previous_color for %s in $STATE\" >&2; exit 1 ;;\n  *) echo \"invalid previous_color for %s in $STATE: $COLOR\" >&2; exit 1 ;;\nesac", serviceName, serviceName, serviceName)
	case "blue", "green":
		return "COLOR=" + color
	default:
		return fmt.Sprintf("if [ ! -f \"$STATE\" ]; then\n  echo \"missing state for %s; cannot resolve active color\" >&2\n  exit 1\nfi\nCOLOR=$(json_field color \"$STATE\")\ncase \"$COLOR\" in\n  blue|green) ;;\n  \"\") echo \"missing color for %s in $STATE\" >&2; exit 1 ;;\n  *) echo \"invalid color for %s in $STATE: $COLOR\" >&2; exit 1 ;;\nesac", serviceName, serviceName, serviceName)
	}
}

func durationSeconds(value string) int {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 2
	}
	seconds := int(duration.Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}

func containerBase(project, env, service string) string {
	return safe(project + "_" + env + "_" + service)
}

func safe(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", ".", "_", "-", "_")
	return replacer.Replace(value)
}

func shell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellJSON(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
