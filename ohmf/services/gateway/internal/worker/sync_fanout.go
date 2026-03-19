package worker

import (
	"context"
	"time"

	"ohmf/services/gateway/internal/replication"
)

type SyncFanoutWorker struct {
	store *replication.Store
	stop  chan struct{}
}

func NewSyncFanoutWorker(store *replication.Store) *SyncFanoutWorker {
	return &SyncFanoutWorker{store: store, stop: make(chan struct{})}
}

func (w *SyncFanoutWorker) Name() string { return "sync_fanout" }

func (w *SyncFanoutWorker) Start(ctx context.Context) error {
	if w.store == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.stop:
			return nil
		default:
		}
		processed, err := w.store.ProcessBatch(ctx, 100)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-w.stop:
				return nil
			case <-time.After(750 * time.Millisecond):
			}
			continue
		}
		if processed == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-w.stop:
				return nil
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

func (w *SyncFanoutWorker) Stop(ctx context.Context) error {
	close(w.stop)
	return nil
}
