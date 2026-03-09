package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env                    string
	Addr                   string
	LogLevel               string
	DBDSN                  string
	RedisAddr              string
	RedisDB                int
	JWTSecret              string
	AccessTTL              time.Duration
	RefreshTTL             time.Duration
	AutoMigrate            bool
	MigrationsDir          string
	AllowedOrigin          string
	UseKafkaSend           bool
	UseCassandraReads      bool
	EnableWSSend           bool
	KafkaBrokers           string
	KafkaIngressTopic      string
	KafkaPersistedTopic    string
	KafkaDeliveryTopic     string
	KafkaSMSDispatchTopic  string
	KafkaPresenceTopic     string
	KafkaMicroserviceTopic string
	KafkaClientID          string
	AckTimeout             time.Duration
	CassandraHosts         string
	CassandraKeyspace      string
	CassandraUsername      string
	CassandraPassword      string
	CassandraConsistency   string
	SupportedProfiles      string
	ClaimAndroidCarrier    bool
	DiscoveryPepper        string
	// Optional headers for API deprecation and sunset information.
	// When non-empty these values will be emitted as `Deprecation` and
	// `Sunset` response headers by the API versioning middleware.
	APIDeprecation string
	APISunset      string
	// Optional PEM-encoded RSA public key for verifying mini-app manifests.
	MiniappPublicKeyPEM string
}

func Load() Config {
	return Config{
		Env:                    get("APP_ENV", "dev"),
		Addr:                   get("APP_ADDR", ":8080"),
		LogLevel:               get("APP_LOG_LEVEL", "info"),
		DBDSN:                  get("APP_DB_DSN", "postgres://ohmf:ohmf@localhost:5432/ohmf?sslmode=disable"),
		RedisAddr:              get("APP_REDIS_ADDR", "localhost:6379"),
		RedisDB:                getInt("APP_REDIS_DB", 0),
		JWTSecret:              get("APP_JWT_SECRET", "dev-only-change-me"),
		AccessTTL:              time.Duration(getInt("APP_ACCESS_TTL_MINUTES", 15)) * time.Minute,
		RefreshTTL:             time.Duration(getInt("APP_REFRESH_TTL_HOURS", 24*30)) * time.Hour,
		AutoMigrate:            getBool("APP_AUTO_MIGRATE", true),
		MigrationsDir:          get("APP_MIGRATIONS_DIR", "migrations"),
		AllowedOrigin:          get("APP_ALLOWED_ORIGIN", "*"),
		UseKafkaSend:           getBool("APP_USE_KAFKA_SEND", false),
		UseCassandraReads:      getBool("APP_USE_CASSANDRA_READS", false),
		EnableWSSend:           getBool("APP_ENABLE_WS_SEND", false),
		KafkaBrokers:           get("APP_KAFKA_BROKERS", "localhost:9092"),
		KafkaIngressTopic:      get("APP_KAFKA_INGRESS_TOPIC", "msg.ingress.v1"),
		KafkaPersistedTopic:    get("APP_KAFKA_PERSISTED_TOPIC", "msg.persisted.v1"),
		KafkaDeliveryTopic:     get("APP_KAFKA_DELIVERY_TOPIC", "msg.delivery.v1"),
		KafkaSMSDispatchTopic:  get("APP_KAFKA_SMS_DISPATCH_TOPIC", "msg.sms.dispatch.v1"),
		KafkaPresenceTopic:     get("APP_KAFKA_PRESENCE_TOPIC", "presence.events.v1"),
		KafkaMicroserviceTopic: get("APP_KAFKA_MICROSERVICE_TOPIC", "microservice.events.v1"),
		KafkaClientID:          get("APP_KAFKA_CLIENT_ID", "ohmf-gateway"),
		AckTimeout:             time.Duration(getInt("APP_ACK_TIMEOUT_MS", 2000)) * time.Millisecond,
		CassandraHosts:         get("APP_CASSANDRA_HOSTS", "localhost:9042"),
		CassandraKeyspace:      get("APP_CASSANDRA_KEYSPACE", "ohmf_messages"),
		CassandraUsername:      get("APP_CASSANDRA_USERNAME", ""),
		CassandraPassword:      get("APP_CASSANDRA_PASSWORD", ""),
		CassandraConsistency:   get("APP_CASSANDRA_CONSISTENCY", "quorum"),
		SupportedProfiles:      get("APP_SUPPORTED_PROFILES", "CORE_OTT"),
		ClaimAndroidCarrier:    getBool("APP_CLAIM_ANDROID_CARRIER", false),
		DiscoveryPepper:        get("APP_DISCOVERY_PEPPER", ""),
		APIDeprecation:         get("APP_API_DEPRECATION", ""),
		APISunset:              get("APP_API_SUNSET", ""),
		MiniappPublicKeyPEM:   get("APP_MINIAPP_PUBLIC_KEY_PEM", ""),
	}
}

func get(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getInt(k string, d int) int {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return d
	}
	return n
}

func getBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return d
	}
	return b
}
