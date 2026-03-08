package main

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	cors "github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/auth"
	"ohmf/services/gateway/internal/bus"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/conversations"
	"ohmf/services/gateway/internal/db"
	"ohmf/services/gateway/internal/events"
	"ohmf/services/gateway/internal/limit"
	"ohmf/services/gateway/internal/messages"
	appmw "ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/observability"
	"ohmf/services/gateway/internal/realtime"
	"ohmf/services/gateway/internal/token"
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

	if cfg.AutoMigrate {
		if err := db.ApplyMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
			logger.Fatal().Err(err).Msg("migration failed")
		}
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   cfg.RedisDB,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Fatal().Err(err).Msg("redis connection failed")
	}
	defer rdb.Close()

	var kafkaProducer *bus.KafkaProducer
	var asyncPipeline *messages.AsyncPipeline
	if cfg.UseKafkaSend {
		kafkaProducer = bus.NewKafkaProducer(cfg.KafkaBrokers, cfg.KafkaClientID, cfg.KafkaIngressTopic)
		asyncPipeline = messages.NewAsyncPipeline(kafkaProducer, rdb)
		defer kafkaProducer.Close()
	}

	var cassandraStore *messages.CassandraStore
	if cfg.UseCassandraReads {
		store, err := messages.NewCassandraStore(messages.CassandraConfig{
			Hosts:       cfg.CassandraHosts,
			Keyspace:    cfg.CassandraKeyspace,
			Username:    cfg.CassandraUsername,
			Password:    cfg.CassandraPassword,
			Consistency: cfg.CassandraConsistency,
		})
		if err != nil {
			logger.Fatal().Err(err).Msg("cassandra connection failed")
		}
		cassandraStore = store
		defer cassandraStore.Close()
	}

	rateLimiter := limit.NewTokenBucket(rdb)

	tokens := token.NewService(cfg.JWTSecret)
	authSvc := auth.NewService(pool, rdb, tokens, cfg.AccessTTL, cfg.RefreshTTL)
	convSvc := conversations.NewService(pool)
	msgSvc := messages.NewService(pool, messages.Options{
		UseKafkaSend:      cfg.UseKafkaSend,
		UseCassandraReads: cfg.UseCassandraReads,
		AckTimeout:        cfg.AckTimeout,
		Async:             asyncPipeline,
		Cassandra:         cassandraStore,
		RateLimiter:       rateLimiter,
	})
	eventsHandler := events.NewHandler(pool)
	wsHandler := realtime.NewHandler(tokens, msgSvc, rdb, rateLimiter, cfg.EnableWSSend)

	authHandler := auth.NewHandler(authSvc)
	convHandler := conversations.NewHandler(convSvc)
	msgHandler := messages.NewHandler(msgSvc)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// CORS middleware: allow local web dev server to call this API.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{
			"http://localhost:5173",
			"http://localhost:5174",
			"http://127.0.0.1:5173",
			"http://127.0.0.1:5174",
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Set Content-Security-Policy as a response header. frame-ancestors
	// must be delivered as a header (not a meta tag) to be enforced.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self' http://localhost:5174")
			next.ServeHTTP(w, req)
		})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/v1", func(v1 chi.Router) {
		v1.Post("/auth/phone/start", authHandler.StartPhone)
		v1.Post("/auth/phone/verify", authHandler.VerifyPhone)
		v1.Post("/auth/refresh", authHandler.Refresh)
		v1.Get("/ws", wsHandler.ServeHTTP)

		v1.Group(func(protected chi.Router) {
			protected.Use(appmw.RequireAuth(tokens))
			protected.Post("/auth/logout", authHandler.Logout)
			protected.Post("/conversations", convHandler.Create)
			protected.Post("/conversations/phone", convHandler.CreatePhone)
			protected.Get("/conversations", convHandler.List)
			protected.Get("/conversations/{id}", convHandler.Get)
			protected.Post("/messages", msgHandler.Send)
			protected.Post("/messages/phone", msgHandler.SendToPhone)
			protected.Get("/conversations/{id}/messages", msgHandler.List)
			protected.Post("/conversations/{id}/read", msgHandler.MarkRead)
			protected.Get("/events/stream", eventsHandler.Stream)
		})
	})

	logger.Info().Str("addr", cfg.Addr).Msg("starting api")
	if err := http.ListenAndServe(cfg.Addr, r); err != nil {
		logger.Fatal().Err(err).Msg("server stopped")
	}
}
