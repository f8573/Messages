package main

import (
    "context"
    "fmt"
    "os"

    "github.com/getkin/kin-openapi/openapi3"
)

func main() {
    loader := openapi3.NewLoader()
    loader.IsExternalRefsAllowed = true
    path := "ohmf/packages/protocol/openapi/openapi.yaml"
    doc, err := loader.LoadFromFile(path)
    if err != nil {
        fmt.Fprintf(os.Stderr, "failed to load OpenAPI file %s: %v\n", path, err)
        os.Exit(2)
    }
    if err := doc.Validate(context.Background()); err != nil {
        fmt.Fprintf(os.Stderr, "openapi validation failed: %v\n", err)
        os.Exit(3)
    }
    fmt.Println("OpenAPI validation: OK")
}
