package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env             string
	Addr            string
	LogLevel        string
	DBDSN           string
	RedisAddr       string
	JWTSecret       string
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	AutoMigrate     bool
	MigrationsDir   string
	AllowedOrigin   string
}

func Load() Config {
	return Config{
		Env:           get("APP_ENV", "dev"),
		Addr:          get("APP_ADDR", ":8080"),
		LogLevel:      get("APP_LOG_LEVEL", "info"),
		DBDSN:         get("APP_DB_DSN", "postgres://ohmf:ohmf@localhost:5432/ohmf?sslmode=disable"),
		RedisAddr:     get("APP_REDIS_ADDR", "localhost:6379"),
		JWTSecret:     get("APP_JWT_SECRET", "dev-only-change-me"),
		AccessTTL:     time.Duration(getInt("APP_ACCESS_TTL_MINUTES", 15)) * time.Minute,
		RefreshTTL:    time.Duration(getInt("APP_REFRESH_TTL_HOURS", 24*30)) * time.Hour,
		AutoMigrate:   getBool("APP_AUTO_MIGRATE", true),
		MigrationsDir: get("APP_MIGRATIONS_DIR", "migrations"),
		AllowedOrigin: get("APP_ALLOWED_ORIGIN", "*"),
	}
}

func get(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func getBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return d
	}
	return b
}
