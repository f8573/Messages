-- Drop search analytics table
DROP TABLE IF EXISTS search_analytics;

-- Restore original trigger function (without new columns)
CREATE OR REPLACE FUNCTION update_messages_search_vector() RETURNS trigger AS $$
BEGIN
  NEW.search_vector := to_tsvector(
    'simple',
    trim(
      both ' '
      FROM COALESCE(NEW.content->>'text', '') || ' ' || COALESCE(NEW.content->>'attachment_id', '')
    )
  );
  RETURN NEW;
END
$$ LANGUAGE plpgsql;

-- Recreate trigger without the new column updates
DROP TRIGGER IF EXISTS trg_messages_search_vector ON messages;

CREATE TRIGGER trg_messages_search_vector
BEFORE INSERT OR UPDATE OF content
ON messages
FOR EACH ROW
EXECUTE FUNCTION update_messages_search_vector();

-- Drop new indices
DROP INDEX IF EXISTS idx_messages_search_trigram;
DROP INDEX IF EXISTS idx_messages_search_vector_en;

-- Remove new columns
ALTER TABLE messages
  DROP COLUMN IF EXISTS search_rank_base,
  DROP COLUMN IF EXISTS search_text_normalized,
  DROP COLUMN IF EXISTS search_vector_en;

-- Leave extensions enabled (they don't hurt)
-- If you want to remove them, uncomment:
-- DROP EXTENSION IF EXISTS pg_trgm;
-- DROP EXTENSION IF EXISTS unaccent;
