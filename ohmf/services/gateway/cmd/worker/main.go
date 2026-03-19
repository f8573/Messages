package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/db"
	"ohmf/services/gateway/internal/devices"
	"ohmf/services/gateway/internal/notification"
	"ohmf/services/gateway/internal/observability"
	"ohmf/services/gateway/internal/replication"
	wk "ohmf/services/gateway/internal/worker"

	"github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()
	logger := observability.NewLogger(cfg.LogLevel)
	ctx := context.Background()

	pool, err := db.NewPool(ctx, cfg.DBDSN)
	if err != nil {
		logger.Fatal().Err(err).Msg("db connection failed")
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("redis connection failed")
	}
	defer rdb.Close()

	replicationStore := replication.NewStore(pool, rdb)
	deviceSvc := devices.NewService(pool, cfg)
	notificationSvc := notification.NewService(pool, deviceSvc, cfg)

	// create runner and workers
	runner := wk.NewRunner()
	runner.Add(wk.NewMediaWorker(pool))
	runner.Add(wk.NewNotificationWorker(notificationSvc))
	runner.Add(wk.NewAbuseAggregatorWorker(pool))
	runner.Add(wk.NewRelayRetryWorker(pool))
	runner.Add(wk.NewSyncFanoutWorker(replicationStore))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := runner.StartAll(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to start workers")
		return
	}

	// wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	logger.Info().Msg("shutting down workers")
	// allow short grace
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	_ = runner.StopAll(stopCtx)
}
