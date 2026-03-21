package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"ohmf/services/gateway/internal/middleware"
)

const (
	discoveryAlgorithmV1 = "SHA256_PEPPERED_V1"
	discoveryAlgorithmV2 = "SHA256_PEPPERED_V2"
	discoveryBodyLimit   = 1 << 20
	defaultContactLimit  = 256
	defaultRateWindow    = time.Minute
	defaultUserLimit     = 10
	defaultIPLimit       = 30
)

type Contact struct {
	Hash  string `json:"hash"`
	Label string `json:"label,omitempty"`
}

type Match struct {
	Hash        string `json:"hash"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type requestBody struct {
	Algorithm string    `json:"algorithm"`
	Contacts  []Contact `json:"contacts"`
}

type responseBody struct {
	Matches []Match `json:"matches"`
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

type rateBucket struct {
	count int
	reset time.Time
}

type Handler struct {
	db      queryer
	pepper  string
	limiter *rateLimiter
}

func NewHandler(db queryer, pepper string) *Handler {
	return &Handler{db: db, pepper: pepper, limiter: &rateLimiter{buckets: make(map[string]*rateBucket)}}
}

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ip := normalizeRemoteAddr(r.RemoteAddr)
	if !h.allowRequest("user:"+userID, discoveryUserLimit(), discoveryRateWindow()) {
		http.Error(w, "rate_limited", http.StatusTooManyRequests)
		return
	}
	if ip != "" && !h.allowRequest("ip:"+ip, discoveryIPLimit(), discoveryRateWindow()) {
		http.Error(w, "rate_limited", http.StatusTooManyRequests)
		return
	}

	bodyReader := http.MaxBytesReader(w, r.Body, discoveryBodyLimit)
	defer bodyReader.Close()

	var body requestBody
	if err := json.NewDecoder(bodyReader).Decode(&body); err != nil {
		http.Error(w, "invalid_json", http.StatusBadRequest)
		return
	}

	body.Algorithm = strings.ToUpper(strings.TrimSpace(body.Algorithm))
	if !supportedAlgorithm(body.Algorithm) {
		http.Error(w, "unsupported_algorithm", http.StatusBadRequest)
		return
	}

	if len(body.Contacts) > discoveryContactLimit() {
		http.Error(w, "too_many_contacts", http.StatusBadRequest)
		return
	}

	contacts, err := normalizeContacts(body.Contacts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(contacts) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responseBody{Matches: nil})
		return
	}

	matches, err := h.search(r.Context(), body.Algorithm, contacts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(responseBody{Matches: matches})
}

func (h *Handler) search(ctx context.Context, algorithm string, contacts []Contact) ([]Match, error) {
	in := make(map[string]struct{}, len(contacts))
	for _, c := range contacts {
		in[c.Hash] = struct{}{}
	}

	rows, err := h.db.Query(ctx, `
        SELECT ec.phone_e164, COALESCE(u.id::text, ''), COALESCE(u.display_name, '')
        FROM external_contacts ec
        LEFT JOIN users u ON u.primary_phone_e164 = ec.phone_e164
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var phone, uid, name string
		if err := rows.Scan(&phone, &uid, &name); err != nil {
			return nil, err
		}
		hash := h.hashPhone(algorithm, phone)
		if _, ok := in[hash]; ok {
			out = append(out, Match{Hash: hash, UserID: uid, DisplayName: name})
		}
	}
	return out, nil
}

func (h *Handler) hashPhone(algorithm, phone string) string {
	var sum [32]byte
	switch algorithm {
	case discoveryAlgorithmV2:
		sum = sha256.Sum256([]byte(discoveryAlgorithmV2 + ":" + h.pepper + ":" + phone))
	default:
		sum = sha256.Sum256(append([]byte(h.pepper), []byte(phone)...))
	}
	return hex.EncodeToString(sum[:])
}

func (h *Handler) allowRequest(key string, limit int, window time.Duration) bool {
	if h == nil || h.limiter == nil || limit <= 0 || window <= 0 {
		return true
	}
	return h.limiter.allow(key, limit, window)
}

func (l *rateLimiter) allow(key string, limit int, window time.Duration) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok || now.After(bucket.reset) {
		l.buckets[key] = &rateBucket{count: 1, reset: now.Add(window)}
		return true
	}

	if bucket.count >= limit {
		return false
	}

	bucket.count++
	return true
}

func normalizeContacts(contacts []Contact) ([]Contact, error) {
	seen := make(map[string]struct{}, len(contacts))
	out := make([]Contact, 0, len(contacts))
	for _, contact := range contacts {
		hash, err := canonicalizeHash(contact.Hash)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, Contact{
			Hash:  hash,
			Label: strings.TrimSpace(contact.Label),
		})
	}
	return out, nil
}

func canonicalizeHash(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if len(raw) != sha256.Size*2 {
		return "", errors.New("invalid_contact_hash")
	}
	if _, err := hex.DecodeString(raw); err != nil {
		return "", errors.New("invalid_contact_hash")
	}
	return raw, nil
}

func supportedAlgorithm(algorithm string) bool {
	switch algorithm {
	case discoveryAlgorithmV1, discoveryAlgorithmV2:
		return true
	default:
		return false
	}
}

func normalizeRemoteAddr(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		remote = host
	}
	remote = strings.Trim(remote, "[]")
	if ip := net.ParseIP(remote); ip != nil {
		return ip.String()
	}
	return remote
}

func discoveryContactLimit() int {
	if v := envInt("APP_DISCOVERY_MAX_CONTACTS", defaultContactLimit); v > 0 {
		return v
	}
	return defaultContactLimit
}

func discoveryUserLimit() int {
	if v := envInt("APP_DISCOVERY_RATE_PER_USER", defaultUserLimit); v > 0 {
		return v
	}
	return defaultUserLimit
}

func discoveryIPLimit() int {
	if v := envInt("APP_DISCOVERY_RATE_PER_IP", defaultIPLimit); v > 0 {
		return v
	}
	return defaultIPLimit
}

func discoveryRateWindow() time.Duration {
	if v := envInt("APP_DISCOVERY_RATE_WINDOW_MINUTES", 1); v > 0 {
		return time.Duration(v) * time.Minute
	}
	return defaultRateWindow
}

func envInt(name string, def int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
