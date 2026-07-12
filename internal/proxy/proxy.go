package proxy

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

type Manager interface {
	Provision(config.Server) []remote.Command
	Switch(config.Server, SwitchInput) []remote.Command
	Sync(config.Server, SwitchInput) []remote.Command
}

type SwitchInput struct {
	Services map[string]config.Service
	Routes   []config.ProxyRoute
}

type Caddy struct {
	Project     string
	EnvName     string
	Root        string
	Network     string
	ListenHTTP  int
	ListenHTTPS int
}

func (c Caddy) Provision(server config.Server) []remote.Command {
	container := caddyContainer(c.Project, c.EnvName)
	script := fmt.Sprintf(`set -eu
ROOT=%s
CADDY_CONTAINER=%s
NETWORK=%s
mkdir -p "$ROOT"/proxy
if [ ! -f "$ROOT/proxy/Caddyfile" ]; then
  printf ':80 {\n\trespond "Slipway ready" 200\n}\n' > "$ROOT/proxy/Caddyfile"
fi
if docker inspect "$CADDY_CONTAINER" >/dev/null 2>&1; then
  running=$(docker inspect -f '{{.State.Running}}' "$CADDY_CONTAINER" 2>/dev/null || printf false)
  if [ "$running" = true ]; then
    exit 0
  fi
  docker start "$CADDY_CONTAINER"
  exit 0
fi
docker run -d --restart unless-stopped --name "$CADDY_CONTAINER" --network "$NETWORK" -p %d:80 -p %d:443 -v "$ROOT/proxy:/etc/caddy" caddy:2`, shell(c.Root), shell(container), shell(c.Network), c.ListenHTTP, c.ListenHTTPS)
	return []remote.Command{c.remoteCommand(server, "start Caddy", script)}
}

func (c Caddy) Switch(server config.Server, input SwitchInput) []remote.Command {
	return c.writeRoutes(server, input, "switch Caddy routes", targetColorAssignment)
}

func (c Caddy) Sync(server config.Server, input SwitchInput) []remote.Command {
	return c.writeRoutes(server, input, "sync Caddy routes", activeColorAssignment)
}

func (c Caddy) writeRoutes(server config.Server, input SwitchInput, description string, colorAssignment func(Caddy, string) string) []remote.Command {
	services := routeServiceNames(input.Routes)
	var vars []string
	for _, serviceName := range services {
		vars = append(vars, colorAssignment(c, serviceName))
	}

	var blocks []string
	for _, route := range input.Routes {
		service := input.Services[route.Service]
		host := route.Host
		if !route.TLS && !strings.HasPrefix(host, "http://") {
			host = "http://" + host
		}
		blocks = append(blocks, fmt.Sprintf("%s {\n\treverse_proxy %s_%s:%d\n}", host, containerBase(c.Project, c.EnvName, route.Service), colorPlaceholder(route.Service), service.InternalPort))
	}

	script := c.caddyfileUpdateScript(vars, colorReplacements(services), strings.Join(blocks, "\n\n"))
	command := c.remoteCommand(server, description, script)
	command.OutputMode = remote.OutputCaddySummary
	return []remote.Command{command}
}

func (c Caddy) caddyfileUpdateScript(vars []string, replacements []string, contents string) string {
	sections := []string{
		"set -eu",
		fmt.Sprintf("ROOT=%s", shell(c.Root)),
		fmt.Sprintf("CADDY_CONTAINER=%s", shell(caddyContainer(c.Project, c.EnvName))),
		`PROXY_DIR="$ROOT/proxy"`,
		`ACTIVE_CADDYFILE="$PROXY_DIR/Caddyfile"`,
		`if [ ! -f "$ACTIVE_CADDYFILE" ]; then echo "active Caddyfile missing: $ACTIVE_CADDYFILE" >&2; exit 1; fi`,
		`TIMESTAMP=$(date -u +%Y%m%dT%H%M%SZ)`,
		`TMP_NAME="Caddyfile.tmp.$TIMESTAMP.$$"`,
		`TMP_CADDYFILE="$PROXY_DIR/$TMP_NAME"`,
		`BACKUP_CADDYFILE="$PROXY_DIR/Caddyfile.backup.$TIMESTAMP"`,
		`trap 'rm -f "$TMP_CADDYFILE"' EXIT`,
	}
	if len(vars) > 0 {
		sections = append(sections, strings.Join(vars, "\n"))
	}
	sections = append(sections,
		fmt.Sprintf("cat > \"$TMP_CADDYFILE\" <<'CADDY'\n%s\nCADDY", contents),
	)
	if len(replacements) > 0 {
		sections = append(sections, strings.Join(replacements, "\n"))
	}
	sections = append(sections,
		`docker exec "$CADDY_CONTAINER" caddy validate --config "/etc/caddy/$TMP_NAME"`,
		`cp "$ACTIVE_CADDYFILE" "$BACKUP_CADDYFILE"`,
		`mv "$TMP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`if ! docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile; then
  cp "$BACKUP_CADDYFILE" "$ACTIVE_CADDYFILE"
  docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile || true
  exit 1
fi`,
	)
	return strings.Join(sections, "\n")
}

func colorReplacements(serviceNames []string) []string {
	var replacements []string
	for _, serviceName := range serviceNames {
		replacements = append(replacements, fmt.Sprintf(`sed -i "s/%s/$%s/g" "$TMP_CADDYFILE"`, colorPlaceholder(serviceName), colorVariable(serviceName)))
	}
	return replacements
}

func (c Caddy) stateFile(serviceName string) string {
	return path.Join(c.Root, "state", serviceName+".json")
}

func (c Caddy) remoteCommand(server config.Server, description string, script string) remote.Command {
	return remote.Command{Host: server.Host, SSHUser: server.SSHUser, SSHPort: server.SSHPort, Description: description, Script: script}
}

func routeServiceNames(routes []config.ProxyRoute) []string {
	seen := map[string]bool{}
	var names []string
	for _, route := range routes {
		if !seen[route.Service] {
			seen[route.Service] = true
			names = append(names, route.Service)
		}
	}
	sort.Strings(names)
	return names
}

func targetColorScript(stateFile string) string {
	return fmt.Sprintf("if [ -f %s ] && grep -Eq '\"color\"[[:space:]]*:[[:space:]]*\"blue\"' %s; then printf green; else printf blue; fi", shell(stateFile), shell(stateFile))
}

func targetColorAssignment(c Caddy, serviceName string) string {
	return fmt.Sprintf("%s=$(%s)", colorVariable(serviceName), targetColorScript(c.stateFile(serviceName)))
}

func activeColorAssignment(c Caddy, serviceName string) string {
	state := c.stateFile(serviceName)
	return fmt.Sprintf("%s=$(STATE=%s\n%s\nCOLOR=$(json_field color \"$STATE\")\ncase \"$COLOR\" in\n  blue|green) printf '%%s' \"$COLOR\" ;;\n  \"\") echo \"missing color for %s in $STATE\" >&2; exit 1 ;;\n  *) echo \"invalid color for %s in $STATE: $COLOR\" >&2; exit 1 ;;\nesac)", colorVariable(serviceName), shell(state), jsonFieldFunction(), serviceName, serviceName)
}

func jsonFieldFunction() string {
	return "json_field() {\n  key=$1\n  file=$2\n  sed -n \"s/.*\\\"$key\\\"[[:space:]]*:[[:space:]]*\\\"\\([^\\\"]*\\)\\\".*/\\1/p\" \"$file\" | head -n 1\n}"
}

func colorVariable(serviceName string) string {
	return "COLOR_" + strings.ToUpper(safe(serviceName))
}

func colorPlaceholder(serviceName string) string {
	return "__SLIPWAY_" + colorVariable(serviceName) + "__"
}

func containerBase(project, env, service string) string {
	return safe(project + "_" + env + "_" + service)
}

func caddyContainer(project, env string) string {
	return safe(project + "_" + env + "_caddy")
}

func safe(value string) string {
	replacer := strings.NewReplacer("/", "_", ":", "_", ".", "_", "-", "_")
	return replacer.Replace(value)
}

func shell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
