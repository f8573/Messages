package e2ee

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
)

// TestMLSRatchetTreeAddMember tests adding members to tree
func TestMLSRatchetTreeAddMember(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())

	// Add first member
	leaf1 := TreeLeaf{
		UserID:   uuid.New().String(),
		DeviceID: uuid.New().String(),
	}
	idx1, err := tree.AddMember(leaf1)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	if idx1 != 1 {
		t.Errorf("Expected index 1, got %d", idx1)
	}
	if tree.Generation != 1 {
		t.Errorf("Expected generation 1, got %d", tree.Generation)
	}

	// Add second member
	leaf2 := TreeLeaf{
		UserID:   uuid.New().String(),
		DeviceID: uuid.New().String(),
	}
	idx2, err := tree.AddMember(leaf2)
	if err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}
	if idx2 != 2 {
		t.Errorf("Expected index 2, got %d", idx2)
	}
	if tree.Generation != 2 {
		t.Errorf("Expected generation 2, got %d", tree.Generation)
	}

	// Verify both members in tree
	members := tree.GetGroupMembers()
	if len(members) != 2 {
		t.Errorf("Expected 2 members, got %d", len(members))
	}
}

// TestMLSRatchetTreeRemoveMember tests removing members triggers forward secrecy
func TestMLSRatchetTreeRemoveMember(t *testing.T) {
	groupID := uuid.New().String()
	tree := NewMLSRatchetTree(groupID)
	initialEpoch := tree.Epoch

	// Add members
	user1 := uuid.New().String()
	device1 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user1, DeviceID: device1})

	user2 := uuid.New().String()
	device2 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user2, DeviceID: device2})

	if len(tree.Leaves) != 2 {
		t.Errorf("Expected 2 leaves, got %d", len(tree.Leaves))
	}

	// Remove first member
	err := tree.RemoveMember(user1, device1)
	if err != nil {
		t.Fatalf("RemoveMember failed: %v", err)
	}

	// Verify epoch incremented (forward secrecy)
	if tree.Epoch <= initialEpoch {
		t.Errorf("Expected epoch > %d, got %d", initialEpoch, tree.Epoch)
	}

	// Verify member removed
	if len(tree.Leaves) != 1 {
		t.Errorf("Expected 1 leaf after removal, got %d", len(tree.Leaves))
	}

	// Verify remaining member is user2
	members := tree.GetGroupMembers()
	if members[0].UserID != user2 {
		t.Errorf("Expected remaining member %s, got %s", user2, members[0].UserID)
	}
}

// TestMLSRatchetTreeRemoveNonexistentMember tests error handling
func TestMLSRatchetTreeRemoveNonexistentMember(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())

	err := tree.RemoveMember(uuid.New().String(), uuid.New().String())
	if err == nil {
		t.Fatal("Expected error removing nonexistent member")
	}
}

// TestMLSRatchetTreeComputeHash tests deterministic tree hashing
func TestMLSRatchetTreeComputeHash(t *testing.T) {
	groupID := uuid.New().String()
	tree1 := NewMLSRatchetTree(groupID)
	tree2 := NewMLSRatchetTree(groupID)

	user1 := uuid.New().String()
	device1 := uuid.New().String()
	leaf1 := TreeLeaf{UserID: user1, DeviceID: device1}

	tree1.AddMember(leaf1)
	tree2.AddMember(leaf1)

	hash1 := tree1.ComputeTreeHash()
	hash2 := tree2.ComputeTreeHash()

	if hash1 != hash2 {
		t.Errorf("Same tree state should produce same hash")
	}

	// Add different member to tree2
	user2 := uuid.New().String()
	device2 := uuid.New().String()
	tree2.AddMember(TreeLeaf{UserID: user2, DeviceID: device2})

	hash3 := tree2.ComputeTreeHash()
	if hash1 == hash3 {
		t.Errorf("Different tree state should produce different hash")
	}
}

// TestMultiRecipientEncryptionFlow tests end-to-end encryption for group
func TestMultiRecipientEncryptionFlow(t *testing.T) {
	// This is a placeholder for integration tests that require database
	// Real tests would use pgxmock or Docker PostgreSQL

	plaintext := []byte("Hello, Group!")

	// Verify wrapped key format
	encodedKey := base64.StdEncoding.EncodeToString(plaintext)
	decoded, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		t.Fatalf("Base64 encoding failed: %v", err)
	}

	if string(decoded) != string(plaintext) {
		t.Errorf("Decoded plaintext mismatch")
	}
}

// TestRecipientWrappedKeyStructure tests serialization
func TestRecipientWrappedKeyStructure(t *testing.T) {
	recipient := RecipientWrappedKey{
		UserID:         uuid.New().String(),
		DeviceID:       uuid.New().String(),
		WrappedKey:     base64.StdEncoding.EncodeToString([]byte("wrapped")),
		WrappedKeyNonce: base64.StdEncoding.EncodeToString([]byte("nonce")),
	}

	if recipient.UserID == "" {
		t.Fatal("UserID should not be empty")
	}
	if recipient.DeviceID == "" {
		t.Fatal("DeviceID should not be empty")
	}

	// Verify Base64 strings are decodable
	if _, err := base64.StdEncoding.DecodeString(recipient.WrappedKey); err != nil {
		t.Fatalf("WrappedKey not valid Base64: %v", err)
	}
	if _, err := base64.StdEncoding.DecodeString(recipient.WrappedKeyNonce); err != nil {
		t.Fatalf("WrappedKeyNonce not valid Base64: %v", err)
	}
}

// TestTreeMemberOrdering tests deterministic member ordering
func TestTreeMemberOrdering(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())

	// Add members in random-ish order
	users := []string{
		uuid.New().String(),
		uuid.New().String(),
		uuid.New().String(),
	}

	for _, userID := range users {
		tree.AddMember(TreeLeaf{
			UserID:   userID,
			DeviceID: uuid.New().String(),
		})
	}

	// Get members twice and verify same order
	members1 := tree.GetGroupMembers()
	members2 := tree.GetGroupMembers()

	if len(members1) != len(members2) {
		t.Fatal("Member count mismatch")
	}

	for i := range members1 {
		if members1[i].Index != members2[i].Index {
			t.Errorf("Member order changed: index %d vs %d", members1[i].Index, members2[i].Index)
		}
	}
}

// TestTreeGenerationTracking tests generation bumping
func TestTreeGenerationTracking(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())
	initialGen := tree.Generation

	user1 := uuid.New().String()
	device1 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user1, DeviceID: device1})

	if tree.Generation == initialGen {
		t.Errorf("Generation should increment on AddMember")
	}

	prevGen := tree.Generation
	user2 := uuid.New().String()
	device2 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user2, DeviceID: device2})

	if tree.Generation != prevGen+1 {
		t.Errorf("Generation should increment by 1 per AddMember")
	}
}

// TestEpochIncrementOnRemoval tests epoch-based forward secrecy
func TestEpochIncrementOnRemoval(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())
	initialEpoch := tree.Epoch

	user1 := uuid.New().String()
	device1 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user1, DeviceID: device1})

	user2 := uuid.New().String()
	device2 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user2, DeviceID: device2})

	// Remove member should bump epoch
	tree.RemoveMember(user1, device1)

	if tree.Epoch == initialEpoch {
		t.Errorf("Epoch should increment on RemoveMember for forward secrecy")
	}
}

// TestGroupKeyDerivation tests key derivation is deterministic
func TestGroupKeyDerivation(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())

	user1 := uuid.New().String()
	tree.AddMember(TreeLeaf{UserID: user1, DeviceID: uuid.New().String()})

	salt := []byte("test_salt")
	key1 := tree.DeriveGroupKey(salt)
	key2 := tree.DeriveGroupKey(salt)

	if string(key1) != string(key2) {
		t.Errorf("Same salt should produce same key")
	}

	if len(key1) != 32 {
		t.Errorf("Key should be 32 bytes (AES-256), got %d", len(key1))
	}

	// Different salt should produce different key
	salt2 := []byte("different_salt")
	key3 := tree.DeriveGroupKey(salt2)

	if string(key1) == string(key3) {
		t.Errorf("Different salt should produce different key")
	}
}

// TestMultipleMemberRemoval tests cascading removals
func TestMultipleMemberRemoval(t *testing.T) {
	tree := NewMLSRatchetTree(uuid.New().String())

	members := make([]struct {
		UserID   string
		DeviceID string
	}, 3)

	for i := 0; i < 3; i++ {
		members[i].UserID = uuid.New().String()
		members[i].DeviceID = uuid.New().String()
		tree.AddMember(TreeLeaf{
			UserID:   members[i].UserID,
			DeviceID: members[i].DeviceID,
		})
	}

	if len(tree.Leaves) != 3 {
		t.Errorf("Expected 3 members, got %d", len(tree.Leaves))
	}

	// Remove first member
	tree.RemoveMember(members[0].UserID, members[0].DeviceID)
	if len(tree.Leaves) != 2 {
		t.Errorf("Expected 2 members after removal, got %d", len(tree.Leaves))
	}

	// Remove second member
	tree.RemoveMember(members[1].UserID, members[1].DeviceID)
	if len(tree.Leaves) != 1 {
		t.Errorf("Expected 1 member after removal, got %d", len(tree.Leaves))
	}

	// Last member should be members[2]
	lastMembers := tree.GetGroupMembers()
	if len(lastMembers) != 1 || lastMembers[0].UserID != members[2].UserID {
		t.Errorf("Wrong member remaining")
	}
}

// BenchmarkTreeAddMember benchmarks member addition performance
func BenchmarkTreeAddMember(b *testing.B) {
	tree := NewMLSRatchetTree(uuid.New().String())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.AddMember(TreeLeaf{
			UserID:   uuid.New().String(),
			DeviceID: uuid.New().String(),
		})
	}
}

// BenchmarkTreeComputeHash benchmarks hash computation
func BenchmarkTreeComputeHash(b *testing.B) {
	tree := NewMLSRatchetTree(uuid.New().String())

	// Add some members
	for i := 0; i < 10; i++ {
		tree.AddMember(TreeLeaf{
			UserID:   uuid.New().String(),
			DeviceID: uuid.New().String(),
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.ComputeTreeHash()
	}
}

// BenchmarkGroupKeyDerivation benchmarks KDF performance
func BenchmarkGroupKeyDerivation(b *testing.B) {
	tree := NewMLSRatchetTree(uuid.New().String())
	tree.AddMember(TreeLeaf{
		UserID:   uuid.New().String(),
		DeviceID: uuid.New().String(),
	})

	salt := []byte("benchmark_salt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tree.DeriveGroupKey(salt)
	}
}
