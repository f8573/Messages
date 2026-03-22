# libsignal-go Integration - Final Implementation Checklist

**Status**: Ready for Implementation
**Date**: March 22, 2026
**Estimated Time**: 4-5 hours to production

---

## 📋 Integration Phases

### Phase 1: Dependency Addition (30 minutes)

#### Step 1.1: Add libsignal-go dependency

```bash
cd ohmf/services/gateway
go get github.com/signal-golang/libsignal-go@latest
go mod tidy
```

**Expected Result**:
- `go.mod` includes `github.com/signal-golang/libsignal-go v0.x.x`
- `go.sum` updated with libsignal checksums

#### Step 1.2: Verify dependency compiles

```bash
go build ./cmd/api
```

**Expected Result**: Build succeeds, all packages resolve

---

### Phase 2: Store Interface Implementation (60 minutes)

**File**: `internal/e2ee/libsignal_stores.go` (✅ CREATED)

This file provides four store implementations:

1. **PostgresSessionStore** - Implements `signal.SessionStore`
   - ✅ LoadSession(ctx, name, deviceID)
   - ✅ StoreSession(ctx, name, deviceID, sessionBytes)
   - ✅ HasSession(ctx, name, deviceID)
   - ✅ DeleteSession(ctx, name, deviceID)
   - ✅ DeleteAllSessions(ctx, name)

2. **PostgresIdentityKeyStore** - Implements `signal.IdentityKeyStore`
   - ✅ GetIdentityKeyPair(ctx)
   - ✅ GetLocalRegistrationID(ctx)
   - ✅ IsTrustedIdentity(ctx, name, deviceID, identityKey)
   - ✅ SaveIdentity(ctx, name, deviceID, identityKey)

3. **PostgresPreKeyStore** - Implements `signal.PreKeyStore`
   - ✅ LoadPreKey(ctx, prekeyID)
   - ✅ ContainsPreKey(ctx, prekeyID)
   - ✅ RemovePreKey(ctx, prekeyID)

4. **PostgresSignedPreKeyStore** - Implements `signal.SignedPreKeyStore`
   - ✅ LoadSignedPreKey(ctx, signedPreKeyID)
   - ✅ ContainsSignedPreKey(ctx, signedPreKeyID)

#### Action Items for Phase 2:

1. Review `libsignal_stores.go` for database query correctness
2. Ensure all SQL queries match your schema (e.g., `CURRENT_USER_UUID()`, `CURRENT_DEVICE_ID()` functions)
3. Add missing helper functions if needed (e.g., `CURRENT_DEVICE_ID()` function in PostgreSQL)
4. Test each store interface in isolation:
   ```bash
   go test -v ./internal/e2ee/... -run TestPostgresSessionStore
   go test -v ./internal/e2ee/... -run TestPostgresIdentityKeyStore
   go test -v ./internal/e2ee/... -run TestPostgresPreKeyStore
   go test -v ./internal/e2ee/... -run TestPostgresSignedPreKeyStore
   ```

---

### Phase 3: Enable libsignal in crypto.go (90 minutes)

**File**: `internal/e2ee/crypto_production.go`

#### Step 3.1: Uncomment libsignal import

```go
// Change from:
// "github.com/signal-golang/libsignal-go/signal"

// To:
"github.com/signal-golang/libsignal-go/signal"
```

#### Step 3.2: Implement ProductionEncryptMessage()

```go
// Replace the placeholder with:
func ProductionEncryptMessage(
    ctx context.Context,
    sessionStore *PostgresSessionStore,
    identityStore *PostgresIdentityKeyStore,
    recipientName string,
    recipientDeviceID uint32,
    messageBytes []byte,
) (ciphertext string, err error) {
    // 1. Create session cipher
    cipher := signal.NewSessionCipher(sessionStore, identityStore, recipientName, recipientDeviceID)

    // 2. Encrypt with Double Ratchet
    ciphertextBytes, err := cipher.Encrypt(ctx, messageBytes)
    if err != nil {
        return "", fmt.Errorf("libsignal encryption failed: %w", err)
    }

    // 3. Return base64-encoded ciphertext
    return base64.StdEncoding.EncodeToString(ciphertextBytes), nil
}
```

#### Step 3.3: Implement ProductionDecryptMessage()

```go
// Replace the placeholder with:
func ProductionDecryptMessage(
    ctx context.Context,
    sessionStore *PostgresSessionStore,
    identityStore *PostgresIdentityKeyStore,
    senderName string,
    senderDeviceID uint32,
    ciphertextBase64 string,
) (plaintext []byte, err error) {
    // 1. Decode base64 ciphertext
    ciphertextBytes, err := base64.StdEncoding.DecodeString(ciphertextBase64)
    if err != nil {
        return nil, fmt.Errorf("invalid ciphertext encoding: %w", err)
    }

    // 2. Create session cipher
    cipher := signal.NewSessionCipher(sessionStore, identityStore, senderName, senderDeviceID)

    // 3. Decrypt with Double Ratchet
    plaintext, err = cipher.Decrypt(ctx, ciphertextBytes)
    if err != nil {
        return nil, fmt.Errorf("libsignal decryption failed: %w", err)
    }

    return plaintext, nil
}
```

#### Step 3.4: Implement ProductionX3DH()

```go
// Replace the placeholder with:
func ProductionX3DH(
    ctx context.Context,
    sessionStore *PostgresSessionStore,
    identityStore *PostgresIdentityKeyStore,
    preKeyStore *PostgresPreKeyStore,
    recipientName string,
    recipientBundleBytes []byte,  // From client key bundle
) (*signal.SessionRecord, error) {
    // 1. Deserialize recipient's key bundle
    recipientBundle := signal.DeserializePreKeyBundle(recipientBundleBytes)

    // 2. Perform X3DH key agreement
    sessionBuilder := signal.NewSessionBuilder(
        sessionStore,
        identityStore,
        preKeyStore,
        nil,  // No signed prekey store needed for sender
    )

    // 3. Process bundle (performs X3DH)
    err := sessionBuilder.ProcessBundle(ctx, recipientName, recipientBundle)
    if err != nil {
        return nil, fmt.Errorf("X3DH failed: %w", err)
    }

    // 4. Session is now ready in sessionStore
    session, err := sessionStore.LoadSession(ctx, recipientName, recipientBundle.DeviceID)
    if err != nil {
        return nil, fmt.Errorf("failed to load new session: %w", err)
    }

    return signal.DeserializeSessionRecord(session), nil
}
```

#### Step 3.5: Update SessionManager methods

```go
// Modify crypto.go EncryptMessageContent() to use production implementation when enabled:

func EncryptMessageContent(ctx context.Context, sessionStore *PostgresSessionStore,
    identityStore *PostgresIdentityKeyStore, recipientName string,
    recipientDeviceID uint32, messageBytes []byte) (ciphertext string, nonce string, err error) {

    if ProductionSignalReadiness {
        // Use libsignal
        ct, err := ProductionEncryptMessage(ctx, sessionStore, identityStore,
            recipientName, recipientDeviceID, messageBytes)
        return ct, "", err
    } else {
        // Use placeholder (existing code)
        return encryptMessageContentPlaceholder(messageBytes, sessionKey)
    }
}
```

---

### Phase 4: Update SessionManager Initialization (60 minutes)

**File**: `internal/e2ee/handler.go` or new file `internal/e2ee/session_manager.go`

#### Step 4.1: Create SessionManager with stores

```go
type SessionManager struct {
    db                  *sql.DB
    sessionStore        *PostgresSessionStore
    identityStore       *PostgresIdentityKeyStore
    preKeyStore         *PostgresPreKeyStore
    signedPreKeyStore   *PostgresSignedPreKeyStore
}

func NewSessionManager(db *sql.DB) *SessionManager {
    return &SessionManager{
        db:                db,
        sessionStore:      NewPostgresSessionStore(db),
        identityStore:     NewPostgresIdentityKeyStore(db),
        preKeyStore:       NewPostgresPreKeyStore(db),
        signedPreKeyStore: NewPostgresSignedPreKeyStore(db),
    }
}
```

#### Step 4.2: Update message encryption flow

```go
// In messages/service.go Send() method:

if msg.IsEncrypted && ProductionSignalReadiness {
    // Use libsignal-backed encryption
    sessionMgr := NewSessionManager(s.db)

    ciphertext, err := sessionMgr.EncryptMessageContent(
        ctx,
        contactUserID,
        contactDeviceID,
        plaintext,
    )
    if err != nil {
        return nil, fmt.Errorf("libsignal encryption failed: %w", err)
    }

    msg.Content["ciphertext"] = ciphertext
} else if msg.IsEncrypted {
    // Use placeholder encryption
    // ... existing code
}
```

#### Step 4.3: Update message decryption flow

```go
// In sync/service.go or messages/handler.go when delivering messages:

if msg.IsEncrypted && ProductionSignalReadiness {
    // Can optionally validate decryption server-side
    // (Not typically done - client decrypts)
    sessionMgr := NewSessionManager(s.db)

    plaintext, err := sessionMgr.DecryptMessageContent(
        ctx,
        senderUserID,
        senderDeviceID,
        msg.Content["ciphertext"].(string),
    )
    // ... validate for server-side checks if needed
} else {
    // Ciphertext delivered as-is to client
}
```

---

### Phase 5: Testing & Validation (60 minutes)

#### Step 5.1: Unit Tests

Create `internal/e2ee/libsignal_integration_test.go`:

```go
package e2ee

import (
    "context"
    "testing"
    "github.com/signal-golang/libsignal-go/signal"
)

func TestLibSignalEncryptDecrypt(t *testing.T) {
    // 1. Create two users with sessions
    // 2. User A sends encrypted message to User B
    // 3. Verify ciphertext ≠ plaintext
    // 4. User B decrypts
    // 5. Verify plaintext matches original
}

func TestX3DHKeyExchange(t *testing.T) {
    // 1. Generate User A identity keys
    // 2. Generate User B key bundle
    // 3. User A performs X3DH with User B's bundle
    // 4. Verify session established
    // 5. Verify encryption works
}

func TestMessageRatcheting(t *testing.T) {
    // 1. Send message 1: verify ratchet state changes
    // 2. Send message 2: verify can still decrypt message 1
    // 3. Receive out-of-order message: verify still works
}

func TestDeviceRevocation(t *testing.T) {
    // 1. Establish session
    // 2. Revoke device
    // 3. Verify HandleDeviceRevocationE2EE() deletes sessions
    // 4. Verify new session required
}
```

#### Step 5.2: Integration Tests

```bash
# Test compilation
go build -v ./internal/e2ee/...

# Test all E2EE code
go test -v ./internal/e2ee/...

# Test with coverage
go test -cover ./internal/e2ee/...

# Test with race detector
go test -race ./internal/e2ee/...

# Benchmark encryption/decryption
go test -bench=. ./internal/e2ee/... -benchmem
```

#### Step 5.3: Validation with Signal Test Vectors

libsignal provides test vectors for cross-library validation:
- Download from https://github.com/signalapp/libsignal/tree/main/vectors
- Test against reference implementations
- Verify compatibility

---

### Phase 6: Enable Production Mode (30 minutes)

#### Step 6.1: Set production readiness flag

```go
// In internal/e2ee/crypto_production.go:

// Change from:
const ProductionSignalReadiness = false

// To:
const ProductionSignalReadiness = true
```

#### Step 6.2: Build and validate

```bash
go build -o gateway ./cmd/api

# Verify binary created
ls -lh gateway
```

#### Step 6.3: Deploy to staging

- Deploy gateway binary to staging environment
- Run smoke tests
- Verify encrypted messages work end-to-end

---

## 🔧 Key Configuration Items

### Database Functions Required

These PostgreSQL functions must exist:

```sql
-- Get current authenticated user ID
CREATE OR REPLACE FUNCTION CURRENT_USER_UUID() RETURNS UUID AS $$
BEGIN
    RETURN current_setting('app.current_user_id')::uuid;
END;
$$ LANGUAGE plpgsql;

-- Get current authenticated device ID
CREATE OR REPLACE FUNCTION CURRENT_DEVICE_ID() RETURNS BIGINT AS $$
BEGIN
    RETURN current_setting('app.current_device_id')::bigint;
END;
$$ LANGUAGE plpgsql;

-- Set user context from JWT
SELECT set_config('app.current_user_id', 'user-uuid', false);
SELECT set_config('app.current_device_id', 'device-id', false);
```

### Environment Variables

```bash
# Enable libsignal integration
export LIBSIGNAL_ENABLED=true

# Signal protocol constants
export SIGNAL_PROTOCOL_VERSION="1.0"
export X3DH_PROTOCOL_VERSION="1.0"
```

---

## ✅ Pre-Production Checklist

Before deploying to production:

- [ ] All 4 Store interfaces implemented
- [ ] Unit tests pass (>80% coverage)
- [ ] Integration tests pass
- [ ] Production crypto functions enabled
- [ ] Database functions exist
- [ ] Staging deployment tested
- [ ] No regressions in plaintext messages
- [ ] Encrypted messages store ciphertext correctly
- [ ] Signatures verify correctly
- [ ] Device revocation works
- [ ] Account deletion cleanup works
- [ ] Performance within SLA (encryption/decryption <100ms)
- [ ] Load testing completed (1000+ messages/sec)
- [ ] Security audit passed
- [ ] Team trained on E2EE operations

---

## 🚀 Production Rollout Strategy

### Week 1: Backend Deployment
```bash
# Apply code changes
git commit -m "feat: Enable libsignal-go for production E2EE"

# Deploy to staging
# Run full test suite
# Validate with monitoring

# Deploy to production (5% of users)
# Monitor error rates, performance
```

### Week 2: Staged Rollout
- 5% users: 24 hours of monitoring
- 25% users: 24 hours of monitoring
- 100% users: Full rollout

### Week 3-4: Monitoring & Optimization
- Monitor encryption/decryption latency
- Check database performance
- Adjust indexes if needed
- Optimize query patterns

---

## 📊 Performance Targets

| Metric | Target | Priority |
|--------|--------|----------|
| Encryption latency | <50ms | Critical |
| Decryption latency | <50ms | Critical |
| X3DH key exchange | <200ms | High |
| Database query (session load) | <10ms | High |
| Message throughput | 1000+ msg/sec | High |

---

## 🆘 Troubleshooting Guide

### Issue: Build fails with undefined libsignal types

**Solution**: Ensure `go get` completed successfully
```bash
go get -u github.com/signal-golang/libsignal-go
go mod verify
go build ./cmd/api -v
```

### Issue: Decryption fails with "invalid session"

**Solution**: Verify X3DH completed and session established
- Check `e2ee_sessions` table has session record
- Verify `device_key_trust` has TOFU entry
- Ensure sender/recipient device IDs match

### Issue: Performance degradation

**Solution**: Check database indexes
```sql
-- Verify indexes exist
SELECT * FROM pg_indexes
WHERE tablename IN ('e2ee_sessions', 'device_key_trust');

-- Add if missing
CREATE INDEX idx_e2ee_sessions_lookup
  ON e2ee_sessions(user_id, contact_user_id, contact_device_id);
```

### Issue: Signature verification failing

**Solution**: Ensure correct key is being used
- Verify sender's public signing key is from `device_identity_keys`
- Check signature algorithm matches (Ed25519)
- Validate signature was computed over correct message bytes

---

## 📚 Reference Documentation

- **Signal Protocol Spec**: https://signal.org/docs/
- **libsignal-go GitHub**: https://github.com/signalapp/libsignal
- **Double Ratchet Algorithm**: https://signal.org/docs/specifications/doubleratchet/
- **X3DH Protocol**: https://signal.org/docs/specifications/x3dh/
- **libsignal Test Vectors**: https://github.com/signalapp/libsignal/tree/main/vectors

---

**Timeline**: 4-5 hours total implementation time
**Status**: ✅ Framework ready, waiting for libsignal integration
**Quality**: Production-ready architecture

