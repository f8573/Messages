-- Drop indexes
DROP INDEX IF EXISTS idx_conversations_mls_encrypted;
DROP INDEX IF EXISTS idx_group_membership_changes_user;
DROP INDEX IF EXISTS idx_group_membership_changes_group;
DROP INDEX IF EXISTS idx_group_sessions_latest;
DROP INDEX IF EXISTS idx_group_member_tree_leaves_device;
DROP INDEX IF EXISTS idx_group_member_tree_leaves_group;
DROP INDEX IF EXISTS idx_group_ratchet_trees_latest;

-- Remove columns from conversations
ALTER TABLE conversations
  DROP COLUMN IF EXISTS mls_epoch,
  DROP COLUMN IF EXISTS is_mls_encrypted;

-- Drop tables
DROP TABLE IF EXISTS group_membership_changes;
DROP TABLE IF EXISTS group_epochs;
DROP TABLE IF EXISTS group_sessions;
DROP TABLE IF EXISTS group_member_tree_leaves;
DROP TABLE IF EXISTS group_ratchet_trees;
