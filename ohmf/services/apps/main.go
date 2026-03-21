package main

import (
	"context"
	"os"

	"github.com/f8573/Messages/pkg/observability"
)

func main() {
	observability.Init()
	dataFile := os.Getenv("DATA_FILE")
	if dataFile == "" {
		dataFile = "ohmf/services/apps/data/registry.json"
	}
	migrationsDir := os.Getenv("APP_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "ohmf/services/apps/migrations"
	}
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = ":18086"
	}
	server := newAppsServer(dataFile)
	if dsn := os.Getenv("APP_DB_DSN"); dsn != "" {
		ctx := context.Background()
		pool, err := newRegistryPool(ctx, dsn)
		if err != nil {
			observability.Logger.Printf("registry db connect error: %v", err)
			os.Exit(1)
		}
		defer pool.Close()
		if err := applyRegistryMigrations(ctx, pool, migrationsDir); err != nil {
			observability.Logger.Printf("registry migration error: %v", err)
			os.Exit(1)
		}
		server = newAppsServerWithDB(dataFile, pool)
		observability.Logger.Printf("apps registry using postgres backend with migrations from %s", migrationsDir)
	} else {
		observability.Logger.Printf("apps registry using file backend at %s", dataFile)
	}
	observability.Logger.Printf("apps registry listening on %s", addr)
	if err := httpListenAndServe(addr, makeHandler(server)); err != nil {
		observability.Logger.Printf("listen error: %v", err)
		os.Exit(1)
	}
}
