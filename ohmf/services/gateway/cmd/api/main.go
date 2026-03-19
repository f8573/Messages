package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	cors "github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/abuse"
	"ohmf/services/gateway/internal/auth"
	"ohmf/services/gateway/internal/bus"
	"ohmf/services/gateway/internal/carrier"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/conversations"
	"ohmf/services/gateway/internal/db"
	"ohmf/services/gateway/internal/devicekeys"
	"ohmf/services/gateway/internal/devices"
	"ohmf/services/gateway/internal/discovery"
	"ohmf/services/gateway/internal/events"
	"ohmf/services/gateway/internal/limit"
	"ohmf/services/gateway/internal/media"
	"ohmf/services/gateway/internal/messages"
	appmw "ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/miniapp"
	"ohmf/services/gateway/internal/notification"
	"ohmf/services/gateway/internal/observability"
	openapipkg "ohmf/services/gateway/internal/openapi"
	"ohmf/services/gateway/internal/otp"
	// removed: presence import - package deleted
	"ohmf/services/gateway/internal/realtime"
	"ohmf/services/gateway/internal/relay"
	"ohmf/services/gateway/internal/replication"
	"ohmf/services/gateway/internal/serviceregistry"
	"ohmf/services/gateway/internal/sync"
	"ohmf/services/gateway/internal/token"
	"ohmf/services/gateway/internal/users"
	wk "ohmf/services/gateway/internal/worker"
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
		r.Use(observability.HTTPMetricsMiddleware)

		// Lightweight reverse-proxy handlers for local dev and integration tests.
		// These proxy /v1/contacts, /v1/apps, /v1/media to configured backend addresses.
		makeProxy := func(target string) http.Handler {
			u, err := url.Parse(target)
			if err != nil {
				// fallback to a handler that returns 502
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "bad backend url", http.StatusBadGateway)
				})
			}
			proxy := httputil.NewSingleHostReverseProxy(u)
			// Ensure request uses target host/scheme but keep original path
			origDirector := proxy.Director
			proxy.Director = func(req *http.Request) {
				origDirector(req)
				req.URL.Scheme = u.Scheme
				req.URL.Host = u.Host
				// leave req.URL.Path unchanged so backends see /v1/.. paths
			}
			return proxy
		}

		// Mount both exact and wildcard paths for proxies so chi routing matches.
		r.Handle("/v1/contacts", makeProxy(cfg.ContactsAddr))
		r.Handle("/v1/contacts/*", makeProxy(cfg.ContactsAddr))
		r.Handle("/v1/apps", makeProxy(cfg.AppsAddr))
		r.Handle("/v1/apps/*", makeProxy(cfg.AppsAddr))
		r.Handle("/v1/media", makeProxy(cfg.MediaAddr))
		r.Handle("/v1/media/*", makeProxy(cfg.MediaAddr))
		r.Handle("/metrics", observability.MetricsHandler())
		r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
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
	replicationStore := replication.NewStore(pool, rdb)

	tokens := token.NewService(cfg.JWTSecret)
	otpProvider, err := otp.NewProvider(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("otp provider init failed")
	}
	authSvc := auth.NewService(pool, rdb, tokens, otpProvider, cfg.AccessTTL, cfg.RefreshTTL, cfg)
	usersSvc := users.NewService(pool, replicationStore)
	discoverySvc := discovery.NewService(pool, cfg.DiscoveryPepper)
	convSvc := conversations.NewService(pool, replicationStore)
	devSvc := devices.NewService(pool, cfg)
	mediaSvc := media.NewService(pool, cfg)
	carrierSvc := carrier.NewService(&pgxAdapter{p: pool})
	// removed: presence service - unused routes
	notificationSvc := notification.NewService(pool, devSvc, cfg)
	abuseSvc := abuse.NewService(pool)
	relayHandler := relay.NewHandlerWithOptions(&pgxAdapter{p: pool}, relay.Options{RequireAttestation: cfg.RequireRelayAttestation})
	miniappSvc := miniapp.NewService(pool, cfg, rdb, replicationStore)
	deviceKeysSvc := devicekeys.NewService(pool)
	msgSvc := messages.NewService(pool, messages.Options{
		UseKafkaSend:      cfg.UseKafkaSend,
		UseCassandraReads: cfg.UseCassandraReads,
		AckTimeout:        cfg.AckTimeout,
		Async:             asyncPipeline,
		Cassandra:         cassandraStore,
		RateLimiter:       rateLimiter,
		Redis:             rdb,
		Replication:       replicationStore,
	})
	eventsHandler := events.NewHandler(pool, rdb, msgSvc)
	wsHandler := realtime.NewHandler(tokens, msgSvc, rdb, rateLimiter, cfg.EnableWSSend, replicationStore)

	authHandler := auth.NewHandler(authSvc)
	usersHandler := users.NewHandler(usersSvc)
	discoveryHandler := discovery.NewHandler(discoverySvc)
	convHandler := conversations.NewHandler(convSvc)
	// removed: trivial constructor wrappers inlined
	devHandler := &devices.Handler{svc: devSvc}
	mediaHandler := &media.Handler{svc: mediaSvc}
	carrierHandler := &carrier.Handler{db: &pgxAdapter{p: pool}}
	// removed: presence handler - service deleted
	notificationHandler := notification.NewHandler(notificationSvc)
	miniappHandler := &miniapp.Handler{svc: miniappSvc}
	abuseHandler := &abuse.Handler{svc: abuseSvc}
	deviceKeysHandler := &devicekeys.Handler{db: deviceKeysSvc.DB()}
	msgHandler := messages.NewHandler(msgSvc)
	syncSvc := sync.NewService(pool, replicationStore)
	syncHandler := &sync.Handler{svc: syncSvc}
	syncFanoutWorker := wk.NewSyncFanoutWorker(replicationStore)
	go func() {
		if err := syncFanoutWorker.Start(ctx); err != nil {
			logger.Error().Err(err).Msg("sync fanout worker stopped")
		}
	}()

	// Build a lightweight runtime view of which high-level services are present
	reg := serviceregistry.New(map[string]bool{
		"gateway":       true,
		"auth":          authSvc != nil,
		"users":         usersSvc != nil,
		"conversations": convSvc != nil,
		"messages":      msgSvc != nil,
		"notification":  notificationSvc != nil,
		"miniapp":       miniappSvc != nil,
		"abuse":         abuseSvc != nil,
		"relay":         true, // removed: relay service unified with handler
		"media":         mediaSvc != nil,
	})

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(func(next http.Handler) http.Handler {
		timeout := chimiddleware.Timeout(30 * time.Second)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			switch req.URL.Path {
			case "/v1/ws", "/v1/events/stream", "/v2/ws":
				next.ServeHTTP(w, req)
				return
			}
			timeout(next).ServeHTTP(w, req)
		})
	})
	r.Use(observability.HTTPMetricsMiddleware)

	// API versioning middleware: advertise spec & API versions
	r.Use(appmw.APIVersionMiddleware(cfg))

	// CORS middleware: allow local web dev server to call this API.
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(_ *http.Request, origin string) bool {
			if origin == "" {
				return false
			}
			if cfg.AllowedOrigin != "*" && origin == cfg.AllowedOrigin {
				return true
			}
			return strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:")
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Set Content-Security-Policy as a response header. frame-ancestors
	// must be delivered as a header (not a meta tag) to be enforced.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'self' http://localhost:* http://127.0.0.1:*")
			next.ServeHTTP(w, req)
		})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Handle("/metrics", observability.MetricsHandler())

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
		v1.Put("/media/uploads/{token}", mediaHandler.UploadObject)
		v1.Get("/media/downloads/{token}", mediaHandler.DownloadObject)

		v1.Group(func(protected chi.Router) {
			protected.Use(appmw.RequireAuth(tokens))
			protected.Post("/auth/logout", authHandler.Logout)
			protected.Post("/account/export", usersHandler.ExportAccount)
			protected.Post("/account/delete", usersHandler.DeleteAccount)
			protected.Get("/me", usersHandler.GetMe)
			protected.Patch("/me", usersHandler.UpdateMe)
			protected.Post("/users/resolve", usersHandler.ResolveProfiles)
			protected.Post("/discovery", discoveryHandler.Discover)
			// Alternate path per spec: /contacts/discover
			protected.Post("/contacts/discover", discoveryHandler.Discover)
			protected.Post("/conversations", convHandler.Create)
			protected.Post("/devices", devHandler.Register)
			protected.Get("/devices", devHandler.List)
			protected.Patch("/devices/{id}", devHandler.Update)
			protected.Delete("/devices/{id}", devHandler.Revoke)
			protected.Put("/device-keys/{deviceID}", deviceKeysHandler.Publish)
			protected.Post("/device-keys/{deviceID}/prekeys", deviceKeysHandler.AddPrekeys)
			protected.Get("/device-keys/{userID}", deviceKeysHandler.ListForUser)
			protected.Post("/device-keys/{userID}/claim", deviceKeysHandler.ClaimForUser)
			// removed: presence routes - service deleted
			protected.Post("/media/attachments", mediaHandler.Register)
			protected.Get("/media/attachments/{id}/download", mediaHandler.CreateDownload)
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
			protected.Post("/miniapps/sessions/{id}/join", miniappHandler.JoinSession)
			protected.Post("/miniapps/sessions/{id}/events", miniappHandler.AppendEvent)
			protected.Post("/miniapps/sessions/{id}/snapshot", miniappHandler.Snapshot)
			protected.Post("/miniapps/shares", miniappHandler.Share)
			// Spec aliases: /apps/... map to miniapp handlers
			protected.Post("/apps/sessions", miniappHandler.CreateSession)
			protected.Get("/apps/sessions/{id}", miniappHandler.GetSession)
			protected.Delete("/apps/sessions/{id}", miniappHandler.EndSession)
			protected.Post("/apps/sessions/{id}/join", miniappHandler.JoinSession)
			protected.Post("/apps/register", miniappHandler.RegisterManifest)
			protected.Post("/apps/sessions/{id}/events", miniappHandler.AppendEvent)
			protected.Post("/apps/sessions/{id}/snapshot", miniappHandler.Snapshot)
			protected.Post("/apps/shares", miniappHandler.Share)
			protected.Get("/apps", miniappHandler.ListApps)
			protected.Get("/apps/{appID}", miniappHandler.GetApp)

			protected.Post("/notifications/send", notificationHandler.Send)
			protected.Get("/notifications/preferences", notificationHandler.GetPreferences)
			protected.Put("/notifications/preferences", notificationHandler.UpdatePreferences)
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
			protected.Patch("/conversations/{id}/metadata", convHandler.UpdateMetadata)
			protected.Patch("/conversations/{id}/preferences", convHandler.UpdatePreferences)
			protected.Post("/conversations/{id}/members", convHandler.AddMembers)
			protected.Delete("/conversations/{id}/members/{userID}", convHandler.RemoveMember)
			protected.Post("/conversations/{id}/thread_keys", convHandler.SetThreadKeys)
			protected.With(appmw.ValidateJSONMiddleware("send-message-request")).Post("/messages", msgHandler.Send)
			protected.With(appmw.ValidateJSONMiddleware("send-phone-message-request")).Post("/messages/phone", msgHandler.SendToPhone)
			protected.Post("/messages/{id}/reactions", msgHandler.AddReaction)
			protected.Delete("/messages/{id}/reactions", msgHandler.RemoveReaction)
			protected.Post("/messages/{id}/deliveries", msgHandler.RecordDelivery)
			protected.Get("/conversations/{id}/messages", msgHandler.List)
			protected.Get("/conversations/{id}/timeline", msgHandler.Timeline)
			protected.Patch("/messages/{id}", msgHandler.Edit)
			protected.Delete("/messages/{id}", msgHandler.Delete)
			protected.Get("/messages/{id}/deliveries", msgHandler.ListDeliveries)
			protected.Post("/conversations/{id}/read", msgHandler.MarkRead)
			protected.Post("/messages/{id}/redact", msgHandler.Redact)
			protected.Get("/events/stream", eventsHandler.Stream)
			protected.Get("/sync", syncHandler.Incremental)
		})
	})

	r.Route("/v2", func(v2 chi.Router) {
		v2.Get("/ws", wsHandler.ServeV2)
		v2.Group(func(protected chi.Router) {
			protected.Use(appmw.RequireAuth(tokens))
			protected.Get("/conversations", convHandler.ListProjected)
			protected.Get("/sync", syncHandler.IncrementalV2)
			protected.Post("/conversations/{id}/read", msgHandler.MarkRead)
			protected.Post("/conversations/{id}/delivered", syncHandler.MarkDeliveredV2)
		})
	})

	logger.Info().Str("addr", cfg.Addr).Msg("starting api")
	if err := http.ListenAndServe(cfg.Addr, r); err != nil {
		logger.Fatal().Err(err).Msg("server stopped")
	}
}

// pgxAdapter adapts a *pgxpool.Pool to the package-local `carrier.DB` interface.
type pgxAdapter struct{ p *pgxpool.Pool }

func (a *pgxAdapter) QueryRow(ctx context.Context, sql string, args ...any) carrier.RowScanner {
	return a.p.QueryRow(ctx, sql, args...)
}

func (a *pgxAdapter) Query(ctx context.Context, sql string, args ...any) (carrier.Rows, error) {
	return a.p.Query(ctx, sql, args...)
}

func (a *pgxAdapter) Exec(ctx context.Context, sql string, args ...any) (any, error) {
	return a.p.Exec(ctx, sql, args...)
}
