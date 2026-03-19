package miniapp

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/config"
	"ohmf/services/gateway/internal/replication"
)

var (
	ErrManifestRequired          = errors.New("manifest_required")
	ErrManifestInvalid           = errors.New("manifest_invalid")
	ErrManifestSignatureRequired = errors.New("manifest_signature_required")
	ErrManifestSignatureInvalid  = errors.New("manifest_signature_invalid")
	ErrManifestNotFound          = errors.New("manifest_not_found")
	ErrSessionNotFound           = errors.New("session_not_found")
	ErrSessionEnded              = errors.New("session_ended")
	ErrStateVersionConflict      = errors.New("state_version_conflict")
)

var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[A-Za-z0-9.-]+)?$`)

type Service struct {
	db          *pgxpool.Pool
	publicKey   any
	redis       *redis.Client
	replication *replication.Store
}

type SessionParticipant struct {
	UserID      string `json:"user_id"`
	Role        string `json:"role"`
	DisplayName string `json:"display_name,omitempty"`
}

type CreateSessionInput struct {
	ManifestID         string
	AppID              string
	ConversationID     string
	Viewer             SessionParticipant
	Participants       []SessionParticipant
	GrantedPermissions []string
	StateSnapshot      any
	TTL                time.Duration
	ResumeExisting     bool
}

type sessionState struct {
	Snapshot                  map[string]any   `json:"snapshot"`
	SessionStorage            map[string]any   `json:"session_storage"`
	SharedConversationStorage map[string]any   `json:"shared_conversation_storage"`
	ProjectedMessages         []map[string]any `json:"projected_messages"`
}

type sessionRecord struct {
	ID                     string
	ManifestID             string
	AppID                  string
	ConversationID         string
	Participants           []SessionParticipant
	GlobalPermissions      []string
	ParticipantPermissions map[string][]string
	State                  sessionState
	StateVersion           int
	CreatedBy              string
	ExpiresAt              *time.Time
	CreatedAt              *time.Time
	EndedAt                *time.Time
}

type manifestSignature struct {
	Alg string
	KID string
	Sig string
}

func NewService(db *pgxpool.Pool, cfg config.Config, redisClient *redis.Client, store *replication.Store) *Service {
	s := &Service{db: db, redis: redisClient, replication: store}
	if cfg.MiniappPublicKeyPEM != "" {
		block, _ := pem.Decode([]byte(cfg.MiniappPublicKeyPEM))
		if block != nil {
			if pk, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
				switch typed := pk.(type) {
				case *rsa.PublicKey:
					s.publicKey = typed
				case ed25519.PublicKey:
					s.publicKey = typed
				}
			}
		}
	}
	return s
}

// RegisterManifest stores a mini-app manifest JSON and returns its id.
func (s *Service) RegisterManifest(ctx context.Context, ownerID string, manifest any) (string, error) {
	if manifest == nil {
		return "", ErrManifestRequired
	}
	var mmap map[string]any
	b, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("%w: marshal failed", ErrManifestInvalid)
	}
	if err := json.Unmarshal(b, &mmap); err != nil {
		return "", fmt.Errorf("%w: object required", ErrManifestInvalid)
	}
	if err := validateManifest(mmap); err != nil {
		return "", err
	}
	if s.publicKey != nil {
		if err := s.verifyManifestSignature(mmap); err != nil {
			return "", fmt.Errorf("%w: %v", ErrManifestSignatureInvalid, err)
		}
	}
	id := uuid.New().String()
	_, err = s.db.Exec(ctx, `INSERT INTO miniapp_manifests (id, owner_user_id, manifest, created_at) VALUES ($1::uuid, $2::uuid, $3::jsonb, now())`, id, ownerID, string(b))
	if err != nil {
		return "", err
	}
	return id, nil
}

func validateManifest(mmap map[string]any) error {
	if len(mmap) == 0 {
		return ErrManifestRequired
	}
	if !nonEmptyStringField(mmap, "app_id") {
		return fmt.Errorf("%w: app_id required", ErrManifestInvalid)
	}
	if !nonEmptyStringField(mmap, "name") {
		return fmt.Errorf("%w: name required", ErrManifestInvalid)
	}
	if manifestVersion, ok := mmap["manifest_version"]; ok {
		if version, ok := manifestVersion.(string); !ok || version == "" {
			return fmt.Errorf("%w: manifest_version must be a string", ErrManifestInvalid)
		}
	}
	version, ok := mmap["version"].(string)
	if !ok || !semverPattern.MatchString(version) {
		return fmt.Errorf("%w: version must be semver", ErrManifestInvalid)
	}
	entrypoint, ok := mmap["entrypoint"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: entrypoint object required", ErrManifestInvalid)
	}
	entryType, ok := entrypoint["type"].(string)
	if !ok || (entryType != "url" && entryType != "inline" && entryType != "web_bundle") {
		return fmt.Errorf("%w: entrypoint.type invalid", ErrManifestInvalid)
	}
	if !nonEmptyStringField(entrypoint, "url") {
		return fmt.Errorf("%w: entrypoint.url required", ErrManifestInvalid)
	}
	messagePreview, ok := mmap["message_preview"].(map[string]any)
	if !ok {
		return fmt.Errorf("%w: message_preview object required", ErrManifestInvalid)
	}
	previewType := stringField(messagePreview, "type")
	if previewType != "static_image" && previewType != "live" {
		return fmt.Errorf("%w: message_preview.type invalid", ErrManifestInvalid)
	}
	if !nonEmptyStringField(messagePreview, "url") {
		return fmt.Errorf("%w: message_preview.url required", ErrManifestInvalid)
	}
	if fitMode := stringField(messagePreview, "fit_mode"); fitMode != "" && fitMode != "scale" && fitMode != "crop" {
		return fmt.Errorf("%w: message_preview.fit_mode invalid", ErrManifestInvalid)
	}
	if !stringSliceFieldPresent(mmap, "permissions") {
		return fmt.Errorf("%w: permissions array required", ErrManifestInvalid)
	}
	if _, ok := mmap["capabilities"].(map[string]any); !ok {
		return fmt.Errorf("%w: capabilities object required", ErrManifestInvalid)
	}
	if rawSignature, ok := mmap["signature"]; ok && rawSignature != nil {
		if _, err := manifestSignatureFromMap(mmap); err != nil {
			return err
		}
	}
	return nil
}

func manifestSignatureFromMap(mmap map[string]any) (manifestSignature, error) {
	rawSignature, ok := mmap["signature"]
	if !ok || rawSignature == nil {
		return manifestSignature{}, ErrManifestSignatureRequired
	}
	sigMap, ok := rawSignature.(map[string]any)
	if !ok {
		return manifestSignature{}, fmt.Errorf("%w: signature object required", ErrManifestInvalid)
	}
	sig := manifestSignature{
		Alg: stringField(sigMap, "alg"),
		KID: stringField(sigMap, "kid"),
		Sig: stringField(sigMap, "sig"),
	}
	if sig.Alg == "" || sig.KID == "" || sig.Sig == "" {
		return manifestSignature{}, fmt.Errorf("%w: signature.alg, signature.kid, and signature.sig are required", ErrManifestInvalid)
	}
	return sig, nil
}

func nonEmptyStringField(m map[string]any, key string) bool {
	return stringField(m, key) != ""
}

func stringField(m map[string]any, key string) string {
	value, ok := m[key].(string)
	if !ok {
		return ""
	}
	return value
}

func stringSliceFieldPresent(m map[string]any, key string) bool {
	switch m[key].(type) {
	case []any, []string:
		return true
	default:
		return false
	}
}

// verifyManifestSignature verifies a manifest map contains a signature over the
// manifest JSON with the `signature` field removed.
func (s *Service) verifyManifestSignature(mmap map[string]any) error {
	signature, err := manifestSignatureFromMap(mmap)
	if err != nil {
		return err
	}

	copyMap := make(map[string]any, len(mmap))
	for k, v := range mmap {
		if k == "signature" {
			continue
		}
		copyMap[k] = v
	}
	payload, err := json.Marshal(copyMap)
	if err != nil {
		return err
	}
	sigBytes, err := base64.StdEncoding.DecodeString(signature.Sig)
	if err != nil {
		return err
	}

	switch signature.Alg {
	case "RS256":
		rsaKey, ok := s.publicKey.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("configured public key does not support RS256")
		}
		h := sha256.Sum256(payload)
		return rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, h[:], sigBytes)
	case "Ed25519", "EdDSA":
		edKey, ok := s.publicKey.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("configured public key does not support Ed25519")
		}
		if !ed25519.Verify(edKey, payload, sigBytes) {
			return fmt.Errorf("signature verification failed")
		}
		return nil
	default:
		return fmt.Errorf("unsupported signature algorithm %q", signature.Alg)
	}
}

func (s *Service) GetManifestByAppID(ctx context.Context, appID string) (map[string]any, error) {
	if appID == "" {
		return nil, ErrManifestNotFound
	}
	row, err := s.loadManifestByAppID(ctx, appID)
	if err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) GetManifestByID(ctx context.Context, manifestID string) (map[string]any, error) {
	var id, owner string
	var manifestB []byte
	var createdAt time.Time
	err := s.db.QueryRow(ctx, `SELECT id::text, owner_user_id::text, manifest, created_at FROM miniapp_manifests WHERE id = $1::uuid`, manifestID).Scan(&id, &owner, &manifestB, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	var manifest any
	_ = json.Unmarshal(manifestB, &manifest)
	return map[string]any{
		"id":            id,
		"owner_user_id": owner,
		"manifest":      manifest,
		"created_at":    createdAt,
	}, nil
}

// CreateSession creates or resumes a runtime session for an app bound to a conversation.
func (s *Service) CreateSession(ctx context.Context, input CreateSessionInput) (map[string]any, bool, error) {
	if input.ConversationID == "" {
		return nil, false, fmt.Errorf("conversation_id required")
	}
	if input.TTL <= 0 {
		input.TTL = 30 * time.Minute
	}

	manifestID, appID, manifestPermissions, err := s.resolveManifest(ctx, input.ManifestID, input.AppID)
	if err != nil {
		return nil, false, err
	}

	viewer := normalizeParticipant(input.Viewer)
	participants := normalizeParticipants(input.Participants, viewer)
	grantedPermissions := sanitizeGrantedPermissions(input.GrantedPermissions, manifestPermissions)
	if len(grantedPermissions) == 0 {
		grantedPermissions = append([]string(nil), manifestPermissions...)
	}

	if input.ResumeExisting {
		existing, err := s.findActiveSession(ctx, appID, input.ConversationID)
		if err != nil && !errors.Is(err, ErrSessionNotFound) {
			return nil, false, err
		}
		if err == nil {
			if err := s.refreshSession(ctx, existing.ID, participants, grantedPermissions, input.TTL); err != nil {
				return nil, false, err
			}
			record, err := s.GetSession(ctx, existing.ID)
			return record, false, err
		}
	}

	id := uuid.New().String()
	partsB, _ := json.Marshal(participants)
	permsB, _ := json.Marshal(grantedPermissions)
	participantPermissions := map[string][]string{}
	if viewer.UserID != "" {
		participantPermissions[viewer.UserID] = append([]string(nil), grantedPermissions...)
	}
	participantPermsB, _ := json.Marshal(participantPermissions)
	stateB, _ := json.Marshal(defaultSessionState(input.StateSnapshot))
	expires := time.Now().Add(input.TTL)
	createdBy := nullUUIDArg(viewer.UserID)
	_, err = s.db.Exec(
		ctx,
		`INSERT INTO miniapp_sessions (id, manifest_id, app_id, conversation_id, participants, granted_permissions, participant_permissions, state, state_version, created_by, expires_at, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, 1, $9::uuid, $10, now())`,
		id,
		manifestID,
		appID,
		input.ConversationID,
		string(partsB),
		string(permsB),
		string(participantPermsB),
		string(stateB),
		createdBy,
		expires,
	)
	if err != nil {
		return nil, true, err
	}
	record, err := s.GetSession(ctx, id)
	return record, true, err
}

// GetSession returns the session record and computed launch context.
func (s *Service) GetSession(ctx context.Context, sessionID string) (map[string]any, error) {
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return s.sessionRecordToMap(record), nil
}

// EndSession marks a session as ended.
func (s *Service) EndSession(ctx context.Context, sessionID string) error {
	tag, err := s.db.Exec(ctx, `UPDATE miniapp_sessions SET ended_at = now() WHERE id = $1::uuid AND ended_at IS NULL`, sessionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		record, err := s.loadSessionRecord(ctx, sessionID)
		if err != nil {
			return err
		}
		if record.EndedAt != nil {
			return ErrSessionEnded
		}
		return ErrSessionEnded
	}
	return nil
}

// AppendEvent appends an event to a mini-app session's event log.
func (s *Service) AppendEvent(ctx context.Context, sessionID, actorID, eventName string, body any) (int64, error) {
	if eventName == "" {
		return 0, fmt.Errorf("event_name required")
	}
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if record.EndedAt != nil {
		return 0, ErrSessionEnded
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	var seq int64
	err = s.db.QueryRow(
		ctx,
		`INSERT INTO miniapp_events (app_session_id, actor_user_id, event_name, body, created_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, now())
		 RETURNING event_seq`,
		sessionID,
		nullUUIDArg(actorID),
		eventName,
		string(b),
	).Scan(&seq)
	if err != nil {
		return 0, err
	}
	_, _ = s.db.Exec(ctx, `UPDATE miniapp_sessions SET expires_at = now() + interval '1 hour' WHERE id = $1::uuid`, sessionID)
	return seq, nil
}

// SnapshotSession replaces persisted state and advances the session state version.
func (s *Service) SnapshotSession(ctx context.Context, sessionID string, state any, version int, _ []string) (int, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var currentVersion int
	var appID string
	var endedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT app_id, state_version, ended_at FROM miniapp_sessions WHERE id = $1::uuid FOR UPDATE`, sessionID).Scan(&appID, &currentVersion, &endedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrSessionNotFound
		}
		return 0, err
	}
	if endedAt != nil {
		return 0, ErrSessionEnded
	}

	nextVersion := version
	if nextVersion <= 0 {
		nextVersion = currentVersion + 1
	}
	if nextVersion <= currentVersion {
		return 0, ErrStateVersionConflict
	}

	stateEnvelope := stateEnvelopeFromAny(state)
	stateB, err := json.Marshal(stateEnvelope)
	if err != nil {
		return 0, err
	}

	query := `UPDATE miniapp_sessions SET state = $1::jsonb, state_version = $2, expires_at = now() + interval '1 hour' WHERE id = $3::uuid`
	args := []any{string(stateB), nextVersion, sessionID}
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return nextVersion, nil
}

// ListManifests returns the latest manifest for each app id.
func (s *Service) ListManifests(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.Query(
		ctx,
		`SELECT DISTINCT ON ((manifest->>'app_id')) id::text, owner_user_id::text, manifest, created_at
		   FROM miniapp_manifests
		  ORDER BY (manifest->>'app_id'), created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []map[string]any{}
	for rows.Next() {
		var id, owner string
		var manifestB []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &owner, &manifestB, &createdAt); err != nil {
			return nil, err
		}
		var manifest any
		_ = json.Unmarshal(manifestB, &manifest)
		out = append(out, map[string]any{
			"id":            id,
			"owner_user_id": owner,
			"manifest":      manifest,
			"created_at":    createdAt,
		})
	}
	return out, nil
}

func (s *Service) loadManifestByAppID(ctx context.Context, appID string) (map[string]any, error) {
	var id, owner string
	var manifestB []byte
	var createdAt time.Time
	err := s.db.QueryRow(
		ctx,
		`SELECT id::text, owner_user_id::text, manifest, created_at
		   FROM miniapp_manifests
		  WHERE manifest->>'app_id' = $1
		  ORDER BY created_at DESC
		  LIMIT 1`,
		appID,
	).Scan(&id, &owner, &manifestB, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrManifestNotFound
		}
		return nil, err
	}
	var manifest any
	_ = json.Unmarshal(manifestB, &manifest)
	return map[string]any{
		"id":            id,
		"owner_user_id": owner,
		"manifest":      manifest,
		"created_at":    createdAt,
	}, nil
}

func (s *Service) resolveManifest(ctx context.Context, manifestID, appID string) (string, string, []string, error) {
	switch {
	case manifestID != "":
		row, err := s.GetManifestByID(ctx, manifestID)
		if err != nil {
			return "", "", nil, err
		}
		manifestMap, _ := row["manifest"].(map[string]any)
		return row["id"].(string), stringField(manifestMap, "app_id"), permissionsFromManifest(manifestMap), nil
	case appID != "":
		row, err := s.loadManifestByAppID(ctx, appID)
		if err != nil {
			return "", "", nil, err
		}
		manifestMap, _ := row["manifest"].(map[string]any)
		return row["id"].(string), stringField(manifestMap, "app_id"), permissionsFromManifest(manifestMap), nil
	default:
		return "", "", nil, ErrManifestNotFound
	}
}

func (s *Service) manifestPermissionsForAppID(ctx context.Context, appID string) ([]string, error) {
	row, err := s.loadManifestByAppID(ctx, appID)
	if err != nil {
		return nil, err
	}
	manifestMap, _ := row["manifest"].(map[string]any)
	return permissionsFromManifest(manifestMap), nil
}

func permissionsFromManifest(manifest map[string]any) []string {
	rawPermissions, _ := manifest["permissions"].([]any)
	out := make([]string, 0, len(rawPermissions))
	for _, raw := range rawPermissions {
		if permission, ok := raw.(string); ok && permission != "" {
			out = append(out, permission)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func sanitizeGrantedPermissions(requested, allowed []string) []string {
	if len(allowed) == 0 {
		return nil
	}
	if len(requested) == 0 {
		return append([]string(nil), allowed...)
	}
	allowedSet := map[string]struct{}{}
	for _, permission := range allowed {
		allowedSet[permission] = struct{}{}
	}
	out := make([]string, 0, len(requested))
	for _, permission := range requested {
		if _, ok := allowedSet[permission]; ok {
			out = append(out, permission)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func (s *Service) findActiveSession(ctx context.Context, appID, conversationID string) (*sessionRecord, error) {
	var sessionID string
	err := s.db.QueryRow(
		ctx,
		`SELECT id::text
		   FROM miniapp_sessions
		  WHERE app_id = $1 AND conversation_id = $2::uuid AND ended_at IS NULL
		  ORDER BY created_at DESC
		  LIMIT 1`,
		appID,
		conversationID,
	).Scan(&sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	record, err := s.loadSessionRecord(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Service) refreshSession(ctx context.Context, sessionID string, participants []SessionParticipant, _ []string, ttl time.Duration) error {
	partsB, _ := json.Marshal(participants)
	_, err := s.db.Exec(
		ctx,
		`UPDATE miniapp_sessions
		    SET participants = $1::jsonb,
		        expires_at = $2
		  WHERE id = $3::uuid`,
		string(partsB),
		time.Now().Add(ttl),
		sessionID,
	)
	return err
}

func (s *Service) loadSessionRecord(ctx context.Context, sessionID string) (sessionRecord, error) {
	var record sessionRecord
	var participantsB, permissionsB, participantPermissionsB, stateB []byte
	var createdBy *string
	err := s.db.QueryRow(
		ctx,
		`SELECT id::text, manifest_id::text, app_id, conversation_id::text, participants, granted_permissions, participant_permissions, state, state_version, created_by::text, expires_at, created_at, ended_at
		   FROM miniapp_sessions
		  WHERE id = $1::uuid`,
		sessionID,
	).Scan(
		&record.ID,
		&record.ManifestID,
		&record.AppID,
		&record.ConversationID,
		&participantsB,
		&permissionsB,
		&participantPermissionsB,
		&stateB,
		&record.StateVersion,
		&createdBy,
		&record.ExpiresAt,
		&record.CreatedAt,
		&record.EndedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sessionRecord{}, ErrSessionNotFound
		}
		return sessionRecord{}, err
	}
	if createdBy != nil {
		record.CreatedBy = *createdBy
	}
	record.Participants = decodeParticipants(participantsB)
	record.GlobalPermissions = decodeStringList(permissionsB)
	record.ParticipantPermissions = decodeParticipantPermissions(participantPermissionsB)
	record.State = decodeSessionState(stateB)
	return record, nil
}

func (s *Service) sessionRecordToMap(record sessionRecord) map[string]any {
	viewer := pickViewer(record.Participants, record.CreatedBy)
	return map[string]any{
		"app_session_id":          record.ID,
		"manifest_id":             record.ManifestID,
		"app_id":                  record.AppID,
		"conversation_id":         record.ConversationID,
		"participants":            record.Participants,
		"capabilities_granted":    record.viewerGrantedPermissions(viewer.UserID),
		"participant_permissions": record.ParticipantPermissions,
		"state":                   record.State,
		"state_version":           record.StateVersion,
		"expires_at":              record.ExpiresAt,
		"created_at":              record.CreatedAt,
		"launch_context":          buildLaunchContext(record),
		"ended_at":                record.EndedAt,
	}
}

func buildLaunchContext(record sessionRecord) map[string]any {
	viewer := pickViewer(record.Participants, record.CreatedBy)
	return map[string]any{
		"app_id":               record.AppID,
		"app_session_id":       record.ID,
		"conversation_id":      record.ConversationID,
		"viewer":               viewer,
		"participants":         record.Participants,
		"capabilities_granted": record.viewerGrantedPermissions(viewer.UserID),
		"state_snapshot":       record.State.Snapshot,
		"state_version":        record.StateVersion,
		"joinable":             record.EndedAt == nil,
	}
}

func defaultSessionState(snapshot any) sessionState {
	return sessionState{
		Snapshot:                  normalizeJSONObject(snapshot),
		SessionStorage:            map[string]any{},
		SharedConversationStorage: map[string]any{},
		ProjectedMessages:         []map[string]any{},
	}
}

func stateEnvelopeFromAny(state any) sessionState {
	if state == nil {
		return defaultSessionState(nil)
	}
	if envelope, ok := state.(map[string]any); ok {
		if _, hasSnapshot := envelope["snapshot"]; hasSnapshot {
			return sessionState{
				Snapshot:                  normalizeJSONObject(envelope["snapshot"]),
				SessionStorage:            normalizeJSONObject(envelope["session_storage"]),
				SharedConversationStorage: normalizeJSONObject(envelope["shared_conversation_storage"]),
				ProjectedMessages:         normalizeMessageList(envelope["projected_messages"]),
			}
		}
	}
	return defaultSessionState(state)
}

func decodeSessionState(raw []byte) sessionState {
	if len(raw) == 0 {
		return defaultSessionState(nil)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return defaultSessionState(nil)
	}
	return stateEnvelopeFromAny(value)
}

func decodeParticipants(raw []byte) []SessionParticipant {
	if len(raw) == 0 {
		return nil
	}
	var participants []SessionParticipant
	if err := json.Unmarshal(raw, &participants); err == nil {
		return normalizeParticipants(participants, SessionParticipant{})
	}
	var legacy []string
	if err := json.Unmarshal(raw, &legacy); err == nil {
		out := make([]SessionParticipant, 0, len(legacy))
		for _, userID := range legacy {
			if userID == "" {
				continue
			}
			out = append(out, SessionParticipant{UserID: userID, Role: "PLAYER"})
		}
		return out
	}
	return nil
}

func decodeStringList(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	slices.Sort(values)
	return slices.Compact(values)
}

func normalizeParticipants(participants []SessionParticipant, viewer SessionParticipant) []SessionParticipant {
	out := make([]SessionParticipant, 0, len(participants)+1)
	seen := map[string]struct{}{}
	if viewer.UserID != "" {
		viewer = normalizeParticipant(viewer)
		out = append(out, viewer)
		seen[viewer.UserID] = struct{}{}
	}
	for _, participant := range participants {
		participant = normalizeParticipant(participant)
		if participant.UserID == "" {
			continue
		}
		if _, exists := seen[participant.UserID]; exists {
			continue
		}
		seen[participant.UserID] = struct{}{}
		out = append(out, participant)
	}
	return out
}

func normalizeParticipant(participant SessionParticipant) SessionParticipant {
	if participant.Role == "" {
		participant.Role = "PLAYER"
	}
	return participant
}

func pickViewer(participants []SessionParticipant, createdBy string) SessionParticipant {
	for _, participant := range participants {
		if participant.UserID == createdBy {
			return participant
		}
	}
	if len(participants) > 0 {
		return participants[0]
	}
	return SessionParticipant{}
}

func normalizeJSONObject(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	b, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{}
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func normalizeMessageList(value any) []map[string]any {
	b, err := json.Marshal(value)
	if err != nil {
		return []map[string]any{}
	}
	var out []map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return []map[string]any{}
	}
	if out == nil {
		return []map[string]any{}
	}
	return out
}

func nullUUIDArg(value string) any {
	if value == "" {
		return nil
	}
	return value
}
