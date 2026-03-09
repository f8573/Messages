package miniapp

import (
    "context"
    "crypto"
    "crypto/rsa"
    "crypto/sha256"
    "crypto/x509"
    "encoding/base64"
    "encoding/json"
    "encoding/pem"
    "fmt"
    "time"

    "github.com/google/uuid"
    "github.com/jackc/pgx/v5/pgxpool"
    "ohmf/services/gateway/internal/config"
)

type Service struct{
    db *pgxpool.Pool
    publicKey *rsa.PublicKey
}

func NewService(db *pgxpool.Pool, cfg config.Config) *Service {
    s := &Service{db: db}
    if cfg.MiniappPublicKeyPEM != "" {
        block, _ := pem.Decode([]byte(cfg.MiniappPublicKeyPEM))
        if block != nil {
            if pk, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
                if rsaKey, ok := pk.(*rsa.PublicKey); ok {
                    s.publicKey = rsaKey
                }
            }
        }
    }
    return s
}

// RegisterManifest stores a mini-app manifest JSON and returns its id.
func (s *Service) RegisterManifest(ctx context.Context, ownerID string, manifest any) (string, error) {
    id := uuid.New().String()
    // Ensure manifest contains a signature block per spec (must be signed).
    // Expect manifest to be a JSON object with a top-level "signature" field.
    var mmap map[string]any
    b, err := json.Marshal(manifest)
    if err != nil {
        return "", err
    }
    if err := json.Unmarshal(b, &mmap); err != nil {
        return "", err
    }
    if _, ok := mmap["signature"]; !ok {
        return "", fmt.Errorf("manifest_signature_required")
    }

    // If a public key is configured, verify the manifest signature.
    if s.publicKey != nil {
        if err := s.verifyManifestSignature(mmap); err != nil {
            return "", fmt.Errorf("manifest_signature_invalid: %w", err)
        }
    }
    _, err = s.db.Exec(ctx, `INSERT INTO miniapp_manifests (id, owner_user_id, manifest, created_at) VALUES ($1::uuid, $2::uuid, $3::jsonb, now())`, id, ownerID, string(b))
    if err != nil {
        return "", err
    }
    return id, nil
}

// verifyManifestSignature verifies a manifest map contains a base64-encoded
// RSA PKCS1v15 SHA256 signature in `signature` over the manifest JSON with the
// `signature` field removed.
func (s *Service) verifyManifestSignature(mmap map[string]any) error {
    sigVal, ok := mmap["signature"]
    if !ok {
        return fmt.Errorf("signature field missing")
    }
    sigStr, ok := sigVal.(string)
    if !ok || sigStr == "" {
        return fmt.Errorf("signature not a string")
    }

    // Build payload without the signature field
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

    sigBytes, err := base64.StdEncoding.DecodeString(sigStr)
    if err != nil {
        return err
    }
    h := sha256.Sum256(payload)
    if err := rsa.VerifyPKCS1v15(s.publicKey, crypto.SHA256, h[:], sigBytes); err != nil {
        return err
    }
    return nil
}

// CreateSession creates a runtime session for a manifest bound to a conversation.
func (s *Service) CreateSession(ctx context.Context, manifestID, conversationID string, participants []string, ttl time.Duration) (string, error) {
    id := uuid.New().String()
    partsB, _ := json.Marshal(participants)
    expires := time.Now().Add(ttl)
    _, err := s.db.Exec(ctx, `INSERT INTO miniapp_sessions (id, manifest_id, conversation_id, participants, state, expires_at, created_at) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::jsonb, '{}', $5, now())`, id, manifestID, conversationID, string(partsB), expires)
    if err != nil {
        return "", err
    }
    return id, nil
}

// GetSession returns session record as a generic map.
func (s *Service) GetSession(ctx context.Context, sessionID string) (map[string]any, error) {
    var id, manifestID, conversationID string
    var participantsB, stateB []byte
    var expiresAt, createdAt, endedAt *time.Time
    err := s.db.QueryRow(ctx, `SELECT id::text, manifest_id::text, conversation_id::text, participants, state, expires_at, created_at, ended_at FROM miniapp_sessions WHERE id = $1::uuid`, sessionID).Scan(&id, &manifestID, &conversationID, &participantsB, &stateB, &expiresAt, &createdAt, &endedAt)
    if err != nil {
        return nil, err
    }
    var participants any
    var state any
    _ = json.Unmarshal(participantsB, &participants)
    _ = json.Unmarshal(stateB, &state)
    out := map[string]any{
        "id": id,
        "manifest_id": manifestID,
        "conversation_id": conversationID,
        "participants": participants,
        "state": state,
        "expires_at": expiresAt,
        "created_at": createdAt,
        "ended_at": endedAt,
    }
    return out, nil
}

// EndSession marks a session as ended.
func (s *Service) EndSession(ctx context.Context, sessionID string) error {
    _, err := s.db.Exec(ctx, `UPDATE miniapp_sessions SET ended_at = now() WHERE id = $1::uuid`, sessionID)
    return err
}

// AppendEvent appends an event to a mini-app session's event log.
func (s *Service) AppendEvent(ctx context.Context, sessionID, actorID, eventName string, body any) (int64, error) {
    b, err := json.Marshal(body)
    if err != nil {
        return 0, err
    }
    var seq int64
    var actorArg interface{}
    if actorID == "" {
        actorArg = nil
    } else {
        actorArg = actorID
    }
    err = s.db.QueryRow(ctx, `INSERT INTO miniapp_events (app_session_id, actor_user_id, event_name, body, created_at) VALUES ($1::uuid, $2::uuid, $3, $4::jsonb, now()) RETURNING event_seq`, sessionID, actorArg, eventName, string(b)).Scan(&seq)
    if err != nil {
        return 0, err
    }
    return seq, nil
}

// SnapshotSession stores a snapshot of session state (replace state column).
func (s *Service) SnapshotSession(ctx context.Context, sessionID string, state any, version int) error {
    b, err := json.Marshal(state)
    if err != nil {
        return err
    }
    _, err = s.db.Exec(ctx, `UPDATE miniapp_sessions SET state = $1::jsonb, expires_at = now() + interval '1 hour' WHERE id = $2::uuid`, string(b), sessionID)
    return err
}

// ListManifests returns all manifests (simple listing)
func (s *Service) ListManifests(ctx context.Context) ([]map[string]any, error) {
    rows, err := s.db.Query(ctx, `SELECT id::text, owner_user_id::text, manifest, created_at FROM miniapp_manifests ORDER BY created_at DESC`)
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
        out = append(out, map[string]any{"id": id, "owner_user_id": owner, "manifest": manifest, "created_at": createdAt})
    }
    return out, nil
}
