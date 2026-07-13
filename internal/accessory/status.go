package accessory

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/s4m1nd/slipway/internal/console"
)

type Status struct {
	Name       string
	Type       string
	HostName   string
	Host       string
	Exists     bool
	Managed    bool
	State      string
	Health     string
	Image      string
	Volume     string
	ConfigHash string
}

func ParseStatus(target Target, output string) (Status, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" {
			return Status{}, fmt.Errorf("malformed accessory status line %q", line)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return Status{}, err
	}
	for _, key := range []string{"managed", "status", "health", "image", "config_hash", "volume"} {
		if _, ok := values[key]; !ok {
			return Status{}, fmt.Errorf("accessory status is missing %q", key)
		}
	}
	if values["managed"] != "true" && values["managed"] != "false" {
		return Status{}, fmt.Errorf("accessory status managed must be true or false, got %q", values["managed"])
	}
	return Status{
		Name:       target.Name,
		Type:       target.Config.Type,
		HostName:   target.HostName,
		Host:       target.Server.Host,
		Exists:     values["status"] != "missing",
		Managed:    values["managed"] == "true",
		State:      values["status"],
		Health:     values["health"],
		Image:      values["image"],
		Volume:     values["volume"],
		ConfigHash: values["config_hash"],
	}, nil
}

func RenderStatuses(w io.Writer, project string, environment string, statuses []Status) {
	c := console.New(w, w)
	c.Title(fmt.Sprintf("%s/%s accessories", project, environment))
	fmt.Fprintln(w)
	if len(statuses) == 0 {
		fmt.Fprintln(w, "  no accessories")
		return
	}
	sort.SliceStable(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	fmt.Fprintf(w, "%-14s %-10s %-14s %-12s %-12s %-24s %s\n", "NAME", "TYPE", "HOST", "STATUS", "HEALTH", "IMAGE", "VOLUME")
	for _, status := range statuses {
		styles := []console.Style(nil)
		state := status.State
		if status.Exists && !status.Managed {
			state = "unmanaged"
		}
		if status.Health == "healthy" && status.Managed {
			styles = []console.Style{console.StyleGreen}
		} else if status.Exists {
			styles = []console.Style{console.StyleYellow}
		} else {
			styles = []console.Style{console.StyleDim}
		}
		fmt.Fprintf(w, "%-14s %-10s %-14s %-12s ", status.Name, status.Type, status.HostName, state)
		fmt.Fprintf(w, "%-12s", c.Paint(status.Health, styles...))
		fmt.Fprintf(w, " %-24s %s\n", status.Image, status.Volume)
	}
}
