package worker

import (
    "context"
    "sync"
)

type Worker interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

type Runner struct {
    mu      sync.Mutex
    workers map[string]Worker
}

func NewRunner() *Runner {
    return &Runner{workers: make(map[string]Worker)}
}

func (r *Runner) Add(w Worker) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.workers[w.Name()] = w
}

func (r *Runner) StartAll(ctx context.Context) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    var wg sync.WaitGroup
    for _, w := range r.workers {
        wg.Add(1)
        go func(w Worker) {
            defer wg.Done()
            _ = w.Start(ctx)
        }(w)
    }
    // return after starting; workers run until context cancelled or Stop called
    go func() { wg.Wait() }()
    return nil
}

func (r *Runner) StopAll(ctx context.Context) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    for _, w := range r.workers {
        _ = w.Stop(ctx)
    }
    return nil
}
