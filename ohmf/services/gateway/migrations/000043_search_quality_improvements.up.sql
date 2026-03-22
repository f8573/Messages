-- Enable extensions for search enhancements
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Add new columns for enhanced search to messages table
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS search_vector_en tsvector,
  ADD COLUMN IF NOT EXISTS search_text_normalized text,
  ADD COLUMN IF NOT EXISTS search_rank_base numeric(10,2) DEFAULT 1.0;

-- Create GIN index for English full-text search with stemming
CREATE INDEX IF NOT EXISTS idx_messages_search_vector_en
  ON messages
  USING GIN (search_vector_en);

-- Create GIN trigram index for fuzzy matching and typo tolerance
CREATE INDEX IF NOT EXISTS idx_messages_search_trigram
  ON messages
  USING GIN (search_text_normalized gin_trgm_ops);

-- Populate new columns with existing data
UPDATE messages
SET
  search_vector_en = to_tsvector(
    'english',
    trim(
      both ' '
      FROM COALESCE(content->>'text', '') || ' ' || COALESCE(content->>'attachment_id', '')
    )
  ),
  search_text_normalized = unaccent(lower(
    trim(
      both ' '
      FROM COALESCE(content->>'text', '')
    )
  ))
WHERE search_vector_en IS NULL OR search_text_normalized IS NULL;

-- Update trigger function to populate all search vectors
CREATE OR REPLACE FUNCTION update_messages_search_vector() RETURNS trigger AS $$
BEGIN
  -- Keep existing simple tokenizer for backward compatibility
  NEW.search_vector := to_tsvector(
    'simple',
    trim(
      both ' '
      FROM COALESCE(NEW.content->>'text', '') || ' ' || COALESCE(NEW.content->>'attachment_id', '')
    )
  );

  -- Add English tsvector with stemming
  NEW.search_vector_en := to_tsvector(
    'english',
    trim(
      both ' '
      FROM COALESCE(NEW.content->>'text', '') || ' ' || COALESCE(NEW.content->>'attachment_id', '')
    )
  );

  -- Add normalized text for trigram/fuzzy search
  NEW.search_text_normalized := unaccent(lower(
    trim(
      both ' '
      FROM COALESCE(NEW.content->>'text', '')
    )
  ));

  RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Recreate trigger to include UPDATE of content
DROP TRIGGER IF EXISTS trg_messages_search_vector ON messages;

CREATE TRIGGER trg_messages_search_vector
BEFORE INSERT OR UPDATE OF content
ON messages
FOR EACH ROW
EXECUTE FUNCTION update_messages_search_vector();

-- Create search analytics table for monitoring search quality
CREATE TABLE IF NOT EXISTS search_analytics (
  id BIGSERIAL PRIMARY KEY,
  actor_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
  conversation_id UUID NOT NULL,
  query_text TEXT NOT NULL,
  query_length INT,
  result_count INT,
  result_rank_variance numeric(10,4),
  execution_time_ms INT,
  matched_strategy TEXT,
  click_position INT,
  click_timestamp TIMESTAMPTZ,
  user_feedback TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create indices for search analytics
CREATE INDEX IF NOT EXISTS idx_search_analytics_conversation_created
  ON search_analytics (conversation_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_search_analytics_query_feedback
  ON search_analytics (query_text, user_feedback)
  WHERE user_feedback IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_search_analytics_actor_created
  ON search_analytics (actor_user_id, created_at DESC)
  WHERE actor_user_id IS NOT NULL;
