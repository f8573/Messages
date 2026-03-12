package testutil

import (
    "fmt"
    "os"
    "path/filepath"
)

// ReadMigration searches parent directories for the gateway migrations folder
// and returns the contents of the named migration file.
func ReadMigration(name string) ([]byte, error) {
    wd, err := os.Getwd()
    if err != nil {
        return nil, err
    }

    for {
        candidate := filepath.Join(wd, "ohmf", "services", "gateway", "migrations", name)
        if _, err := os.Stat(candidate); err == nil {
            return os.ReadFile(candidate)
        }
        parent := filepath.Dir(wd)
        if parent == wd {
            break
        }
        wd = parent
    }

    return nil, fmt.Errorf("migration %s not found", name)
}
