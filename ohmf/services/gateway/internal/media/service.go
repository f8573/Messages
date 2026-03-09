package media

import (
    "context"
    "fmt"
    "time"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/google/uuid"
)

// Service is a lightweight media coordinator responsible for attachment metadata
// and instructing object storage deletions. In this reference implementation
// it only manages DB records; object deletion is expected to be handled by an
// external media worker triggered by envelopes.
type Service struct{
    db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service { return &Service{db: db} }

// RegisterAttachment inserts attachment metadata prior to upload completion.
func (s *Service) RegisterAttachment(ctx context.Context, attachmentID, messageID, objectKey, mime string, size int64) error {
    _, err := s.db.Exec(ctx, `
        INSERT INTO attachments (attachment_id, message_id, object_key, mime_type, size_bytes, created_at)
        VALUES ($1::uuid, $2::uuid, $3, $4, $5, now())
    `, attachmentID, messageID, objectKey, mime, size)
    return err
}

// PurgeAttachment marks an attachment as deleted (soft-delete) and returns the object key.
func (s *Service) PurgeAttachment(ctx context.Context, attachmentID string) (string, error) {
    var objectKey string
    err := s.db.QueryRow(ctx, `SELECT object_key FROM attachments WHERE attachment_id = $1`, attachmentID).Scan(&objectKey)
    if err != nil {
        return "", err
    }
    _, err = s.db.Exec(ctx, `UPDATE attachments SET deleted_at = now() WHERE attachment_id = $1`, attachmentID)
    if err != nil {
        return "", err
    }
    // notify external deletion asynchronously
    go s.notifyObjectDeletion(objectKey)
    return objectKey, nil
}

// CreateUploadToken reserves an upload token for an attachment and returns
// the token and a presigned upload URL (placeholder in this reference).
func (s *Service) CreateUploadToken(ctx context.Context, attachmentID, mime string, size int64, ttl time.Duration) (string, string, error) {
    token := uuid.New().String()
    expires := time.Now().Add(ttl)
    _, err := s.db.Exec(ctx, `
        INSERT INTO upload_tokens (token, attachment_id, mime_type, size_bytes, expires_at, created_at)
        VALUES ($1::uuid, $2::uuid, $3, $4, $5, now())
    `, token, attachmentID, mime, size, expires)
    if err != nil {
        return "", "", err
    }
    // In production this would be a presigned S3/GCS URL. Here we return a
    // placeholder URL that a worker or client can interpret.
    uploadURL := fmt.Sprintf("https://stub-upload.local/%s?token=%s", attachmentID, token)
    return token, uploadURL, nil
}

// CompleteUpload marks the upload token as completed and attaches the
// object_key to the attachments row if provided.
func (s *Service) CompleteUpload(ctx context.Context, token, objectKey string) (string, error) {
    var attachmentID string
    err := s.db.QueryRow(ctx, `SELECT attachment_id FROM upload_tokens WHERE token = $1::uuid AND completed_at IS NULL AND expires_at > now()`, token).Scan(&attachmentID)
    if err != nil {
        return "", err
    }
    // mark token completed
    if _, err := s.db.Exec(ctx, `UPDATE upload_tokens SET completed_at = now() WHERE token = $1::uuid`, token); err != nil {
        return "", err
    }
    // update attachments object_key if provided
    if objectKey != "" {
        if _, err := s.db.Exec(ctx, `UPDATE attachments SET object_key = $1 WHERE attachment_id = $2::uuid`, objectKey, attachmentID); err != nil {
            return "", err
        }
    }
    return attachmentID, nil
}

// SweepOldRedacted deletes attachment rows that were redacted or deleted older than retentionDays.
func (s *Service) SweepOldRedacted(ctx context.Context, retentionDays int) (int64, error) {
    res, err := s.db.Exec(ctx, `DELETE FROM attachments WHERE (deleted_at IS NOT NULL OR redacted_at IS NOT NULL) AND COALESCE(deleted_at, redacted_at) < now() - ($1::int || ' days')::interval`, retentionDays)
    if err != nil {
        return 0, err
    }
    return res.RowsAffected(), nil
}

// Helper: notify external media buckets (not implemented)
func (s *Service) notifyObjectDeletion(objectKey string) {
    // placeholder: enqueue deletion to object storage system
    fmt.Println("Purge object:", objectKey, "at", time.Now().UTC())
}
