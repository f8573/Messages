# libsignal-go Integration Guide

## Current State
- **Status**: Placeholder implementation in place
- **File**: `internal/e2ee/crypto_production.go`
- **Purpose**: Framework and integration points for libsignal

## libsignal-go Overview

**Library**: github.com/signal-golang/libsignal-go
**Latest Version**: v0.28.0 (as of March 2026)
**Protocol**: Signal Protocol (Double Ratchet + X3DH)
**Documentation**: https://github.com/signalapp/libsignal

## Integration Steps

### Step 1: Add Dependency
```bash
cd ohmf/services/gateway
go get github.com/signal-golang/libsignal-go@latest
go mod tidy
```

### Step 2: Implement Store Interfaces

libsignal requires implementing four store interfaces:

#### A. SessionStore
```go
type SessionStore interface {
    LoadSession(ctx context.Context, address *signal.SignalAddress) (*signal.SessionRecord, error)
    StoreSession(ctx context.Context, address *signal.SignalAddress, record *signal.SessionRecord) error
    HasSession(ctx context.Context, address *signal.SignalAddress) (bool, error)
    DeleteSession(ctx context.Context, address *signal.SignalAddress) error
    DeleteAllSessions(ctx context.Context, name string) error
}
```

Implementation: Read/write from `e2ee_sessions` PostgreSQL table

#### B. IdentityKeyStore
```go
type IdentityKeyStore interface {
    GetIdentityKeyPair(ctx context.Context) (*signal.IdentityKeyPair, error)
    GetLocalRegistrationID(ctx context.Context) (uint32, error)
    IsTrustedIdentity(ctx context.Context, address *signal.SignalAddress, identityKey *signal.IdentityKey) (bool, error)
    SaveIdentity(ctx context.Context, address *signal.SignalAddress, identityKey *signal.IdentityKey) (bool, error)
}
```

Implementation: Read/write from `device_key_trust` and `device_identity_keys` PostgreSQL tables

#### C. PreKeyStore
```go
type PreKeyStore interface {
    LoadPreKey(ctx context.Context, preKeyID uint32) (*signal.PreKeyRecord, error)
    StorePreKey(ctx context.Context, preKeyID uint32, record *signal.PreKeyRecord) error
    ContainsPreKey(ctx context.Context, preKeyID uint32) (bool, error)
    RemovePreKey(ctx context.Context, preKeyID uint32) error
}
```

Implementation: Read/write from `device_one_time_prekeys` PostgreSQL table

#### D. SignedPreKeyStore
```go
type SignedPreKeyStore interface {
    LoadSignedPreKey(ctx context.Context, signedPreKeyID uint32) (*signal.SignedPreKeyRecord, error)
    StoreSignedPreKey(ctx context.Context, signedPreKeyID uint32, record *signal.SignedPreKeyRecord) error
    ContainsSignedPreKey(ctx context.Context, signedPreKeyID uint32) (bool, error)
}
```

Implementation: Read from `device_identity_keys`, write handled by key rotation

### Step 3: Update ProductionSignalReadiness Flag

In `crypto_production.go`:

```go
// Change from:
const ProductionSignalReadiness = false

// To:
const ProductionSignalReadiness = true
```

### Step 4: Uncomment Production Implementations

In `crypto_production.go`, uncomment:
- `ProductionEncryptMessage()`
- `ProductionDecryptMessage()`
- `ProductionX3DH()`
- `ProductionKeyAgreement()`
- `InitializeSessionWithLibSignal()`

### Step 5: Update SessionManager Methods

Replace placeholder implementations with libsignal calls:

```go
// SessionManager.EncryptMessage() becomes:
func (sm *SessionManager) EncryptMessage(ctx context.Context, address *signal.SignalAddress, plaintext []byte) (ciphertext []byte, err error) {
    cipher := signal.NewSessionCipher(sm.sessionStore, sm.identityStore, address)
    return cipher.Encrypt(ctx, plaintext)
}

// SessionManager.DecryptMessage() becomes:
func (sm *SessionManager) DecryptMessage(ctx context.Context, address *signal.SignalAddress, ciphertext []byte) (plaintext []byte, err error) {
    cipher := signal.NewSessionCipher(sm.sessionStore, sm.identityStore, address)
    return cipher.Decrypt(ctx, ciphertext)
}
```

### Step 6: Update Message Handling

In `internal/messages/encryption_middleware.go`:

```go
// Update ProcessEncryptedMessage() to:
// 1. Verify signature using sender's signing key
// 2. Use SessionCipher to decrypt if needed (server-side validation)
// 3. Verify ciphertext integrity

// Current: Just validates signature and recipients
// Future: Can also validate decryption path if needed for server-side testing
```

### Step 7: Database Schema Adjustments

Session serialization changes:

```sql
-- Current (Placeholder):
session_key_bytes BYTEA -- Raw bytes, placeholder format

-- After libsignal:
session_key_bytes BYTEA -- libsignal SessionRecord serialized binary

-- No schema change needed - just the format changes
-- Recommendation: Clear dev/staging, migrate fresh for production
```

### Step 8: Add Tests

Create `internal/e2ee/libsignal_test.go`:

```go
package e2ee

import (
    "context"
    "testing"
    "github.com/signal-golang/libsignal-go/signal"
)

// Test basic encryption/decryption round trip
func TestLibSignalRoundTrip(t *testing.T) {
    // 1. Generate identity keys
    // 2. Create sessions
    // 3. Encrypt message
    // 4. Decrypt message
    // 5. Verify plaintext matches
}

// Test X3DH key agreement
func TestX3DHKeyExchange(t *testing.T) {
    // 1. Generate sender and recipient key bundles
    // 2. Perform X3DH on both sides
    // 3. Verify shared secrets match
}

// Test message ratcheting
func TestMessageRatcheting(t *testing.T) {
    // 1. Send multiple messages
    // 2. Verify ratchet state changes
    // 3. Verify out-of-order delivery handling
}

// Test with Signal test vectors
func TestSignalVectors(t *testing.T) {
    // Use official Signal protocol test vectors
    // Validate implementations match reference
}
```

### Step 9: Validation

Run pre-production validation:

```bash
# 1. Compilation check
go build -v ./internal/e2ee/...

# 2. Unit tests
go test -v ./internal/e2ee/... -count=1

# 3. Race detector
go test -race ./internal/e2ee/...

# 4. Coverage
go test -cover ./internal/e2ee/...

# 5. Benchmarks
go test -bench=. ./internal/e2ee/... -benchmem
```

### Step 10: Documentation

Update documentation:

1. **API Contract**: Document that `content_type: "encrypted"` now uses actual Signal protocol
2. **Deployment**: Note that database sessions will use new format
3. **Migration**: Document any migration from placeholder to production
4. **Troubleshooting**: Add libsignal-specific debugging tips

## Timeline

| Phase | Duration | Tasks |
|-------|----------|-------|
| Setup | 30 min | Add dependency, read libsignal docs |
| Store Interfaces | 60 min | Implement 4 store interfaces |
| Core Updates | 90 min | Update SessionManager, crypto functions |
| Testing | 60 min | Write unit tests, validate |
| Production Prep | 30 min | Performance test, final validation |
| **Total** | **4-5 hours** | Ready for production |

## Current Placeholder Status

### What Works Now
✅ Encryption validation structure
✅ Signature verification (Ed25519)
✅ Session CRUD operations
✅ Trust state management
✅ Message validation middleware
✅ API contract definition

### What's Placeholder
⏳ Message encryption (uses simple nonce concatenation)
⏳ Message decryption (reverses nonce concatenation)
⏳ Key wrapping (basic base64 encoding)
⏳ X3DH key agreement (not implemented)
⏳ Double Ratchet (not implemented)

### What to Replace
1. `EncryptMessageContent()` → libsignal SessionCipher.Encrypt()
2. `DecryptMessageContent()` → libsignal SessionCipher.Decrypt()
3. `GenerateRecipientWrappedKey()` → libsignal X3DH
4. `UnwrapSessionKey()` → libsignal X3DH decapsulation

## Security Considerations

### Pre-libsignal (Current)
- ❌ **Not suitable for production E2EE**
- ✅ Structure correct for validation
- ✅ Signature verification works
- ✅ Key management correct
- ❌ **Message content not truly encrypted**

### Post-libsignal (After Integration)
- ✅ **Production-grade Signal Protocol**
- ✅ **Forward secrecy via ratcheting**
- ✅ **Out-of-order message handling**
- ✅ **Key compromise resilience**
- ✅ **Standard compliance verified**

## Deployment Strategy

### Option A: Fresh Deployment (Recommended for MVP)
1. Deploy placeholder backend
2. Test API contracts with mock clients
3. Integrate libsignal
4. Clear dev/staging databases
5. Deploy production

### Option B: Migration Path (if data needed)
1. Deploy placeholder backend
2. Accumulate test data (optional, not production)
3. Integrate libsignal  with migration code
4. Run migration to update session format
5. Deploy production

## Known Limitations (Pre-libsignal)

| Item | Status | Note |
|------|--------|------|
| Message encryption | ⏳ Placeholder | Simple nonce concat, not production |
| X3DH key exchange | ⏳ Not implemented | Framework ready |
| Double Ratchet | ⏳ Not implemented | Framework ready |
| Forward secrecy | ❌ No | Requires Double Ratchet |
| Out-of-order delivery | ⚠️ Not tested | Requires ratchet state |
| Message replay protection | ❌ No | Requires counter/ratchet |
| Identity verification | ✅ Works | TOFU + fingerprints |
| Signature verification | ✅ Works | Ed25519 functional |

## Help & Resources

- **libsignal-go GitHub**: https://github.com/signalapp/libsignal
- **Signal Protocol Spec**: https://signal.org/docs/
- **Double Ratchet Algorithm**: https://signal.org/docs/specifications/doubleratchet/
- **X3DH Protocol**: https://signal.org/docs/specifications/x3dh/

## Next Steps

1. **Immediate** (This session):
   - ✅ Framework in place
   - ✅ Placeholder working
   - ✅ Integration guide ready

2. **Short term** (Next session):
   - Add libsignal-go dependency
   - Implement store interfaces
   - Replace placeholder functions
   - Run validation tests

3. **Medium term** (Week after):
   - Client-side E2EE (Web + Android)
   - End-to-end integration tests
   - Security audit

4. **Production** (2-3 weeks):
   - Staged rollout
   - Monitoring and optimization
   - Full feature release

---

**Status**: Framework complete, ready for libsignal-go integration
**Quality**: Production-ready architecture
**Security**: Placeholder now, Signal protocol when integrated
