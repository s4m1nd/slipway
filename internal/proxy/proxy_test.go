package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

func TestTargetColorScriptHandlesFormattedStateJSON(t *testing.T) {
	state := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(state, []byte("{\n  \"previous_color\": \"green\",\n  \"color\": \"blue\"\n}\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	out, err := exec.Command("sh", "-c", targetColorScript(state)).Output()
	if err != nil {
		t.Fatalf("run target color script: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "green" {
		t.Fatalf("target color = %q, want green", got)
	}
}

func TestCaddyProvisionIsIdempotentAndDoesNotRemoveRunningContainer(t *testing.T) {
	command := testCaddy().Provision(testServer())[0]

	for _, forbidden := range []string{"docker rm -f", "docker stop"} {
		if strings.Contains(command.Script, forbidden) {
			t.Fatalf("provision should not disrupt a running Caddy container via %q:\n%s", forbidden, command.Script)
		}
	}
	for _, want := range []string{
		`CADDY_CONTAINER='demo_production_caddy'`,
		`docker inspect "$CADDY_CONTAINER"`,
		`docker start "$CADDY_CONTAINER"`,
		`docker run -d --restart unless-stopped --name "$CADDY_CONTAINER"`,
	} {
		if !strings.Contains(command.Script, want) {
			t.Fatalf("provision script missing %q:\n%s", want, command.Script)
		}
	}
}

func TestCaddySwitchUsesSafeCaddyfileUpdatePath(t *testing.T) {
	command := testCaddy().Switch(testServer(), testSwitchInput())[0]
	if command.OutputMode != remote.OutputCaddySummary {
		t.Fatalf("Caddy switch output mode = %q, want summary", command.OutputMode)
	}

	assertSafeCaddyfileUpdate(t, command.Script)
	assertOrder(t, command.Script,
		`cat > "$TMP_CADDYFILE" <<'CADDY'`,
		`sed -i "s/__SLIPWAY_COLOR_WEB__/$COLOR_WEB/g" "$TMP_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy validate --config "/etc/caddy/$TMP_NAME"`,
		`cp "$ACTIVE_CADDYFILE" "$BACKUP_CADDYFILE"`,
		`mv "$TMP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile`,
	)
}

func TestCaddySwitchRestoresBackupWhenReloadFails(t *testing.T) {
	command := testCaddy().Switch(testServer(), testSwitchInput())[0]

	assertOrder(t, command.Script,
		`if ! docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile; then`,
		`cp "$BACKUP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile || true`,
		`exit 1`,
	)
}

func TestCaddySyncUsesSameSafeCaddyfileUpdatePathAsSwitch(t *testing.T) {
	caddy := testCaddy()
	switchCommand := caddy.Switch(testServer(), testSwitchInput())[0]
	syncCommand := caddy.Sync(testServer(), testSwitchInput())[0]

	assertSafeCaddyfileUpdate(t, switchCommand.Script)
	assertSafeCaddyfileUpdate(t, syncCommand.Script)
	for _, marker := range []string{
		`TMP_NAME="Caddyfile.tmp.$TIMESTAMP.$$"`,
		`BACKUP_CADDYFILE="$PROXY_DIR/Caddyfile.backup.$TIMESTAMP"`,
		`cat > "$TMP_CADDYFILE" <<'CADDY'`,
		`sed -i "s/__SLIPWAY_COLOR_WEB__/$COLOR_WEB/g" "$TMP_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy validate --config "/etc/caddy/$TMP_NAME"`,
		`mv "$TMP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`cp "$BACKUP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
	} {
		if strings.Count(switchCommand.Script, marker) != strings.Count(syncCommand.Script, marker) {
			t.Fatalf("Switch and Sync differ for safety marker %q\nswitch:\n%s\nsync:\n%s", marker, switchCommand.Script, syncCommand.Script)
		}
	}
}

func TestCaddyRouteContentIsWrittenWithoutShellExpansion(t *testing.T) {
	input := testSwitchInput()
	input.Routes[0].Host = `app.$(touch /tmp/slipway-should-not-run).example.com`

	command := testCaddy().Switch(testServer(), input)[0]
	if !strings.Contains(command.Script, `cat > "$TMP_CADDYFILE" <<'CADDY'`) {
		t.Fatalf("Caddyfile heredoc should be single-quoted to avoid shell expansion:\n%s", command.Script)
	}
	if strings.Contains(command.Script, `demo_production_web_${COLOR_WEB}`) {
		t.Fatalf("Caddyfile should use a literal color placeholder, not shell expansion inside route content:\n%s", command.Script)
	}
	if !strings.Contains(command.Script, `sed -i "s/__SLIPWAY_COLOR_WEB__/$COLOR_WEB/g" "$TMP_CADDYFILE"`) {
		t.Fatalf("Caddyfile should replace the controlled color placeholder after writing the literal config:\n%s", command.Script)
	}
}

func testCaddy() Caddy {
	return Caddy{
		Project:     "demo",
		EnvName:     "production",
		Root:        "/opt/slipway/demo/production",
		Network:     "demo_production",
		ListenHTTP:  80,
		ListenHTTPS: 443,
	}
}

func testServer() config.Server {
	return config.Server{Host: "203.0.113.10", SSHUser: "deploy", SSHPort: 22}
}

func testSwitchInput() SwitchInput {
	return SwitchInput{
		Services: map[string]config.Service{
			"web": {InternalPort: 3000},
		},
		Routes: []config.ProxyRoute{
			{Host: "app.example.com", Service: "web", TLS: true},
		},
	}
}

func assertSafeCaddyfileUpdate(t *testing.T, script string) {
	t.Helper()
	for _, want := range []string{
		`PROXY_DIR="$ROOT/proxy"`,
		`ACTIVE_CADDYFILE="$PROXY_DIR/Caddyfile"`,
		`TIMESTAMP=$(date -u +%Y%m%dT%H%M%SZ)`,
		`TMP_NAME="Caddyfile.tmp.$TIMESTAMP.$$"`,
		`TMP_CADDYFILE="$PROXY_DIR/$TMP_NAME"`,
		`BACKUP_CADDYFILE="$PROXY_DIR/Caddyfile.backup.$TIMESTAMP"`,
		`trap 'rm -f "$TMP_CADDYFILE"' EXIT`,
		`cat > "$TMP_CADDYFILE" <<'CADDY'`,
		`sed -i "s/__SLIPWAY_COLOR_WEB__/$COLOR_WEB/g" "$TMP_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy validate --config "/etc/caddy/$TMP_NAME"`,
		`cp "$ACTIVE_CADDYFILE" "$BACKUP_CADDYFILE"`,
		`mv "$TMP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`if ! docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile; then`,
		`cp "$BACKUP_CADDYFILE" "$ACTIVE_CADDYFILE"`,
		`docker exec "$CADDY_CONTAINER" caddy reload --config /etc/caddy/Caddyfile || true`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
}

func assertOrder(t *testing.T, script string, ordered ...string) {
	t.Helper()
	offset := 0
	for _, want := range ordered {
		index := strings.Index(script[offset:], want)
		if index == -1 {
			t.Fatalf("script missing %q after byte %d:\n%s", want, offset, script)
		}
		offset += index + len(want)
	}
}
