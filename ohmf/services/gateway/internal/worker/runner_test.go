package worker

import (
    "context"
    "sync/atomic"
    "testing"
    "time"
)

type fakeWorker struct{
    started int32
    stopped int32
}
func (f *fakeWorker) Name() string { return "fake" }
func (f *fakeWorker) Start(ctx context.Context) error {
    atomic.StoreInt32(&f.started, 1)
    <-ctx.Done()
    return nil
}
func (f *fakeWorker) Stop(ctx context.Context) error {
    atomic.StoreInt32(&f.stopped, 1)
    return nil
}

func TestRunnerStartStop(t *testing.T) {
    r := NewRunner()
    f := &fakeWorker{}
    r.Add(f)
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    if err := r.StartAll(ctx); err != nil {
        t.Fatalf("startall failed: %v", err)
    }
    // give worker a moment to start
    time.Sleep(10 * time.Millisecond)
    if atomic.LoadInt32(&f.started) == 0 {
        t.Fatalf("worker did not start")
    }
    // stop
    _ = r.StopAll(context.Background())
    // allow stop to propagate
    time.Sleep(10 * time.Millisecond)
    if atomic.LoadInt32(&f.stopped) == 0 {
        t.Fatalf("worker did not stop")
    }
}
