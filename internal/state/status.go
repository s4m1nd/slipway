package state

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/s4m1nd/slipway/internal/console"
)

type Target struct {
	HostName string
	Host     string
	SSHUser  string
	SSHPort  int
	Service  string
}

type Release struct {
	Color   string
	Release string
	Image   string
}

type Container struct {
	Exists  bool
	Running bool
	Release string
	Image   string
}

type ServiceStatus struct {
	Project       string
	Environment   string
	Service       string
	TargetService string
	HostName      string
	Host          string
	SSHUser       string
	SSHPort       int
	StateExists   bool
	Active        Release
	Previous      Release
	Blue          Container
	Green         Container
}

func ParseServiceStatus(target Target, output string) (ServiceStatus, error) {
	values, err := parseKeyValues(output)
	if err != nil {
		return ServiceStatus{}, err
	}
	if err := requireStatusKeys(values); err != nil {
		return ServiceStatus{}, err
	}

	stateValue := values["state"]
	if stateValue != "present" && stateValue != "missing" {
		return ServiceStatus{}, fmt.Errorf("status output state must be present or missing, got %q", stateValue)
	}
	blueExists, err := parseStatusBool("blue_exists", values["blue_exists"])
	if err != nil {
		return ServiceStatus{}, err
	}
	blueRunning, err := parseStatusBool("blue_running", values["blue_running"])
	if err != nil {
		return ServiceStatus{}, err
	}
	greenExists, err := parseStatusBool("green_exists", values["green_exists"])
	if err != nil {
		return ServiceStatus{}, err
	}
	greenRunning, err := parseStatusBool("green_running", values["green_running"])
	if err != nil {
		return ServiceStatus{}, err
	}

	service := values["service"]
	if service == "" {
		service = target.Service
	}

	return ServiceStatus{
		Project:       values["project"],
		Environment:   values["environment"],
		Service:       service,
		TargetService: target.Service,
		HostName:      target.HostName,
		Host:          target.Host,
		SSHUser:       target.SSHUser,
		SSHPort:       target.SSHPort,
		StateExists:   stateValue == "present",
		Active: Release{
			Color:   values["active_color"],
			Release: values["active_release"],
			Image:   values["active_image"],
		},
		Previous: Release{
			Color:   values["previous_color"],
			Release: values["previous_release"],
			Image:   values["previous_image"],
		},
		Blue: Container{
			Exists:  blueExists,
			Running: blueRunning,
			Release: values["blue_release"],
			Image:   values["blue_image"],
		},
		Green: Container{
			Exists:  greenExists,
			Running: greenRunning,
			Release: values["green_release"],
			Image:   values["green_image"],
		},
	}, nil
}

func RenderReport(w io.Writer, project string, environment string, statuses []ServiceStatus) {
	RenderReportWithConsole(console.New(w, w), project, environment, statuses)
}

func RenderReportWithConsole(c console.Console, project string, environment string, statuses []ServiceStatus) {
	c.Title(fmt.Sprintf("%s/%s status", project, environment))
	fmt.Fprintln(c.Out)
	if len(statuses) == 0 {
		fmt.Fprintln(c.Out, "  no services")
		return
	}

	sort.SliceStable(statuses, func(i, j int) bool {
		if statuses[i].Service != statuses[j].Service {
			return statuses[i].Service < statuses[j].Service
		}
		return statuses[i].HostID() < statuses[j].HostID()
	})

	writeStatusRow(c,
		statusCell{"SERVICE", 12, []console.Style{console.StyleBold}},
		statusCell{"HOST", 28, []console.Style{console.StyleBold}},
		statusCell{"ACTIVE", 24, []console.Style{console.StyleBold}},
		statusCell{"PREVIOUS", 24, []console.Style{console.StyleBold}},
		statusCell{"BLUE", 10, []console.Style{console.StyleBold, console.StyleBlue}},
		statusCell{"GREEN", 10, []console.Style{console.StyleBold, console.StyleGreen}},
	)
	for _, status := range statuses {
		writeStatusRow(c,
			statusCell{status.Service, 12, nil},
			statusCell{status.HostID(), 28, []console.Style{console.StyleDim}},
			statusCell{releaseSummary(status.StateExists, status.Active), 24, releaseStyles(status.Active)},
			statusCell{releaseSummary(status.StateExists, status.Previous), 24, releaseStyles(status.Previous)},
			statusCell{containerSummary(status.Blue), 10, containerStyles(status.Blue)},
			statusCell{containerSummary(status.Green), 10, containerStyles(status.Green)},
		)
	}
}

type statusCell struct {
	text   string
	width  int
	styles []console.Style
}

func writeStatusRow(c console.Console, cells ...statusCell) {
	for i, cell := range cells {
		if i > 0 {
			fmt.Fprint(c.Out, " ")
		}
		padded := fmt.Sprintf("%-*s", cell.width, cell.text)
		fmt.Fprint(c.Out, c.Paint(padded, cell.styles...))
	}
	fmt.Fprintln(c.Out)
}

func releaseStyles(release Release) []console.Style {
	switch release.Color {
	case "blue":
		return []console.Style{console.StyleBlue}
	case "green":
		return []console.Style{console.StyleGreen}
	default:
		return nil
	}
}

func containerStyles(container Container) []console.Style {
	if !container.Exists {
		return []console.Style{console.StyleDim}
	}
	if container.Running {
		return []console.Style{console.StyleGreen}
	}
	return []console.Style{console.StyleYellow}
}

func (s ServiceStatus) HostID() string {
	target := s.Host
	if s.SSHUser != "" {
		target = s.SSHUser + "@" + target
	}
	if s.SSHPort != 0 && s.SSHPort != 22 {
		target = fmt.Sprintf("%s:%d", target, s.SSHPort)
	}
	if s.HostName == "" {
		return target
	}
	return s.HostName + "/" + target
}

func parseKeyValues(output string) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("malformed status output line %d: expected KEY=VALUE", lineNumber)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func requireStatusKeys(values map[string]string) error {
	required := []string{
		"state",
		"project",
		"environment",
		"service",
		"active_color",
		"active_release",
		"active_image",
		"previous_color",
		"previous_release",
		"previous_image",
		"blue_exists",
		"blue_running",
		"blue_release",
		"blue_image",
		"green_exists",
		"green_running",
		"green_release",
		"green_image",
	}
	for _, key := range required {
		if _, ok := values[key]; !ok {
			return fmt.Errorf("missing status output key %q", key)
		}
	}
	return nil
}

type RollbackReadinessError struct {
	Problems []string
}

func (e *RollbackReadinessError) Error() string {
	return strings.Join(e.Problems, "\n")
}

func (e *RollbackReadinessError) add(status ServiceStatus, format string, args ...any) {
	e.Problems = append(e.Problems, fmt.Sprintf("%s: %s", statusLabel(status), fmt.Sprintf(format, args...)))
}

func (e *RollbackReadinessError) empty() bool { return len(e.Problems) == 0 }

func ValidateRollbackReady(statuses []ServiceStatus) error {
	readiness := &RollbackReadinessError{}
	for _, status := range statuses {
		validateRollbackReadyStatus(readiness, status)
	}
	if readiness.empty() {
		return nil
	}
	return readiness
}

func validateRollbackReadyStatus(readiness *RollbackReadinessError, status ServiceStatus) {
	if !status.StateExists {
		readiness.add(status, "state is missing")
	}
	if status.TargetService != "" && status.Service != status.TargetService {
		readiness.add(status, "state service %q does not match configured service %q", status.Service, status.TargetService)
	}
	if !validColor(status.Active.Color) {
		readiness.add(status, "active_color must be blue or green")
	}
	if strings.TrimSpace(status.Active.Release) == "" {
		readiness.add(status, "active_release is required")
	}
	if strings.TrimSpace(status.Active.Image) == "" {
		readiness.add(status, "active_image is required")
	}
	if !validColor(status.Previous.Color) {
		readiness.add(status, "previous_color must be blue or green")
	}
	if strings.TrimSpace(status.Previous.Release) == "" {
		readiness.add(status, "previous_release is required")
	}
	if strings.TrimSpace(status.Previous.Image) == "" {
		readiness.add(status, "previous_image is required")
	}
	if validColor(status.Active.Color) && validColor(status.Previous.Color) && status.Active.Color == status.Previous.Color {
		readiness.add(status, "previous_color must differ from active_color")
	}
	if validColor(status.Previous.Color) {
		container := status.containerForColor(status.Previous.Color)
		if !container.Exists {
			readiness.add(status, "previous %s container is missing", status.Previous.Color)
		}
	}
}

func (s ServiceStatus) containerForColor(color string) Container {
	if color == "blue" {
		return s.Blue
	}
	if color == "green" {
		return s.Green
	}
	return Container{}
}

func validColor(color string) bool {
	return color == "blue" || color == "green"
}

func statusLabel(status ServiceStatus) string {
	service := status.TargetService
	if service == "" {
		service = status.Service
	}
	if service == "" {
		service = "service"
	}
	return service + " on " + status.HostID()
}

func parseStatusBool(key string, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("status output key %q must be boolean, got %q", key, value)
	}
}

func releaseSummary(stateExists bool, release Release) string {
	if !stateExists {
		return "not deployed"
	}
	if release.Release == "" {
		return "-"
	}
	if release.Color == "" {
		return release.Release
	}
	return release.Color + " " + release.Release
}

func containerSummary(container Container) string {
	if !container.Exists {
		return "missing"
	}
	if container.Running {
		return "running"
	}
	return "stopped"
}
