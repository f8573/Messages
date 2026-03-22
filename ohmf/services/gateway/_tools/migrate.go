package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationInfo holds information about a migration
type MigrationInfo struct {
	Filename string
	Applied  bool
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "verify" {
		verifyMigrations()
		return
	}

	fmt.Println("=== Message Search Migration Helper ===\n")

	// Check if migration files exist
	checkMigrationFiles()

	// Offer to apply migrations
	offerApplyMigrations()
}

func checkMigrationFiles() {
	fmt.Println("📋 Checking migration files...")

	files := []string{
		"migrations/000043_search_quality_improvements.up.sql",
		"migrations/000043_search_quality_improvements.down.sql",
	}

	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			fmt.Printf("  ✅ %s\n", f)
		} else {
			fmt.Printf("  ❌ %s (not found)\n", f)
		}
	}
	fmt.Println()
}

func verifyMigrations() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("APP_DATABASE_URL")
	}
	if dbURL == "" {
		dbURL = "postgresql://dev:dev@localhost:5432/dev"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("❌ Connection failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("❌ Ping failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Database connection successful\n")

	// Check schema_migrations table
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		fmt.Printf("❌ Failed to ensure schema_migrations table: %v\n", err)
		os.Exit(1)
	}

	// List applied migrations
	fmt.Println("📊 Applied migrations in database:")
	rows, err := pool.Query(ctx, `
		SELECT filename, applied_at
		FROM schema_migrations
		ORDER BY applied_at DESC
		LIMIT 10
	`)
	if err != nil {
		fmt.Printf("❌ Failed to query migrations: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var filename string
		var appliedAt string
		if err := rows.Scan(&filename, &appliedAt); err != nil {
			fmt.Printf("❌ Scan error: %v\n", err)
			continue
		}
		fmt.Printf("  ✅ %s (applied: %s)\n", filename, appliedAt)
		found = true
	}

	if !found {
		fmt.Println("  (no migrations applied yet)")
	}

	fmt.Println()

	// Check for 000043
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations
		WHERE filename LIKE '%000043%'
	`).Scan(&count)
	if err != nil {
		fmt.Printf("❌ Query error: %v\n", err)
		os.Exit(1)
	}

	if count > 0 {
		fmt.Println("✅ Migration 000043 is already applied!")

		// Check if new columns exist
		fmt.Println("\n📊 Checking new columns...")
		rows, err := pool.Query(ctx, `
			SELECT column_name
			FROM information_schema.columns
			WHERE table_name = 'messages'
			AND column_name IN ('search_vector_en', 'search_text_normalized', 'search_rank_base')
		`)
		if err != nil {
			fmt.Printf("⚠️  Could not verify columns: %v\n", err)
		} else {
			defer rows.Close()
			columnCount := 0
			for rows.Next() {
				var colName string
				rows.Scan(&colName)
				columnCount++
				fmt.Printf("  ✅ Column: %s\n", colName)
			}
			if columnCount == 3 {
				fmt.Println("\n🎉 All columns created successfully!")
			}
		}
	} else {
		fmt.Println("⚠️  Migration 000043 not yet applied")
		fmt.Println("\nTo apply it, run:")
		fmt.Println("  APP_AUTO_MIGRATE=1 go run ./cmd/api/main.go")
	}
}

func offerApplyMigrations() {
	fmt.Println("🚀 To apply migrations:")
	fmt.Println()
	fmt.Println("Option 1: Automatic (using API server)")
	fmt.Println("  $ APP_AUTO_MIGRATE=1 go run ./cmd/api/main.go")
	fmt.Println()
	fmt.Println("Option 2: Manual verification")
	fmt.Println("  $ go run ./_tools/migrate.go verify")
	fmt.Println()
	fmt.Println("Option 3: Direct SQL (if you have psql)")
	fmt.Println("  $ psql $DATABASE_URL -f migrations/000043_search_quality_improvements.up.sql")
	fmt.Println()
	fmt.Println("Requirements:")
	fmt.Println("  - PostgreSQL database running")
	fmt.Println("  - Environment variable: APP_DATABASE_URL or DATABASE_URL")
	fmt.Println("  - Extensions: unaccent, pg_trgm (created by migration)")
	fmt.Println()
}
