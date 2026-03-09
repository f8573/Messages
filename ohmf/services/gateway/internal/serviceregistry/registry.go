package serviceregistry

// Lightweight runtime registry that records which high-level services are
// available in the current process. This helps map the spec's High-Level
// Architecture into a discoverable runtime artifact.

type Registry struct {
    available map[string]bool
}

func New(m map[string]bool) *Registry {
    if m == nil {
        m = map[string]bool{}
    }
    return &Registry{available: m}
}

func (r *Registry) Available() map[string]bool {
    // return a shallow copy to avoid external mutation
    out := make(map[string]bool, len(r.available))
    for k, v := range r.available {
        out[k] = v
    }
    return out
}

func (r *Registry) Set(name string, present bool) {
    r.available[name] = present
}
