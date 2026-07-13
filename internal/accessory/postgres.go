package accessory

func postgresPreset() preset {
	return preset{
		dataDirectory: "/var/lib/postgresql/data",
		healthCommand: `pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`,
	}
}
