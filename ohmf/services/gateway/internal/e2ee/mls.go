package e2ee

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MLSRatchetTree represents a binary tree for group key derivation
// Implements Material Layer Security (MLS) tree structure
type MLSRatchetTree struct {
	GroupID    string
	Generation int64
	Epoch      int64
	TreeBytes  []byte
	Leaves     map[int]TreeLeaf  // leaf_index → device info
}

// TreeLeaf represents a leaf node in the ratchet tree (a group member device)
type TreeLeaf struct {
	Index     int
	UserID    string
	DeviceID  string
	PublicKey []byte  // X25519 encryption key
}

// TreeNode represents an internal node in the ratchet tree
type TreeNode struct {
	Index    int
	LeftIdx  int
	RightIdx int
	KeyBytes []byte  // Derived group key material
}

// NewMLSRatchetTree creates a new ratchet tree for a group
func NewMLSRatchetTree(groupID string) *MLSRatchetTree {
	return &MLSRatchetTree{
		GroupID:    groupID,
		Generation: 0,
		Epoch:      0,
		Leaves:     make(map[int]TreeLeaf),
	}
}

// AddMember inserts a new device as a leaf in the tree, returns assigned leaf index
func (t *MLSRatchetTree) AddMember(leaf TreeLeaf) (int, error) {
	// Find next available leaf index
	var maxIndex int
	for idx := range t.Leaves {
		if idx > maxIndex {
			maxIndex = idx
		}
	}
	leaf.Index = maxIndex + 1

	t.Leaves[leaf.Index] = leaf
	t.Generation++
	return leaf.Index, nil
}

// RemoveMember blanks leaf node, bumps generation and epoch (forward secrecy)
func (t *MLSRatchetTree) RemoveMember(userID, deviceID string) error {
	// Find the member's leaf
	var leafIndex int
	found := false
	for idx, leaf := range t.Leaves {
		if leaf.UserID == userID && leaf.DeviceID == deviceID {
			leafIndex = idx
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("member not found: %s/%s", userID, deviceID)
	}

	// Remove from tree
	delete(t.Leaves, leafIndex)
	t.Generation++
	t.Epoch++  // Epoch bump = force new group secret (old keys invalid)
	return nil
}

// GetGroupMembers returns all current members sorted by leaf index
func (t *MLSRatchetTree) GetGroupMembers() []TreeLeaf {
	members := make([]TreeLeaf, 0, len(t.Leaves))
	for _, leaf := range t.Leaves {
		members = append(members, leaf)
	}
	// Sort by index for deterministic order
	sort.Slice(members, func(i, j int) bool {
		return members[i].Index < members[j].Index
	})
	return members
}

// DeriveGroupKey generates group encryption key for epoch
// In production, this uses KDF with group secret
func (t *MLSRatchetTree) DeriveGroupKey(salt []byte) []byte {
	// Simple KDF placeholder: hash(tree_bytes || epoch || salt)
	// Production: HKDF-Expand(group_secret, context_string)
	h := sha256.New()
	h.Write(t.TreeBytes)
	h.Write([]byte(fmt.Sprintf("%d:%d", t.Generation, t.Epoch)))
	h.Write(salt)
	return h.Sum(nil)  // 32 bytes for AES-256
}

// ComputeTreeHash computes deterministic hash of current tree state
// Used for detecting unauthorized changes
func (t *MLSRatchetTree) ComputeTreeHash() string {
	h := sha256.New()
	members := t.GetGroupMembers()
	for _, leaf := range members {
		h.Write([]byte(leaf.DeviceID))
		h.Write(leaf.PublicKey)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// MLSSessionStore manages MLS group sessions in database
type MLSSessionStore struct {
	db *pgxpool.Pool
}

// NewMLSSessionStore creates a store for MLS operations
func NewMLSSessionStore(db *pgxpool.Pool) *MLSSessionStore {
	return &MLSSessionStore{db: db}
}

// SaveRatchetTree persists tree state to database
func (s *MLSSessionStore) SaveRatchetTree(ctx context.Context, tree *MLSRatchetTree) error {
	query := `
		INSERT INTO group_ratchet_trees (group_id, generation, tree_bytes, epoch)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (group_id) DO UPDATE SET
			generation = EXCLUDED.generation,
			tree_bytes = EXCLUDED.tree_bytes,
			epoch = EXCLUDED.epoch,
			updated_at = NOW()
	`
	_, err := s.db.Exec(ctx, query, tree.GroupID, tree.Generation, tree.TreeBytes, tree.Epoch)
	return err
}

// LoadRatchetTree retrieves tree state from database
func (s *MLSSessionStore) LoadRatchetTree(ctx context.Context, groupID string) (*MLSRatchetTree, error) {
	query := `
		SELECT group_id, generation, tree_bytes, epoch
		FROM group_ratchet_trees
		WHERE group_id = $1
	`
	var tree MLSRatchetTree
	err := s.db.QueryRow(ctx, query, groupID).Scan(
		&tree.GroupID, &tree.Generation, &tree.TreeBytes, &tree.Epoch,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil  // Tree doesn't exist yet
		}
		return nil, err
	}
	tree.Leaves = make(map[int]TreeLeaf)
	return &tree, nil
}

// SaveMemberLeaves stores member-to-leaf mappings
func (s *MLSSessionStore) SaveMemberLeaves(ctx context.Context, groupID string, leaves []TreeLeaf, generation int64) error {
	const query = `
		INSERT INTO group_member_tree_leaves (group_id, user_id, device_id, leaf_index, generation)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`
	for _, leaf := range leaves {
		_, err := s.db.Exec(ctx, query, groupID, leaf.UserID, leaf.DeviceID, leaf.Index, generation)
		if err != nil {
			return err
		}
	}
	return nil
}

// LoadMemberLeaves retrieves member-to-leaf mappings for current generation
func (s *MLSSessionStore) LoadMemberLeaves(ctx context.Context, groupID string) ([]TreeLeaf, error) {
	query := `
		SELECT leaf_index, user_id, device_id
		FROM group_member_tree_leaves
		WHERE group_id = $1
		ORDER BY leaf_index ASC
	`
	rows, err := s.db.Query(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var leaves []TreeLeaf
	for rows.Next() {
		var idx int
		var userID, deviceID string
		if err := rows.Scan(&idx, &userID, &deviceID); err != nil {
			return nil, err
		}
		leaves = append(leaves, TreeLeaf{
			Index:    idx,
			UserID:   userID,
			DeviceID: deviceID,
		})
	}
	return leaves, rows.Err()
}

// SaveGroupEpoch stores group secret for an epoch
func (s *MLSSessionStore) SaveGroupEpoch(ctx context.Context, groupID string, epoch int64, secret []byte) error {
	query := `
		INSERT INTO group_epochs (group_id, epoch, group_secret)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`
	_, err := s.db.Exec(ctx, query, groupID, epoch, secret)
	return err
}

// GetGroupEpoch retrieves group secret for an epoch
func (s *MLSSessionStore) GetGroupEpoch(ctx context.Context, groupID string, epoch int64) ([]byte, error) {
	query := `
		SELECT group_secret FROM group_epochs
		WHERE group_id = $1 AND epoch = $2
	`
	var secret []byte
	err := s.db.QueryRow(ctx, query, groupID, epoch).Scan(&secret)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, fmt.Errorf("epoch not found: %d", epoch)
		}
		return nil, err
	}
	return secret, nil
}

// SaveGroupSession stores per-device group session for an epoch
func (s *MLSSessionStore) SaveGroupSession(ctx context.Context, groupID, userID, deviceID string, epoch int64, sessionBytes []byte) error {
	query := `
		INSERT INTO group_sessions (group_id, user_id, device_id, epoch, session_key_bytes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING
	`
	_, err := s.db.Exec(ctx, query, groupID, userID, deviceID, epoch, sessionBytes)
	return err
}

// GetGroupSession retrieves per-device group session
func (s *MLSSessionStore) GetGroupSession(ctx context.Context, groupID, userID, deviceID string) ([]byte, int64, error) {
	query := `
		SELECT session_key_bytes, epoch
		FROM group_sessions
		WHERE group_id = $1 AND user_id = $2 AND device_id = $3
		ORDER BY epoch DESC
		LIMIT 1
	`
	var sessionBytes []byte
	var epoch int64
	err := s.db.QueryRow(ctx, query, groupID, userID, deviceID).Scan(&sessionBytes, &epoch)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, 0, fmt.Errorf("group session not found")
		}
		return nil, 0, err
	}
	return sessionBytes, epoch, nil
}

// RecordMembershipChange logs when members are added/removed
func (s *MLSSessionStore) RecordMembershipChange(ctx context.Context, groupID, initiatorID, targetID string, changeType string, epoch int64) error {
	query := `
		INSERT INTO group_membership_changes (group_id, initiator_user_id, target_user_id, change_type, epoch)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := s.db.Exec(ctx, query, groupID, initiatorID, targetID, changeType, epoch)
	return err
}
