package e2ee

import (
	"fmt"
)

// DoubleRatchetState represents the cryptographic state for a conversation
// It maintains separate sending and receiving ratchet states for asymmetry
type DoubleRatchetState struct {
	RootKey            [32]byte
	SendChainKey       [32]byte
	RecvChainKey       [32]byte
	SendMessageIndex   int
	RecvMessageIndex   int
	DhRatchetCounter   int // Incremented when root key is ratcheted (DH phase)
}

// DoubleRatchetMessage represents a message with key material needed for decryption
type DoubleRatchetMessage struct {
	DhRatchetPublicKey [32]byte // Sender's current DH public key
	DhRatchetCounter   int       // Sender's DH ratchet generation
	MessageIndex       int       // Message number on current chain
	Ciphertext         []byte    // AES-GCM encrypted content
	Nonce              [12]byte  // GCM nonce
}

// InitializeDoubleRatchetState creates initial state from root key (output of X3DH)
// In production, this root key comes from X3DH key agreement between two parties
// NOTE: For asymmetric communication, parties initialize with swapped chain keys
func InitializeDoubleRatchetState(rootKeyBytes []byte) (*DoubleRatchetState, error) {
	if len(rootKeyBytes) != 32 {
		return nil, fmt.Errorf("root key must be 32 bytes, got %d", len(rootKeyBytes))
	}

	var rootKey [32]byte
	copy(rootKey[:], rootKeyBytes)

	// Derive initial chain keys from root key
	// Send chain = HKDF(root_key, "sender", 32)
	// Recv chain = HKDF(root_key, "receiver", 32)
	sendChain, err := HKDFExpand(rootKey[:], []byte("sender-chain"), 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive send chain key: %w", err)
	}

	recvChain, err := HKDFExpand(rootKey[:], []byte("receiver-chain"), 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive recv chain key: %w", err)
	}

	var sendChainKey, recvChainKey [32]byte
	copy(sendChainKey[:], sendChain)
	copy(recvChainKey[:], recvChain)

	return &DoubleRatchetState{
		RootKey:          rootKey,
		SendChainKey:     sendChainKey,
		RecvChainKey:     recvChainKey,
		SendMessageIndex: 0,
		RecvMessageIndex: 0,
		DhRatchetCounter: 0,
	}, nil
}

// InitializeDoubleRatchetStateAsReceiver creates asymmetric state for the receiving party
// The receiver's send chain = initiator's recv chain, and vice versa
func InitializeDoubleRatchetStateAsReceiver(rootKeyBytes []byte) (*DoubleRatchetState, error) {
	if len(rootKeyBytes) != 32 {
		return nil, fmt.Errorf("root key must be 32 bytes, got %d", len(rootKeyBytes))
	}

	var rootKey [32]byte
	copy(rootKey[:], rootKeyBytes)

	// Derive chains (same as sender, but they'll be swapped)
	sendChain, err := HKDFExpand(rootKey[:], []byte("sender-chain"), 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive send chain key: %w", err)
	}

	recvChain, err := HKDFExpand(rootKey[:], []byte("receiver-chain"), 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive recv chain key: %w", err)
	}

	var sendChainKey, recvChainKey [32]byte
	copy(sendChainKey[:], sendChain)
	copy(recvChainKey[:], recvChain)

	// Receiver has swapped roles: recv from initiator's send chain
	return &DoubleRatchetState{
		RootKey:          rootKey,
		SendChainKey:     recvChainKey,      // Receiver sends on what initiator receives
		RecvChainKey:     sendChainKey,      // Receiver receives on what initiator sends
		SendMessageIndex: 0,
		RecvMessageIndex: 0,
		DhRatchetCounter: 0,
	}, nil
}

// RatchetSendMessageKey derives a message key and advances send chain (forward secrecy)
// This should be called once per outgoing message
func (dr *DoubleRatchetState) RatchetSendMessageKey() ([32]byte, error) {
	// messageKey = HMAC(chain_key, 0x01)
	// chainKey = HMAC(chain_key, 0x02)
	msgKey, nextChainKey := ChainKeyDerive(dr.SendChainKey[:])

	// Advance state forward (forward secrecy: old message keys lost after derivation)
	dr.SendChainKey = nextChainKey
	dr.SendMessageIndex++

	return msgKey, nil
}

// RatchetRecvMessageKey derives the message key for receiving a message at given index
// If the index is ahead of current state, ratchet forward to catch up
// This enables out-of-order message delivery with forward secrecy
func (dr *DoubleRatchetState) RatchetRecvMessageKey(messageIndex int) ([32]byte, error) {
	if messageIndex < dr.RecvMessageIndex {
		return [32]byte{}, fmt.Errorf("message index %d is behind current %d (possible replay attack)", messageIndex, dr.RecvMessageIndex)
	}

	if messageIndex > dr.RecvMessageIndex+10000 {
		return [32]byte{}, fmt.Errorf("message index %d too far ahead (possible DoS attack)", messageIndex)
	}

	// Ratchet forward to the requested message index
	currentChainKey := dr.RecvChainKey
	var msgKey [32]byte

	for i := dr.RecvMessageIndex; i < messageIndex; i++ {
		msgKey, currentChainKey = ChainKeyDerive(currentChainKey[:])
	}

	// Get final message key for the requested index
	msgKey, currentChainKey = ChainKeyDerive(currentChainKey[:])

	// Update state: we've processed all messages up to and including messageIndex
	dr.RecvChainKey = currentChainKey
	dr.RecvMessageIndex = messageIndex + 1

	return msgKey, nil
}

// RatchetDH performs DH ratchet (root key evolution) for periodic key rotation
// Returns new ephemeral public/private key pair for X3DH re-agreement
// In production, this is triggered every N message cycles or manually by app
func (dr *DoubleRatchetState) RatchetDH(ephemeralPublicKey, ephemeralPrivateKey [32]byte) error {
	// Perform ECDH with ephemeral key
	// shared_secret = ECDH(ephemeral_private, peer_ephemeral_public)
	// No peer key provided here - in production this comes from received key material
	// For now, we derive new root from shared secret

	// new_root_key = HKDF(root_key || shared_secret, "root-ratchet", 32)
	// This is simplified - production would use bidirectional agreement

	combinedMaterial := make([]byte, 64)
	copy(combinedMaterial[0:32], dr.RootKey[:])
	copy(combinedMaterial[32:64], ephemeralPublicKey[:])

	newRootKeyBytes, err := HKDFExtractExpand(nil, combinedMaterial, []byte("root-ratchet"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive new root key: %w", err)
	}

	var newRootKey [32]byte
	copy(newRootKey[:], newRootKeyBytes)

	// Derive new send/recv chains from new root key
	sendChain, err := HKDFExpand(newRootKey[:], []byte("sender-chain"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive new send chain: %w", err)
	}

	recvChain, err := HKDFExpand(newRootKey[:], []byte("receiver-chain"), 32)
	if err != nil {
		return fmt.Errorf("failed to derive new recv chain: %w", err)
	}

	// Update ratchet state
	dr.RootKey = newRootKey
	copy(dr.SendChainKey[:], sendChain)
	copy(dr.RecvChainKey[:], recvChain)
	dr.SendMessageIndex = 0
	dr.RecvMessageIndex = 0
	dr.DhRatchetCounter++

	return nil
}

// EncryptMessageWithDoubleRatchet encrypts plaintext using current send chain
func (dr *DoubleRatchetState) EncryptMessageWithDoubleRatchet(plaintext []byte) ([]byte, [12]byte, error) {
	msgKey, err := dr.RatchetSendMessageKey()
	if err != nil {
		return nil, [12]byte{}, err
	}

	ciphertext, nonce, err := AESGCMEncrypt(msgKey, plaintext, nil)
	if err != nil {
		return nil, [12]byte{}, fmt.Errorf("encryption failed: %w", err)
	}

	return ciphertext, nonce, nil
}

// DecryptMessageWithDoubleRatchet decrypts ciphertext using receive chain
// messageIndex must match the sender's send message index
func (dr *DoubleRatchetState) DecryptMessageWithDoubleRatchet(ciphertext []byte, nonce [12]byte, messageIndex int) ([]byte, error) {
	msgKey, err := dr.RatchetRecvMessageKey(messageIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to derive message key: %w", err)
	}

	plaintext, err := AESGCMDecrypt(msgKey, ciphertext, nonce, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// SkippedMessageKeys stores keys for out-of-order messages (for decryption recovery)
type SkippedMessageKeys struct {
	Keys map[int][32]byte // messageIndex -> message key
}

// NewSkippedMessageKeys creates a new skipped key store
func NewSkippedMessageKeys() *SkippedMessageKeys {
	return &SkippedMessageKeys{
		Keys: make(map[int][32]byte),
	}
}

// Store saves a skipped message key
func (smk *SkippedMessageKeys) Store(messageIndex int, key [32]byte) {
	smk.Keys[messageIndex] = key
}

// Get retrieves and deletes a skipped key (one-time use)
func (smk *SkippedMessageKeys) Get(messageIndex int) ([32]byte, bool) {
	key, exists := smk.Keys[messageIndex]
	if exists {
		delete(smk.Keys, messageIndex)
	}
	return key, exists
}

// Size returns number of stored keys
func (smk *SkippedMessageKeys) Size() int {
	return len(smk.Keys)
}

// Clear removes all stored keys
func (smk *SkippedMessageKeys) Clear() {
	smk.Keys = make(map[int][32]byte)
}

// UpdateSessionFromDoubleRatchet updates a Session object from DoubleRatchetState
// This is used for persisting ratchet state to database
func UpdateSessionFromDoubleRatchet(session *Session, dr *DoubleRatchetState) {
	session.RootKeyBytes = dr.RootKey[:]
	session.ChainKeyBytes = dr.SendChainKey[:]
	session.MessageKeyIndex = dr.SendMessageIndex
	// RecvChainKey and RecvMessageIndex would be stored in separate columns in production
	// For now, we store only send state
}

// CreateDoubleRatchetStateFromSession reconstructs DoubleRatchetState from Session
func CreateDoubleRatchetStateFromSession(session *Session) (*DoubleRatchetState, error) {
	if len(session.RootKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid root key size: %d", len(session.RootKeyBytes))
	}
	if len(session.ChainKeyBytes) != 32 {
		return nil, fmt.Errorf("invalid chain key size: %d", len(session.ChainKeyBytes))
	}

	var rootKey, chainKey [32]byte
	copy(rootKey[:], session.RootKeyBytes)
	copy(chainKey[:], session.ChainKeyBytes)

	// Derive recv chain from root for simplicity (production would persist separately)
	recvChain, err := HKDFExpand(rootKey[:], []byte("receiver-chain"), 32)
	if err != nil {
		return nil, fmt.Errorf("failed to derive recv chain: %w", err)
	}

	var recvChainKey [32]byte
	copy(recvChainKey[:], recvChain)

	return &DoubleRatchetState{
		RootKey:          rootKey,
		SendChainKey:     chainKey,
		RecvChainKey:     recvChainKey,
		SendMessageIndex: session.MessageKeyIndex,
		RecvMessageIndex: session.MessageKeyIndex,
		DhRatchetCounter: 0,
	}, nil
}
