package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/otp"
	"ohmf/services/gateway/internal/phone"
	"ohmf/services/gateway/internal/token"
)

type StartRequest struct {
	PhoneE164 string `json:"phone_e164"`
	Channel   string `json:"channel"`
}

type VerifyRequest struct {
	ChallengeID string `json:"challenge_id"`
	OTPCode     string `json:"otp_code"`
	Device      struct {
		Platform   string `json:"platform"`
		DeviceName string `json:"device_name"`
		PushToken  string `json:"push_token"`
		PublicKey  string `json:"public_key"`
	} `json:"device"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	DeviceID string `json:"device_id"`
}

var (
	ErrChallengeNotFound = errors.New("challenge_not_found")
	ErrChallengeExpired  = errors.New("challenge_expired")
	ErrInvalidOTP        = errors.New("invalid_otp")
	ErrInvalidRefresh    = errors.New("invalid_refresh")
	ErrRateLimited       = errors.New("rate_limited")
)

type Service struct {
	db         *pgxpool.Pool
	redis      *redis.Client
	tokens     *token.Service
	accessTTL  time.Duration
	refreshTTL time.Duration
	cfg        config.Config
}

func NewService(db *pgxpool.Pool, redis *redis.Client, tokens *token.Service, accessTTL, refreshTTL time.Duration, cfg config.Config) *Service {
	return &Service{db: db, redis: redis, tokens: tokens, accessTTL: accessTTL, refreshTTL: refreshTTL, cfg: cfg}
}

func (s *Service) StartPhoneVerification(ctx context.Context, req StartRequest, ip string) (map[string]any, error) {
	phoneE164 := phone.NormalizeE164(req.PhoneE164)
	if phoneE164 == "" {
		return nil, fmt.Errorf("phone_required")
	}
	allowed, err := s.allowRate(ctx, "otp:start:"+phoneE164, 5, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrRateLimited
	}
	if ip != "" {
		allowedIP, err := s.allowRate(ctx, "otp:start:ip:"+ip, 20, 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if !allowedIP {
			return nil, ErrRateLimited
		}
	}

	// rate limit by subnet (simple IPv4 /16 heuristic)
	if ip != "" {
		parts := strings.Split(ip, ".")
		if len(parts) >= 2 {
			subnet := parts[0] + "." + parts[1]
			allowedSubnet, err := s.allowRate(ctx, "otp:start:subnet:"+subnet, 100, 10*time.Minute)
			if err != nil {
				return nil, err
			}
			if !allowedSubnet {
				return nil, ErrRateLimited
			}
		}
	}

	id := uuid.New()
	code := "123456"
	_, err = s.db.Exec(ctx, `
		INSERT INTO phone_verification_challenges (id, phone_e164, otp_code_hash, channel, attempts_remaining, expires_at)
		VALUES ($1, $2, $3, $4, 5, now() + interval '5 minute')
	`, id, phoneE164, otp.Hash(code), req.Channel)
	if err != nil {
		return nil, err
	}
	// If the phone or IP has elevated activity, signal escalation to client
	escalated := false
	if n, _ := s.redis.Get(ctx, "otp:start:"+phoneE164).Int64(); n > 3 {
		escalated = true
	}
	if ip != "" {
		if n, _ := s.redis.Get(ctx, "otp:start:ip:"+ip).Int64(); n > 10 {
			escalated = true
		}
	}

	return map[string]any{
		"challenge_id":    id.String(),
		"expires_in_sec":  300,
		"retry_after_sec": 30,
		"otp_strategy":    "SMS",
		"escalated":       escalated,
	}, nil
}

func (s *Service) VerifyPhone(ctx context.Context, req VerifyRequest, ip string) (map[string]any, error) {
	allowed, err := s.allowRate(ctx, "otp:verify:"+req.ChallengeID, 10, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrRateLimited
	}
	if ip != "" {
		allowedIP, err := s.allowRate(ctx, "otp:verify:ip:"+ip, 50, 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if !allowedIP {
			return nil, ErrRateLimited
		}
	}

	// device-based rate limiting (use public key or push token if present)
	deviceFingerprint := ""
	if req.Device.PublicKey != "" {
		deviceFingerprint = fmt.Sprintf("pk:%x", sha256.Sum256([]byte(req.Device.PublicKey)))
	} else if req.Device.PushToken != "" {
		deviceFingerprint = "pt:" + req.Device.PushToken
	}
	if deviceFingerprint != "" {
		allowedDevice, err := s.allowRate(ctx, "otp:verify:device:"+deviceFingerprint, 10, 10*time.Minute)
		if err != nil {
			return nil, err
		}
		if !allowedDevice {
			return nil, ErrRateLimited
		}
	}

	challengeID, err := uuid.Parse(req.ChallengeID)
	if err != nil {
		return nil, ErrChallengeNotFound
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var phoneE164, otpHash string
	var attemptsRemaining int
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT phone_e164, otp_code_hash, attempts_remaining, expires_at
		FROM phone_verification_challenges
		WHERE id = $1 AND consumed_at IS NULL
		FOR UPDATE
	`, challengeID).Scan(&phoneE164, &otpHash, &attemptsRemaining, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChallengeNotFound
		}
		return nil, err
	}
	if time.Now().After(expiresAt) {
		return nil, ErrChallengeExpired
	}
	if attemptsRemaining <= 0 || otp.Hash(req.OTPCode) != otpHash {
		_, _ = tx.Exec(ctx, `UPDATE phone_verification_challenges SET attempts_remaining = GREATEST(attempts_remaining - 1, 0) WHERE id = $1`, challengeID)
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrInvalidOTP
	}

	_, err = tx.Exec(ctx, `UPDATE phone_verification_challenges SET consumed_at = now() WHERE id = $1`, challengeID)
	if err != nil {
		return nil, err
	}

	var userID string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (primary_phone_e164, phone_verified_at)
		VALUES ($1, now())
		ON CONFLICT (primary_phone_e164)
		DO UPDATE SET phone_verified_at = EXCLUDED.phone_verified_at, updated_at = now()
		RETURNING id::text
	`, phoneE164).Scan(&userID)
	if err != nil {
		return nil, err
	}

	// Promote phone-based conversations into first-class user conversations so
	// newly verified users immediately receive existing messages.
	if _, err := tx.Exec(ctx, `
		WITH matched AS (
			SELECT DISTINCT cem.conversation_id
			FROM conversation_external_members cem
			JOIN external_contacts ec ON ec.id = cem.external_contact_id
			WHERE ec.phone_e164 = $1
		)
		INSERT INTO conversation_members (conversation_id, user_id, role)
		SELECT m.conversation_id, $2::uuid, 'MEMBER'
		FROM matched m
		ON CONFLICT (conversation_id, user_id) DO NOTHING
	`, phoneE164, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		WITH matched AS (
			SELECT DISTINCT cem.conversation_id
			FROM conversation_external_members cem
			JOIN external_contacts ec ON ec.id = cem.external_contact_id
			WHERE ec.phone_e164 = $1
		)
		UPDATE conversations c
		SET type = 'DM', transport_policy = 'AUTO', updated_at = now()
		WHERE c.id IN (SELECT conversation_id FROM matched)
	`, phoneE164); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM conversation_external_members cem
		USING external_contacts ec
		WHERE cem.external_contact_id = ec.id
		  AND ec.phone_e164 = $1
	`, phoneE164); err != nil {
		return nil, err
	}

	var deviceID string
	err = tx.QueryRow(ctx, `
		INSERT INTO devices (user_id, platform, device_name, push_token, public_key, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, now())
		RETURNING id::text
	`, userID, req.Device.Platform, req.Device.DeviceName, nullable(req.Device.PushToken), nullable(req.Device.PublicKey)).Scan(&deviceID)
	if err != nil {
		return nil, err
	}

	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, device_id, token_hash, expires_at)
		VALUES ($1, $2, $3, now() + ($4 || ' seconds')::interval)
	`, userID, deviceID, hashToken(refresh), strconv.Itoa(int(s.refreshTTL.Seconds())))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// determine feature profiles to include in the access token
	profiles := s.decideProfilesForPlatform(req.Device.Platform)
	access, err := s.tokens.IssueAccess(userID, s.accessTTL, profiles)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"user": map[string]any{
			"user_id":            userID,
			"primary_phone_e164": phoneE164,
		},
		"device": map[string]any{
			"device_id": deviceID,
			"platform":  req.Device.Platform,
		},
		"tokens": map[string]any{
			"access_token":  access,
			"refresh_token": refresh,
		},
	}, nil
}

func (s *Service) Refresh(ctx context.Context, req RefreshRequest) (map[string]any, error) {
	h := hashToken(req.RefreshToken)
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var tokenID, userID, deviceID string
	err = tx.QueryRow(ctx, `
		SELECT id::text, user_id::text, COALESCE(device_id::text, '')
		FROM refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
		FOR UPDATE
	`, h).Scan(&tokenID, &userID, &deviceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidRefresh
		}
		return nil, err
	}
	_, err = tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1`, tokenID)
	if err != nil {
		return nil, err
	}
	newRefresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO refresh_tokens (user_id, device_id, token_hash, expires_at)
		VALUES ($1, NULLIF($2, '')::uuid, $3, now() + ($4 || ' seconds')::interval)
	`, userID, deviceID, hashToken(newRefresh), strconv.Itoa(int(s.refreshTTL.Seconds())))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// determine profiles based on device platform if available
	var profiles []string
	if deviceID != "" {
		var platform string
		if err := tx.QueryRow(ctx, `SELECT platform FROM devices WHERE id = $1`, deviceID).Scan(&platform); err == nil {
			profiles = s.decideProfilesForPlatform(platform)
		}
	}
	access, err := s.tokens.IssueAccess(userID, s.accessTTL, profiles)
	if err != nil {
		return nil, err
	}
	return map[string]any{"access_token": access, "refresh_token": newRefresh}, nil
}

func (s *Service) decideProfilesForPlatform(platform string) []string {
	profiles := []string{"CORE_OTT"}
	switch strings.ToUpper(platform) {
	case "ANDROID":
		profiles = append(profiles, "MINIAPP_RUNTIME")
		if s.cfg.ClaimAndroidCarrier {
			profiles = append(profiles, "ANDROID_CARRIER")
		}
	case "WEB":
		profiles = append(profiles, "WEB_RELAY")
	}
	return profiles
}

func (s *Service) Logout(ctx context.Context, userID string, req LogoutRequest) error {
	if req.DeviceID != "" {
		_, err := s.db.Exec(ctx, `
			UPDATE refresh_tokens
			SET revoked_at = now()
			WHERE user_id = $1 AND device_id = $2::uuid AND revoked_at IS NULL
		`, userID, req.DeviceID)
		return err
	}
	_, err := s.db.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked_at = now()
		WHERE user_id = $1 AND revoked_at IS NULL
	`, userID)
	return err
}

func (s *Service) allowRate(ctx context.Context, key string, limit int64, window time.Duration) (bool, error) {
	n, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if err := s.redis.Expire(ctx, key, window).Err(); err != nil {
			return false, err
		}
	}
	return n <= limit, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
