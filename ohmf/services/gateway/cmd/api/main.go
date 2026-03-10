package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"encoding/json"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	cors "github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"
	openapipkg "ohmf/services/gateway/internal/openapi"
	"ohmf/services/gateway/internal/auth"
	"ohmf/services/gateway/internal/bus"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/conversations"
	"ohmf/services/gateway/internal/db"
	"ohmf/services/gateway/internal/devices"
	"ohmf/services/gateway/internal/events"
	"ohmf/services/gateway/internal/limit"
	"ohmf/services/gateway/internal/messages"
	appmw "ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/observability"
	"ohmf/services/gateway/internal/realtime"
	"ohmf/services/gateway/internal/token"
	"ohmf/services/gateway/internal/users"
	"ohmf/services/gateway/internal/discovery"
	"ohmf/services/gateway/internal/media"
	"ohmf/services/gateway/internal/presence"
	"ohmf/services/gateway/internal/miniapp"
	"ohmf/services/gateway/internal/notification"
	"ohmf/services/gateway/internal/abuse"
	"ohmf/services/gateway/internal/relay"
	"ohmf/services/gateway/internal/carrier"
	"ohmf/services/gateway/internal/serviceregistry"
    "ohmf/services/gateway/internal/sync"
)

func main() {
	cfg := config.Load()
	logger := observability.NewLogger(cfg.LogLevel)
	ctx := context.Background()

	// Smoke mode: run a minimal server that exposes /healthz without initializing
	// external dependencies (DB, Redis, Kafka). Enable with APP_SMOKE_MODE=1.
	if os.Getenv("APP_SMOKE_MODE") == "1" {
		r := chi.NewRouter()
		r.Use(chimiddleware.RequestID)
		r.Use(chimiddleware.RealIP)
		r.Use(chimiddleware.Recoverer)
		r.Use(chimiddleware.Timeout(30 * time.Second))
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		// lightweight services view for smoke mode
		r.Get("/v1/_services", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{
				"gateway":       true,
				"auth":          false,
				"users":         false,
				"conversations": false,
				"messages":      false,
				"relay":         false,
				"presence":      false,
				"notification":  false,
				"media":         false,
				"miniapp":       false,
				"abuse":         false,
			})
		})
		logger.Info().Str("addr", cfg.Addr).Msg("starting api (smoke mode)")
		if err := http.ListenAndServe(cfg.Addr, r); err != nil {
			logger.Fatal().Err(err).Msg("server stopped")
		}
		return
	}

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
	authSvc := auth.NewService(pool, rdb, tokens, cfg.AccessTTL, cfg.RefreshTTL, cfg)
	usersSvc := users.NewService(pool)
	discoverySvc := discovery.NewService(pool, cfg.DiscoveryPepper)
	convSvc := conversations.NewService(pool)
	devSvc := devices.NewService(pool)
	mediaSvc := media.NewService(pool)
	carrierSvc := carrier.NewService(pool)
	presenceSvc := presence.NewService(rdb)
	notificationSvc := notification.NewService(pool)
	miniappSvc := miniapp.NewService(pool, cfg)
	abuseSvc := abuse.NewService(pool)
	relaySvc := relay.NewService(pool)
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
	usersHandler := users.NewHandler(usersSvc)
	discoveryHandler := discovery.NewHandler(discoverySvc)
	convHandler := conversations.NewHandler(convSvc)
	devHandler := devices.NewHandler(devSvc)
	mediaHandler := media.NewHandler(mediaSvc)
	carrierHandler := carrier.NewHandler(carrierSvc, pool)
	presenceHandler := presence.NewHandler(presenceSvc)
	notificationHandler := notification.NewHandler(notificationSvc)
	miniappHandler := miniapp.NewHandler(miniappSvc)
	abuseHandler := abuse.NewHandler(abuseSvc)
	relayHandler := relay.NewHandler(relaySvc)
	msgHandler := messages.NewHandler(msgSvc)
	syncSvc := sync.NewService(pool)
	syncHandler := sync.NewHandler(syncSvc)

	// Build a lightweight runtime view of which high-level services are present
	reg := serviceregistry.New(map[string]bool{
		"gateway":       true,
		"auth":          authSvc != nil,
		"users":         usersSvc != nil,
		"conversations": convSvc != nil,
		"messages":      msgSvc != nil,
		"presence":      presenceSvc != nil,
		"notification":  notificationSvc != nil,
		"miniapp":       miniappSvc != nil,
		"abuse":         abuseSvc != nil,
		"relay":         relaySvc != nil,
		"media":         mediaSvc != nil,
	})

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))

	// API versioning middleware: advertise spec & API versions
	r.Use(appmw.APIVersionMiddleware(cfg))

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

	// Serve the OpenAPI spec at the repository-level path.
	r.Get("/openapi.yaml", openapipkg.NewHandler())

	r.Route("/v1", func(v1 chi.Router) {
		// Expose a small runtime discovery endpoint mapping to the spec's
		// High-Level Architecture. This is intentionally simple and safe for
		// dev/ops use.
		v1.Get("/_services", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reg.Available())
		})

		v1.Post("/auth/phone/start", authHandler.StartPhone)
		v1.Post("/auth/phone/verify", authHandler.VerifyPhone)
		v1.Post("/auth/refresh", authHandler.Refresh)
		v1.Get("/ws", wsHandler.ServeHTTP)

		v1.Group(func(protected chi.Router) {
			protected.Use(appmw.RequireAuth(tokens))
			protected.Post("/auth/logout", authHandler.Logout)
			protected.Post("/account/export", usersHandler.ExportAccount)
			protected.Post("/account/delete", usersHandler.DeleteAccount)
			protected.Post("/discovery", discoveryHandler.Discover)
			// Alternate path per spec: /contacts/discover
			protected.Post("/contacts/discover", discoveryHandler.Discover)
			protected.Post("/conversations", convHandler.Create)
			protected.Post("/devices", devHandler.Register)
			protected.Get("/devices", devHandler.List)
			protected.Delete("/devices/{id}", devHandler.Revoke)
			protected.Get("/presence/users/{id}", presenceHandler.GetUser)
			protected.Get("/presence/conversations/{id}", presenceHandler.GetConversation)
			protected.Post("/media/attachments", mediaHandler.Register)
			protected.Delete("/media/attachments/{id}", mediaHandler.Purge)
			protected.Post("/carrier/messages/import", carrierHandler.Import)
			protected.Get("/carrier/messages", carrierHandler.List)
			protected.Post("/carrier/messages/{id}/link", carrierHandler.Link)
			protected.Get("/carrier/messages/{id}/links", carrierHandler.ListLinks)
			protected.Get("/admin/carrier_message_links", carrierHandler.AdminListLinks)
			protected.Post("/media/uploads", mediaHandler.CreateUploadToken)
			protected.Post("/media/uploads/{token}/complete", mediaHandler.CompleteUpload)
			protected.Post("/miniapps/manifests", miniappHandler.RegisterManifest)
			protected.Post("/miniapps/sessions", miniappHandler.CreateSession)
			protected.Get("/miniapps/sessions/{id}", miniappHandler.GetSession)
			protected.Delete("/miniapps/sessions/{id}", miniappHandler.EndSession)
			// Spec aliases: /apps/... map to miniapp handlers
			protected.Post("/apps/sessions", miniappHandler.CreateSession)
			protected.Get("/apps/sessions/{id}", miniappHandler.GetSession)
			protected.Post("/apps/register", miniappHandler.RegisterManifest)
			protected.Post("/apps/sessions/{id}/events", miniappHandler.AppendEvent)
			protected.Post("/apps/sessions/{id}/snapshot", miniappHandler.Snapshot)
			protected.Get("/apps", miniappHandler.ListApps)
        
			protected.Post("/notifications/send", notificationHandler.Send)
			protected.Post("/relay/messages", relayHandler.CreateMessage)
			protected.Get("/relay/jobs/{id}", relayHandler.GetJob)
			protected.Get("/relay/jobs/available", relayHandler.ListAvailable)
			protected.Post("/relay/jobs/{id}/accept", relayHandler.Accept)
			protected.Post("/relay/jobs/{id}/result", relayHandler.Result)
			protected.Post("/abuse/events", abuseHandler.Record)
			protected.Get("/abuse/score/{id}", abuseHandler.GetScore)
			protected.Get("/abuse/destination", abuseHandler.GetDestinationScore)
			protected.Post("/abuse/otp/check", abuseHandler.CheckOTP)
			protected.Post("/blocks/{id}", usersHandler.BlockUser)
			protected.Delete("/blocks/{id}", usersHandler.UnblockUser)
			protected.Get("/blocks", usersHandler.ListBlocked)
			protected.Post("/conversations/phone", convHandler.CreatePhone)
			protected.Get("/conversations", convHandler.List)
			protected.Get("/conversations/{id}", convHandler.Get)
			protected.Patch("/conversations/{id}", convHandler.UpdatePolicy)
			protected.Post("/conversations/{id}/thread_keys", convHandler.SetThreadKeys)
			protected.With(appmw.ValidateJSONMiddleware("message-ingress")).Post("/messages", msgHandler.Send)
			protected.With(appmw.ValidateJSONMiddleware("message-ingress")).Post("/messages/phone", msgHandler.SendToPhone)
			protected.Post("/messages/{id}/reactions", msgHandler.AddReaction)
			protected.Delete("/messages/{id}/reactions", msgHandler.RemoveReaction)
			protected.Post("/messages/{id}/deliveries", msgHandler.RecordDelivery)
			protected.Get("/conversations/{id}/messages", msgHandler.List)
			protected.Get("/conversations/{id}/timeline", msgHandler.Timeline)
			protected.Delete("/messages/{id}", msgHandler.Delete)
			protected.Get("/messages/{id}/deliveries", msgHandler.ListDeliveries)
			protected.Post("/conversations/{id}/read", msgHandler.MarkRead)
			protected.Post("/messages/{id}/redact", msgHandler.Redact)
			protected.Get("/events/stream", eventsHandler.Stream)
			protected.Get("/sync", syncHandler.Incremental)
		})
	})

	logger.Info().Str("addr", cfg.Addr).Msg("starting api")
	if err := http.ListenAndServe(cfg.Addr, r); err != nil {
		logger.Fatal().Err(err).Msg("server stopped")
	}
}
