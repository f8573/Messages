package main

import (
    "encoding/json"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "strings"
)

// This tool checks that the repository contains the key components called out in
// OHMF spec section 1 (Purpose and Scope). It is intentionally lightweight: it
// verifies presence of directories and key files that indicate implementation
// of the in-scope features.

var requiredPaths = []string{
    "services/auth",
    "services/users",
    "services/conversations",
    "services/messages",
    "services/realtime",
    "services/media",
    "services/relay",
    "packages/miniapp",
    "apps/android",
    "apps/web",
}

type Result struct {
    CheckedPaths []string          `json:"checked_paths"`
    Missing      []string          `json:"missing"`
    Present      []string          `json:"present"`
    Details      map[string]string `json:"details,omitempty"`
}

func exists(path string) bool {
    info, err := os.Stat(path)
    if err != nil {
        return false
    }
    return info.IsDir()
}

func main() {
    root, err := os.Getwd()
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(2)
    }

    res := Result{
        CheckedPaths: make([]string, 0, len(requiredPaths)),
        Missing:      []string{},
        Present:      []string{},
        Details:      map[string]string{},
    }

    for _, p := range requiredPaths {
        res.CheckedPaths = append(res.CheckedPaths, p)
        full := filepath.Join(root, p)
        if exists(full) {
            res.Present = append(res.Present, p)
            // gather a small hint (README presence)
            hint := "dir"
            readme := filepath.Join(full, "README.md")
            if _, err := os.Stat(readme); err == nil {
                hint = "readme"
            } else {
                // look for any .go file as another signal
                hasGo := false
                _ = filepath.WalkDir(full, func(path string, d fs.DirEntry, err error) error {
                    if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".go") {
                        hasGo = true
                        return filepath.SkipDir
                    }
                    return nil
                })
                if hasGo {
                    hint = "go-files"
                }
            }
            res.Details[p] = hint
        } else {
            res.Missing = append(res.Missing, p)
            res.Details[p] = "missing"
        }
    }

    // write report
    outPath := filepath.Join(root, "build", "spec_section_1_report.json")
    _ = os.MkdirAll(filepath.Dir(outPath), 0755)
    f, err := os.Create(outPath)
    if err == nil {
        enc := json.NewEncoder(f)
        enc.SetIndent("", "  ")
        _ = enc.Encode(res)
        _ = f.Close()
    }

    // print concise summary
    if len(res.Missing) == 0 {
        fmt.Println("OK: all required section-1 components present")
        os.Exit(0)
    }

    fmt.Fprintf(os.Stderr, "MISSING %d required items:\n", len(res.Missing))
    for _, m := range res.Missing {
        fmt.Fprintf(os.Stderr, " - %s\n", m)
    }
    fmt.Fprintf(os.Stderr, "Report written to %s\n", outPath)
    os.Exit(1)
}
