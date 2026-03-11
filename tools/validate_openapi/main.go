package main

import (
    "context"
    "flag"
    "fmt"
    "os"

    "github.com/getkin/kin-openapi/openapi3"
)

func main() {
    var path string

    flag.StringVar(&path, "spec", "", "path to OpenAPI spec (yaml/json); can also be provided via OPENAPI_SPEC")
    flag.Parse()

    if path == "" && flag.NArg() > 0 {
        path = flag.Arg(0)
    }
    if path == "" {
        path = os.Getenv("OPENAPI_SPEC")
    }
    if path == "" {
        path = "openapi.yaml"
    }

    loader := openapi3.NewLoader()
    loader.IsExternalRefsAllowed = true

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
