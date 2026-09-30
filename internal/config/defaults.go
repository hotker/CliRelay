package config

const (
	DefaultPanelGitHubRepository      = "https://github.com/kittors/codeProxy"
	DefaultPprofAddr                  = "127.0.0.1:8316"
	DefaultAutoUpdateChannel          = "main"
	DefaultAutoUpdateRepository       = "https://github.com/kittors/CliRelay"
	DefaultAutoUpdateDockerImage      = "ghcr.io/kittors/clirelay"
	DefaultAutoUpdateUpdaterURL       = "http://clirelay-updater:8320"
	DefaultModelRequestBodyMB         = 128
	DefaultRequestBodyDiskThresholdMB = 8

	// EnvAuthPath overrides auth-dir with the path visible inside the running container/process.
	EnvAuthPath = "AUTH_PATH"
	// EnvHost overrides the configured listen host. A slot behind a same-host
	// reverse proxy sets 127.0.0.1 so its plain-HTTP port is not reachable
	// from outside. Unlike a config.yaml edit, it does not hot-reload the
	// running process.
	EnvHost = "CLIRELAY_HOST"
	// EnvPort overrides the configured listen port for blue-green deploy slots.
	EnvPort = "CLIRELAY_PORT"
	// EnvLegacyPort keeps the existing Docker installer PORT environment useful.
	EnvLegacyPort = "PORT"
	// EnvPostgresDSN overrides postgres.dsn for container and secret-managed deployments.
	EnvPostgresDSN = "CLIRELAY_POSTGRES_DSN"
	// EnvRedisEnable overrides redis.enable.
	EnvRedisEnable = "CLIRELAY_REDIS_ENABLE"
	// EnvRedisAddr overrides redis.addr.
	EnvRedisAddr = "CLIRELAY_REDIS_ADDR"
	// EnvRedisPassword overrides redis.password.
	EnvRedisPassword = "CLIRELAY_REDIS_PASSWORD"
	// EnvRedisDB overrides redis.db.
	EnvRedisDB = "CLIRELAY_REDIS_DB"
)
