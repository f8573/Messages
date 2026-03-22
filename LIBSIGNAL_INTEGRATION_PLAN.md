# libsignal Integration Plan

This document outlines the integration of libsignal-go for production-grade Signal protocol implementation.

## Current State
- Placeholder: Basic AES-256-GCM with structure showing integration points
- Target: Full Signal protocol (X3DH + Double Ratchet) via libsignal-go

## libsignal-go Overview

**Library**: github.com/signal-golang/libsignal-go
**Protocol**: Signal Protocol (Double Ratchet Algorithm)
**Key Exchange**: X3DH (Extended Triple Diffie-Hellman)
**Core Types**:
- `SessionRecord` - Persistent session state
- `SessionBuilder` - Creates sessions from key bundles
- `SessionCipher` - Encrypts/decrypts messages
- `IdentityKeyPair` - Long-term identity keys
- `SignedPreKey` - Medium-term signing keys
- `PreKey` - One-time prekeys for initial contact

## Integration Points

### 1. Session Manager Evolution
Current: Basic CRUD operations on database
Future: Use libsignal SessionRecord for actual state management

### 2. Key Exchange
Current: Placeholder wrapping
Future: X3DH protocol via libsignal

### 3. Message Encryption
Current: Simple nonce + concatenation
Future: Double Ratchet algorithm via libsignal

### 4. Session Serialization
Current: Raw byte storage
Future: Binary SessionRecord serialization

## Implementation Strategy

### Phase 1: Add libsignal-go Dependency
```bash
go get github.com/signal-golang/libsignal-go
```

### Phase 2: Create Signal Store Implementations
Implement libsignal's Store interfaces:
- SessionStore
- PreKeyStore
- SignedPreKeyStore
- IdentityKeyStore

### Phase 3: Update SessionManager
Replace placeholder operations with libsignal API calls

### Phase 4: Update Crypto Functions
Replace placeholder encryption/decryption with libsignal operations

### Phase 5: Testing & Validation
Comprehensive testing with production Signal protocol

## Go Dependency Addition

```go
require (
    github.com/signal-golang/libsignal-go v0.28.0 // or latest version
)
```

## Key Changes Needed

1. `SessionManager.EncryptMessageContent()` → libsignal SessionCipher
2. `SessionManager.DecryptMessageContent()` → libsignal SessionCipher
3. `GenerateRecipientWrappedKey()` → libsignal X3DH
4. Session storage format → libsignal SessionRecord binary

## Database Considerations

- `session_key_bytes`: Will change from placeholder to actual libsignal SessionRecord
- `root_key_bytes`: Managed internally by libsignal
- `chain_key_bytes`: Managed internally by libsignal
- Serialization: Binary libsignal format instead of simple concatenation

## Backward Compatibility

⚠️ **Breaking Change**: Migrating from placeholder to libsignal requires:
1. Database migration to clear old session data (non-production)
2. Or: Dual support during transition period
3. Recommendation: Clear dev/staging, deploy fresh to production

## Testing Strategy

1. Unit tests with known Signal vectors
2. Interop testing with reference implementations
3. Round-trip cipher tests
4. Multi-message ratcheting tests
5. Key rotation tests

## Timeline

- **Implementation**: 2-3 hours
- **Testing**: 2-3 hours
- **Integration testing**: 2-3 hours
- **Total**: 6-9 hours to production-ready

## Next Steps

1. Add go.mod dependency
2. Create Store implementations
3. Update SessionManager
4. Update crypto functions
5. Fix compilation errors
6. Run tests
7. Validate with Signal vectors
8. Production deployment
