# Message Search Implementation Guide

## Overview

This document provides a comprehensive guide to the enhanced message search implementation for the backend. The search system has been significantly improved to provide better text search quality, ranking, and relevance while maintaining backward compatibility.

## Architecture

### Components

1. **Search Utilities** (`search.go`)
   - Query normalization and validation
   - Stopword detection
   - Phrase extraction
   - Unicode normalization

2. **Search Service** (`service.go`)
   - `SearchMessages()` - Main search method with multi-factor ranking
   - `buildSearchCondition()` - Constructs WHERE clause for different search modes
   - `buildOrderBy()` - Multi-factor ranking algorithm
   - `SearchOptions` - Configuration for search behavior

3. **API Handler** (`handler.go`)
   - `Search()` - HTTP endpoint handler
   - Request validation
   - Response formatting with search metadata

4. **Database Layer** (Migration 000043)
   - New columns: `search_vector_en`, `search_text_normalized`, `search_rank_base`
   - New indices: `idx_messages_search_vector_en`, `idx_messages_search_trigram`
   - Updated trigger: `update_messages_search_vector()`
   - Analytics table: `search_analytics`

## API Specification

### Endpoint
```
GET /v1/conversations/{id}/search
```

### Query Parameters

**Required:**
- `q` (string): Search query, minimum 2 characters

**Optional:**
- `limit` (integer 1-100): Results limit (default: 50)
- `sender_user_id` (string): Filter by sender user ID
- `content_type` (string): Filter by message content type
- `after` (RFC3339): Results after this timestamp
- `before` (RFC3339): Results before this timestamp
- `search_mode` (string): Search strategy
  - `standard` (default): FTS with stemming + ILIKE fallback + accent-insensitive
  - `fuzzy`: Trigram similarity for typo tolerance
  - `exact`: Only exact phrase matches
- `match_type` (string): Token matching strategy
  - `any` (default): Match any token (OR logic)
  - `all`: Match all tokens (AND logic)
- `sort_by` (string): Sorting strategy
  - `relevance` (default): Multi-factor relevance ranking
  - `recency`: Sort by newest first
- `exact_match` (boolean): Require exact phrase match
- `include_edits` (boolean): Include message edit history (future feature)

### Response

```json
{
  "items": [
    {
      "message_id": "uuid",
      "conversation_id": "uuid",
      "sender_user_id": "uuid",
      "content_type": "text",
      "content": { "text": "message content" },
      "created_at": "2026-03-21T12:34:56Z",
      ...
    }
  ],
  "search_metrics": {
    "query_normalized": "normalized search term",
    "search_mode": "standard",
    "match_type": "any",
    "result_count": 5
  }
}
```

## Search Modes

### Standard Mode (Default)

Uses multiple search strategies for comprehensive coverage:

1. **Full-Text Search (FTS)** - PostgreSQL `websearch_to_tsquery`
   - Boolean operators supported (AND, OR, NOT)
   - Efficient with GIN indices
   - Prefix/infix word matching

2. **English Stemming** - `plainto_tsquery('english')`
   - "running" matches "run"
   - Removes common English suffixes
   - Indexed with `search_vector_en` column

3. **ILIKE Fallback** - Pattern matching
   - Case-insensitive
   - Accent-insensitive via `unaccent()`
   - Substring matching for complex terms

4. **Attachment Search** - Metadata matching
   - Match by attachment ID
   - Future: filename search

### Fuzzy Mode

For typo-tolerant searching using trigram similarity:

- Handles transposed characters ("recieve" → "receive")
- Handles missing/extra characters
- Uses PostgreSQL `similarity()` function
- Slower than standard mode, recommended for <= 1000 results

### Exact Mode

Only matches exact phrases:

- Case-insensitive
- Accent-insensitive
- No substring matching
- Fastest for literal phrase searches

## Ranking Algorithm

The ranking system uses six weighted factors in priority order:

### 1. Full-Text Search Rank (Primary Factor)
```
Formula: ts_rank_cd(search_vector_en, query) × search_rank_base
Weight: Highest
Range: 0.0 - 1.0 (with base up to 1.0)
```
- Based on English stemming tokenization
- Considers term frequency and document frequency
- Multiplied by message-level ranking factor

### 2. Exact/Prefix/Infix Matching
```
Range: 0 - 3 points
Exact match (complete text = query): 3 points
Prefix match (text starts with query): 2.5 points
Infix match (text contains query): 2 points
```
- Differentiates meaningful matches
- Exact > Prefix > Substring

### 3. English Stemming Confirmation
```
Range: 0 - 1 point
Boolean boost when English FTS matches
```
- Secondary confirmation with stemming
- Identifies language-aware matches

### 4. Typo Tolerance Score
```
Formula: similarity(normalized_text, normalized_query)
Range: 0.0 - 1.0
Trigram similarity for near-matches
```
- Handles minor typos and variations
- Only applied in fuzzy mode or as tiebreaker

### 5. Recency
```
Formula: m.created_at DESC
Tiebreaker when other factors equal
```
- Recent messages ranked higher
- Within similar relevance, newer comes first

### 6. Server Order
```
Final tiebreaker: m.server_order DESC
```
- Consistent ordering within same timestamp

## Query Normalization

The `NormalizeQuery()` function preprocesses input for better matching:

### Processing Steps

1. **Whitespace Normalization**
   - Trim leading/trailing spaces
   - Convert multiple spaces to single space

2. **Case Normalization**
   - Convert to lowercase for uniform matching

3. **Quote Extraction**
   - Extract quoted phrases for exact matching
   - Remove quotes from general token processing

4. **Token Splitting**
   - Split into individual tokens
   - Preserve tokens > 0 characters

5. **Operator Detection**
   - Identify AND, OR, NOT operators
   - Mark presence of boolean operators

6. **Stopword Detection**
   - Identify if all tokens are common stopwords
   - Flag for warning/handling

### Example Transformations

- `"  Hello World  "` → tokens: `["hello", "world"]`
- `'find "exact phrase"'` → tokens: `["find"]`, phrases: `["exact phrase"]`
- `"hello AND world"` → tokens: `["hello", "world"]`, operators: `["AND"]`
- `"café"` → normalized: `"cafe"` (accent-insensitive)

## Validation

The `ValidateSearchQuality()` function validates queries:

### Valid Queries
- Minimum 2 characters in query string
- At least one non-stopword token
- Maximum 500 characters

### Invalid Scenarios (Rejected)
- Empty queries
- Only stopwords (without quotes)
- Excessively long queries (> 500 chars)
- All operators, no keywords

### Examples
- ✅ `hello world` - valid
- ✅ `"a"` - valid (exact phrase)
- ❌ `the and or` - invalid (only stopwords)
- ❌ `a` - invalid (single char without quotes)
- ❌ `AND OR NOT` - invalid (only operators)

## Database Changes

### Migration: 000043_search_quality_improvements

#### Extensions Added
```sql
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

#### New Columns on `messages` Table
```sql
- search_vector_en tsvector
  Purpose: English FTS with stemming
  Indexed: GIN (idx_messages_search_vector_en)

- search_text_normalized text
  Purpose: Normalized text for trigram matching
  Value: unaccent(lower(content->>'text'))
  Indexed: GIN with trigram ops (idx_messages_search_trigram)

- search_rank_base numeric(10,2) DEFAULT 1.0
  Purpose: Message-level ranking factor
  Future: Boost important messages/senders
```

#### New Indices
```sql
CREATE INDEX idx_messages_search_vector_en
  ON messages USING GIN (search_vector_en);

CREATE INDEX idx_messages_search_trigram
  ON messages USING GIN (search_text_normalized gin_trgm_ops);
```

#### Updated Trigger
```sql
CREATE TRIGGER trg_messages_search_vector
BEFORE INSERT OR UPDATE OF content ON messages
FOR EACH ROW EXECUTE FUNCTION update_messages_search_vector();
```

The trigger now populates:
- `search_vector` (simple - backward compatibility)
- `search_vector_en` (English stemming - NEW)
- `search_text_normalized` (typo tolerance - NEW)

#### Analytics Table
```sql
CREATE TABLE search_analytics (
  id BIGSERIAL PRIMARY KEY,
  actor_user_id UUID,
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
  created_at TIMESTAMPTZ DEFAULT now()
);
```

Purpose: Monitor search quality, user behavior, and ranking effectiveness.

## Testing

### Unit Tests

**File:** `search_test.go`

- Query normalization (unicode, case, spaces, quotes)
- Stopword detection
- Phrase extraction
- Operator detection
- Query validation
- Token utilities (first token, joining, etc.)
- Typo detection heuristics

**File:** `search_integration_test.go`

- Basic text search scenarios
- Ranking algorithm validation (exact > prefix > infix)
- Search mode behavior (standard, fuzzy, exact)
- Filtering (sender, content_type, date range)
- Edge cases (empty, special chars, length)
- Performance benchmarks
- Database migration verification

### Running Tests

```bash
# Unit tests
go test -v ./internal/messages -run TestNormalize
go test -v ./internal/messages -run TestValidate

# Search-specific tests
go test -v ./internal/messages -run TestSearch

# Benchmarks
go test -bench=BenchmarkNormalize ./internal/messages
go test -bench=BenchmarkValidate ./internal/messages

# All tests
go test -v ./internal/messages
```

## Performance Considerations

### Index Strategy

**Compound Indices for High Performance:**

1. **Full-Text Search Index** (GIN on `search_vector_en`)
   - Fast for most queries
   - Handles stemming efficiently
   - Typical query: < 50ms for 100 results

2. **Trigram Index** (GIN on `search_text_normalized`)
   - Used for fuzzy matching
   - Good for typo tolerance
   - Slower than FTS, use when needed

3. **Existing Conversation Index**
   - `(conversation_id, created_at DESC)` - already exists
   - Filters by conversation quickly
   - Provides natural ordering

### Query Optimization Tips

- Use `search_mode=standard` for most queries (default)
- Use `search_mode=fuzzy` only when standard mode has no results
- Add filters early: sender, content_type, date range reduce dataset
- Limit results to needed amount (default 50 is usually good)
- Avoid very broad queries (2-4 word queries are optimal)

### Expected Performance

| Scenario | Time | Notes |
|----------|------|-------|
| Typical text search | < 50ms | 50-100 results, standard mode |
| Complex multi-word | 50-100ms | More terms increase processing |
| Fuzzy search | 100-200ms | Trigram similarity is slower |
| Large result set | 100-300ms | 500+ results with aggregations |
| Optimized narrow | < 25ms | With filters, small dataset |

## Backward Compatibility

All changes are fully backward compatible:

- **API**: New query parameters are optional
- **Default behavior**: Unchanged when new parameters omitted
- **Database**: New columns are optional (NULL allowed), existing columns unchanged
- **Existing responses**: New `search_metrics` field is additive
- **No breaking changes**: Existing clients work without modification

### Migration Path

1. Deploy migration 000043 to staging
2. Test search functionality
3. Deploy to production during low-traffic period
4. Migration populates new columns automatically
5. No downtime required

## Deployment Checklist

- [ ] Review migration 000043 SQL syntax
- [ ] Test migration in staging environment
- [ ] Verify indices created successfully
- [ ] Check existing message search vectors populated
- [ ] Test API with new query parameters
- [ ] Verify backward compatibility
- [ ] Load test search Performance
- [ ] Monitor search_analytics table for data
- [ ] Verify ranking quality meets expectations
- [ ] Document known limitations
- [ ] Plan gradual rollout if needed

## Known Limitations & Future Enhancements

### Current Limitations
- Minimum 2-character queries (FTS efficiency)
- Single search strategy per query (can't combine modes)
- No user-specific ranking personalization
- Edit history not indexed (can be added)
- No suggestion/autocomplete feature

### Future Enhancements (Out of Scope)
- Advanced boolean operator exposure to clients
- Query expansion with synonyms dictionary
- Machine learning-based ranking personalization
- Multi-language detection and stemming
- Search analytics dashboard
- Reaction emoji search
- Saved search functionality
- Search suggestions/autocomplete

## Troubleshooting

### Empty Search Results

1. **Check query length**: Min 2 characters
2. **Check for all-stopword queries**: "the and or" returns nothing
3. **Verify message exists**: Deleted/expired messages excluded
4. **Check permissions**: User must be conversation member
5. **Try different search mode**: Use `search_mode=fuzzy` for typo tolerance

### Slow Search Performance

1. **Reduce result limit**: Fewer results = faster queries
2. **Add filters**: sender_user_id or date range narrows dataset
3. **Use shorter queries**: 2-4 word queries typically fastest
4. **Check index status**: Verify `idx_messages_search_vector_en` exists
5. **Monitor database load**: High server load slows queries

### Unexpected Ranking

1. **Verify search_rank_base populated**: Check if migration ran successfully
2. **Review ranking formula**: Multi-factor ranking considers several factors
3. **Check normalization**: Accents, case, spaces handled by system
4. **Look at debug info**: `search_metrics` in response shows strategy used

## Monitoring

### Key Metrics to Track

```
- Average query execution time
- P95/P99 query execution time
- Zero-result query rate (should be < 10%)
- Search result click-through position
- User satisfaction (optional feedback field)
```

### Alerting Rules

```
- Query execution time > 200ms
- Zero-result rate > 15%
- Index bloat detected
- Search errors > 0.1%
```

### Analytics Query

```sql
SELECT
  query_text,
  COUNT(*) as search_count,
  AVG(result_count) as avg_results,
  AVG(execution_time_ms) as avg_time_ms,
  COUNT(*) FILTER (WHERE user_feedback = 'relevant') as relevant_count
FROM search_analytics
WHERE created_at > now() - INTERVAL '7 days'
GROUP BY query_text
ORDER BY search_count DESC
LIMIT 20;
```

## Support & Contact

For questions or issues related to message search:
1. Check this documentation
2. Review integration tests for examples
3. Check error messages in search_analytics table
4. Contact backend team

---

## Files Modified/Created

| File | Type | Purpose |
|------|------|---------|
| `migrations/000043_search_quality_improvements.up.sql` | Database | Schema enhancements |
| `migrations/000043_search_quality_improvements.down.sql` | Database | Migration rollback |
| `internal/messages/search.go` | Code | Query normalization utilities |
| `internal/messages/service.go` | Code | Enhanced SearchMessages method |
| `internal/messages/handler.go` | Code | Updated API handler |
| `internal/messages/search_test.go` | Tests | Unit tests for utilities |
| `internal/messages/search_integration_test.go` | Tests | Integration test framework |

---

**Version:** 1.0
**Last Updated:** 2026-03-21
**Status:** READY FOR IMPLEMENTATION
