#!/bin/bash
# Migration Runner Script for Search Quality Improvements
# This script applies the database migration 000043

set -e

MIGRATION_FILE="migrations/000043_search_quality_improvements.up.sql"
ENV_FILE=".env"

# Load environment variables if they exist
if [ -f "$ENV_FILE" ]; then
  set -a
  source "$ENV_FILE"
  set +a
fi

# Use defaults if not set
DATABASE_URL="${APP_DATABASE_URL:-postgresql://dev:dev@localhost:5432/dev}"
MIGRATIONS_DIR="${APP_MIGRATIONS_DIR:-migrations}"

echo "🔧 Message Search Migration Runner"
echo "=================================="
echo ""
echo "Database: $DATABASE_URL"
echo "Migrations Dir: $MIGRATIONS_DIR"
echo "Migration File: $MIGRATION_FILE"
echo ""

# Check if migration file exists
if [ ! -f "$MIGRATION_FILE" ]; then
  echo "❌ Migration file not found: $MIGRATION_FILE"
  exit 1
fi

echo "✅ Migration file found"
echo ""

# Create a temporary Go script to apply the migration
cat > /tmp/migrate_temp.go << 'EOF'
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Args[1]
	migrationFile := os.Args[2]

	ctx := context.Background()

	// Connect to database
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fmt.Printf("❌ Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("❌ Database connection failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Connected to database")

	// Create schema_migrations table if not exists
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		fmt.Printf("❌ Failed to create schema_migrations table: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ schema_migrations table ready")

	// Extract filename from path
	parts := strings.Split(migrationFile, "/")
	filename := parts[len(parts)-1]

	// Check if already applied
	var applied string
	err = pool.QueryRow(ctx, `SELECT filename FROM schema_migrations WHERE filename = $1`, filename).Scan(&applied)
	if err == nil {
		fmt.Printf("⚠️  Migration already applied: %s\n", filename)
		os.Exit(0)
	}

	// Read migration file
	body, err := os.ReadFile(migrationFile)
	if err != nil {
		fmt.Printf("❌ Failed to read migration file: %v\n", err)
		os.Exit(1)
	}

	// Apply migration
	fmt.Printf("📝 Applying migration: %s\n", filename)
	if _, err := pool.Exec(ctx, string(body)); err != nil {
		fmt.Printf("❌ Migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Migration SQL applied")

	// Record migration
	if _, err := pool.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, filename); err != nil {
		fmt.Printf("❌ Failed to record migration: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Migration recorded in schema_migrations")

	fmt.Println("")
	fmt.Println("🎉 Migration completed successfully!")
}
EOF

echo "📋 To apply this migration, you have several options:"
echo ""
echo "Option 1: Using the API server (recommended for production)"
echo "  $ APP_AUTO_MIGRATE=1 APP_DATABASE_URL='${DATABASE_URL}' go run ./cmd/api/main.go"
echo ""
echo "Option 2: Using docker-compose (recommended for local development)"
echo "  $ docker-compose up postgres"
echo "  # Wait for postgres to start, then:"
echo "  $ APP_AUTO_MIGRATE=1 APP_DATABASE_URL='postgresql://dev:dev@localhost:5432/dev' go run ./cmd/api/main.go"
echo ""
echo "Option 3: Manual SQL execution with psql"
echo "  $ psql '${DATABASE_URL}' < ${MIGRATION_FILE}"
echo ""
echo "Option 4: Docker postgres container"
echo "  $ docker run -it --rm --volume \"\$(pwd)\":/migration postgres:15 psql -h host.docker.internal -U dev -d dev -f /migration/${MIGRATION_FILE}"
echo ""
echo "=================================="
echo "✅ Migration files created and ready!"
echo ""
echo "Files created:"
echo "  - migrations/000043_search_quality_improvements.up.sql"
echo "  - migrations/000043_search_quality_improvements.down.sql"
echo ""
