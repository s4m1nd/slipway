package accessory

import (
	"github.com/s4m1nd/slipway/internal/config"
	"github.com/s4m1nd/slipway/internal/remote"
)

// Manager owns stable accessory containers without expanding the blue/green
// application runtime lifecycle.
type Manager interface {
	Apply(config.Server, string, config.Accessory, map[string]string) ([]remote.Command, error)
	Inspect(config.Server, string, config.Accessory) remote.Command
	Logs(config.Server, string, config.Accessory, LogsOptions) remote.Command
	Restart(config.Server, string, config.Accessory) []remote.Command
	Exec(config.Server, string, config.Accessory, []string) (remote.Command, error)
	Verify(config.Server, string, config.Accessory) remote.Command
}

type LogsOptions struct {
	Tail   int
	Follow bool
}

type Target struct {
	HostName string
	Server   config.Server
	Name     string
	Config   config.Accessory
}
