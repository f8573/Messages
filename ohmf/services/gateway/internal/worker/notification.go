package worker

import (
	"context"
	"time"

	"ohmf/services/gateway/internal/notification"
)

type NotificationWorker struct {
	svc  *notification.Handler
	stop chan struct{}
}

func NewNotificationWorker(svc *notification.Handler) *NotificationWorker {
	return &NotificationWorker{svc: svc, stop: make(chan struct{})}
}

func (n *NotificationWorker) Name() string { return "notification" }

func (n *NotificationWorker) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-n.stop:
			return nil
		default:
		}
		if n.svc != nil && n.svc.HasUsablePushProviders() {
			_ = n.svc.DispatchPending(ctx, 25)
		}
		time.Sleep(1 * time.Second)
	}
}

func (n *NotificationWorker) Stop(ctx context.Context) error {
	close(n.stop)
	return nil
}
