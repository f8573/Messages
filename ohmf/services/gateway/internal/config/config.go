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
	// Addresses for lightweight dev service proxies
	ContactsAddr            string
	AppsAddr                string
	MediaAddr               string
	EnableE2EEOTT           bool
	EnableGroups            bool
	EnableMediaPipeline     bool
	EnableWebPush           bool
	RequireRelayAttestation bool
	UseRealOTPProvider      bool
	OTPProvider             string
	OTPFrom                 string
	TwilioAccountSID        string
	TwilioAuthToken         string
	TwilioMessagingService  string
	MediaRootDir            string
	MediaPublicBaseURL      string
	WebPushVAPIDPublicKey   string
	WebPushVAPIDPrivateKey  string
	WebPushSubject          string
	PushSubscriptionKey     string
}

func Load() Config {
	return Config{
		Env:                     get("APP_ENV", "dev"),
		Addr:                    get("APP_ADDR", ":8081"),
		LogLevel:                get("APP_LOG_LEVEL", "info"),
		DBDSN:                   get("APP_DB_DSN", "postgres://ohmf:ohmf@localhost:5432/ohmf?sslmode=disable"),
		RedisAddr:               get("APP_REDIS_ADDR", "localhost:6379"),
		RedisDB:                 getInt("APP_REDIS_DB", 0),
		JWTSecret:               get("APP_JWT_SECRET", "dev-only-change-me"),
		AccessTTL:               time.Duration(getInt("APP_ACCESS_TTL_MINUTES", 15)) * time.Minute,
		RefreshTTL:              time.Duration(getInt("APP_REFRESH_TTL_HOURS", 24*30)) * time.Hour,
		AutoMigrate:             getBool("APP_AUTO_MIGRATE", true),
		MigrationsDir:           get("APP_MIGRATIONS_DIR", "migrations"),
		AllowedOrigin:           get("APP_ALLOWED_ORIGIN", "*"),
		UseKafkaSend:            getBool("APP_USE_KAFKA_SEND", false),
		UseCassandraReads:       getBool("APP_USE_CASSANDRA_READS", false),
		EnableWSSend:            getBool("APP_ENABLE_WS_SEND", false),
		KafkaBrokers:            get("APP_KAFKA_BROKERS", "localhost:9092"),
		KafkaIngressTopic:       get("APP_KAFKA_INGRESS_TOPIC", "msg.ingress.v1"),
		KafkaPersistedTopic:     get("APP_KAFKA_PERSISTED_TOPIC", "msg.persisted.v1"),
		KafkaDeliveryTopic:      get("APP_KAFKA_DELIVERY_TOPIC", "msg.delivery.v1"),
		KafkaSMSDispatchTopic:   get("APP_KAFKA_SMS_DISPATCH_TOPIC", "msg.sms.dispatch.v1"),
		KafkaPresenceTopic:      get("APP_KAFKA_PRESENCE_TOPIC", "presence.events.v1"),
		KafkaMicroserviceTopic:  get("APP_KAFKA_MICROSERVICE_TOPIC", "microservice.events.v1"),
		KafkaClientID:           get("APP_KAFKA_CLIENT_ID", "ohmf-gateway"),
		AckTimeout:              time.Duration(getInt("APP_ACK_TIMEOUT_MS", 2000)) * time.Millisecond,
		CassandraHosts:          get("APP_CASSANDRA_HOSTS", "localhost:9042"),
		CassandraKeyspace:       get("APP_CASSANDRA_KEYSPACE", "ohmf_messages"),
		CassandraUsername:       get("APP_CASSANDRA_USERNAME", ""),
		CassandraPassword:       get("APP_CASSANDRA_PASSWORD", ""),
		CassandraConsistency:    get("APP_CASSANDRA_CONSISTENCY", "quorum"),
		SupportedProfiles:       get("APP_SUPPORTED_PROFILES", "CORE_OTT"),
		ClaimAndroidCarrier:     getBool("APP_CLAIM_ANDROID_CARRIER", false),
		DiscoveryPepper:         get("APP_DISCOVERY_PEPPER", ""),
		APIDeprecation:          get("APP_API_DEPRECATION", ""),
		APISunset:               get("APP_API_SUNSET", ""),
		MiniappPublicKeyPEM:     get("APP_MINIAPP_PUBLIC_KEY_PEM", ""),
		ContactsAddr:            get("APP_CONTACTS_ADDR", "http://localhost:18085"),
		AppsAddr:                get("APP_APPS_ADDR", "http://localhost:18086"),
		MediaAddr:               get("APP_MEDIA_ADDR", "http://localhost:18087"),
		EnableE2EEOTT:           getBool("APP_ENABLE_E2EE_OTT_V1", true),
		EnableGroups:            getBool("APP_ENABLE_GROUPS_V1", true),
		EnableMediaPipeline:     getBool("APP_ENABLE_MEDIA_PIPELINE_V1", false),
		EnableWebPush:           getBool("APP_ENABLE_WEB_PUSH_V1", false),
		RequireRelayAttestation: getBool("APP_REQUIRE_RELAY_ATTESTATION", false),
		UseRealOTPProvider:      getBool("APP_USE_REAL_OTP_PROVIDER", false),
		OTPProvider:             get("APP_OTP_PROVIDER", "dev"),
		OTPFrom:                 get("APP_OTP_FROM", ""),
		TwilioAccountSID:        get("APP_TWILIO_ACCOUNT_SID", ""),
		TwilioAuthToken:         get("APP_TWILIO_AUTH_TOKEN", ""),
		TwilioMessagingService:  get("APP_TWILIO_MESSAGING_SERVICE_SID", ""),
		MediaRootDir:            get("APP_MEDIA_ROOT_DIR", "var/media"),
		MediaPublicBaseURL:      get("APP_MEDIA_PUBLIC_BASE_URL", "http://localhost:18080"),
		WebPushVAPIDPublicKey:   get("APP_WEB_PUSH_VAPID_PUBLIC_KEY", ""),
		WebPushVAPIDPrivateKey:  get("APP_WEB_PUSH_VAPID_PRIVATE_KEY", ""),
		WebPushSubject:          get("APP_WEB_PUSH_SUBJECT", "mailto:devnull@localhost"),
		PushSubscriptionKey:     get("APP_PUSH_SUBSCRIPTION_KEY", ""),
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
