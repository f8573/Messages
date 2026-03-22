-- Create table for Material Layer Security (MLS) ratchet trees (one per group)
CREATE TABLE IF NOT EXISTS group_ratchet_trees (
    group_id UUID PRIMARY KEY,
    generation INT NOT NULL DEFAULT 0,         -- Bumped on member add/remove
    tree_bytes BYTEA NOT NULL,                 -- Serialized binary tree structure
    epoch INT NOT NULL DEFAULT 0,              -- MLS epoch for forward secrecy
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE
);

-- Maps group members to tree leaf positions
CREATE TABLE IF NOT EXISTS group_member_tree_leaves (
    group_id UUID NOT NULL,
    user_id UUID NOT NULL,
    device_id UUID NOT NULL,
    leaf_index INT NOT NULL,                   -- Position in binary tree
    generation INT NOT NULL,                   -- Which generation assigned this leaf
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id, device_id),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

-- Per-group per-device session (MLS group state)
CREATE TABLE IF NOT EXISTS group_sessions (
    group_id UUID NOT NULL,
    user_id UUID NOT NULL,
    device_id UUID NOT NULL,
    epoch INT NOT NULL,
    session_key_bytes BYTEA NOT NULL,         -- Group session state
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (group_id, user_id, device_id, epoch),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (device_id) REFERENCES devices(id) ON DELETE CASCADE
);

-- Group secrets per epoch (for key derivation)
CREATE TABLE IF NOT EXISTS group_epochs (
    group_id UUID NOT NULL,
    epoch INT NOT NULL,
    group_secret BYTEA NOT NULL,               -- KDF'd to derive epoch key
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (group_id, epoch),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE
);

-- Track group member addition/removal events for key rotation
CREATE TABLE IF NOT EXISTS group_membership_changes (
    id BIGSERIAL PRIMARY KEY,
    group_id UUID NOT NULL,
    initiator_user_id UUID NOT NULL,
    target_user_id UUID NOT NULL,
    target_device_id UUID,
    change_type TEXT NOT NULL,                 -- MEMBER_ADDED, MEMBER_REMOVED
    epoch INT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    FOREIGN KEY (group_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (initiator_user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create indexes for common queries
CREATE INDEX IF NOT EXISTS idx_group_ratchet_trees_latest
    ON group_ratchet_trees(group_id, generation DESC);

CREATE INDEX IF NOT EXISTS idx_group_member_tree_leaves_group
    ON group_member_tree_leaves(group_id, generation DESC);

CREATE INDEX IF NOT EXISTS idx_group_member_tree_leaves_device
    ON group_member_tree_leaves(group_id, user_id, device_id);

CREATE INDEX IF NOT EXISTS idx_group_sessions_latest
    ON group_sessions(group_id, epoch DESC);

CREATE INDEX IF NOT EXISTS idx_group_membership_changes_group
    ON group_membership_changes(group_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_group_membership_changes_user
    ON group_membership_changes(target_user_id, created_at DESC);

-- Update conversations table to track group encryption state
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS is_mls_encrypted BOOLEAN DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS mls_epoch INT DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_conversations_mls_encrypted
  ON conversations(id, is_mls_encrypted)
  WHERE is_mls_encrypted = TRUE;
