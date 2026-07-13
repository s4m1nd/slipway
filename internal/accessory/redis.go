package accessory

type preset struct {
	dataDirectory string
	healthCommand string
	startArgs     []string
	user          string
}

func redisPreset() preset {
	return preset{
		dataDirectory: "/data",
		healthCommand: `redis-cli -a "$REDIS_PASSWORD" --no-auth-warning ping | grep -Fx PONG`,
		user:          "redis",
		startArgs: []string{
			"sh",
			"-c",
			`exec redis-server --appendonly yes --requirepass "$REDIS_PASSWORD"`,
		},
	}
}

func presetFor(accessoryType string) preset {
	switch accessoryType {
	case "postgres":
		return postgresPreset()
	case "redis":
		return redisPreset()
	default:
		return preset{}
	}
}
