package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/config"
)

var (
	ErrUploadTokenNotFound = errors.New("upload_token_not_found")
	ErrAttachmentNotFound  = errors.New("attachment_not_found")
	ErrAttachmentForbidden = errors.New("attachment_forbidden")
	ErrChecksumMismatch    = errors.New("attachment_checksum_mismatch")
	ErrUploadIncomplete    = errors.New("attachment_upload_incomplete")
)

type UploadInit struct {
	UploadID     string            `json:"upload_id"`
	AttachmentID string            `json:"attachment_id"`
	Token        string            `json:"token"`
	UploadURL    string            `json:"upload_url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers,omitempty"`
	ObjectKey    string            `json:"object_key"`
	ExpiresAt    string            `json:"expires_at"`
}

type Download struct {
	AttachmentID string `json:"attachment_id"`
	DownloadURL  string `json:"download_url"`
	ExpiresAt    string `json:"expires_at"`
	FileName     string `json:"file_name,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
}

type ObjectInfo struct {
	SizeBytes      int64
	SHA256Hex      string
	LastModifiedAt time.Time
}

type Store interface {
	PutObject(ctx context.Context, objectKey string, body io.Reader) (ObjectInfo, error)
	OpenObject(ctx context.Context, objectKey string) (io.ReadCloser, ObjectInfo, error)
	StatObject(ctx context.Context, objectKey string) (ObjectInfo, error)
	DeleteObject(ctx context.Context, objectKey string) error
}

type LocalStore struct {
	root string
}

func NewLocalStore(root string) *LocalStore { return &LocalStore{root: root} }

func (s *LocalStore) fullPath(objectKey string) string {
	clean := filepath.Clean(strings.TrimPrefix(objectKey, "/"))
	return filepath.Join(s.root, clean)
}

func (s *LocalStore) PutObject(ctx context.Context, objectKey string, body io.Reader) (ObjectInfo, error) {
	path := s.fullPath(objectKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	file, err := os.Create(path)
	if err != nil {
		return ObjectInfo{}, err
	}
	defer file.Close()

	hash := sha256.New()
	written, err := io.Copy(file, io.TeeReader(body, hash))
	if err != nil {
		return ObjectInfo{}, err
	}
	info, err := file.Stat()
	if err != nil {
		return ObjectInfo{}, err
	}
	select {
	case <-ctx.Done():
		return ObjectInfo{}, ctx.Err()
	default:
	}
	return ObjectInfo{
		SizeBytes:      written,
		SHA256Hex:      hex.EncodeToString(hash.Sum(nil)),
		LastModifiedAt: info.ModTime().UTC(),
	}, nil
}

func (s *LocalStore) OpenObject(_ context.Context, objectKey string) (io.ReadCloser, ObjectInfo, error) {
	path := s.fullPath(objectKey)
	file, err := os.Open(path)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ObjectInfo{}, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		file.Close()
		return nil, ObjectInfo{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, ObjectInfo{}, err
	}
	return file, ObjectInfo{
		SizeBytes:      info.Size(),
		SHA256Hex:      hex.EncodeToString(hash.Sum(nil)),
		LastModifiedAt: info.ModTime().UTC(),
	}, nil
}

func (s *LocalStore) StatObject(ctx context.Context, objectKey string) (ObjectInfo, error) {
	reader, info, err := s.OpenObject(ctx, objectKey)
	if err != nil {
		return ObjectInfo{}, err
	}
	_ = reader.Close()
	return info, nil
}

func (s *LocalStore) DeleteObject(_ context.Context, objectKey string) error {
	if objectKey == "" {
		return nil
	}
	if err := os.Remove(s.fullPath(objectKey)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type Service struct {
	db         *pgxpool.Pool
	store      Store
	publicBase string
	signingKey []byte
}

func NewService(db *pgxpool.Pool, cfg config.Config) *Service {
	base := strings.TrimRight(cfg.MediaPublicBaseURL, "/")
	if base == "" {
		base = "http://localhost:18080"
	}
	root := cfg.MediaRootDir
	if root == "" {
		root = "var/media"
	}
	return &Service{
		db:         db,
		store:      NewLocalStore(root),
		publicBase: base,
		signingKey: []byte(cfg.JWTSecret),
	}
}

func (s *Service) AssociateAttachment(ctx context.Context, attachmentID, messageID, fileName string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE attachments
		SET message_id = $2::uuid,
		    file_name = COALESCE(NULLIF($3, ''), file_name)
		WHERE attachment_id = $1::uuid
	`, attachmentID, messageID, fileName)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAttachmentNotFound
	}
	return nil
}

func (s *Service) PurgeAttachment(ctx context.Context, attachmentID string) (string, error) {
	var objectKey string
	err := s.db.QueryRow(ctx, `SELECT object_key FROM attachments WHERE attachment_id = $1::uuid`, attachmentID).Scan(&objectKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrAttachmentNotFound
		}
		return "", err
	}
	_, err = s.db.Exec(ctx, `UPDATE attachments SET deleted_at = now(), status = 'DELETED' WHERE attachment_id = $1::uuid`, attachmentID)
	if err != nil {
		return "", err
	}
	if err := s.store.DeleteObject(ctx, objectKey); err != nil {
		return "", err
	}
	return objectKey, nil
}

func (s *Service) CreateUploadToken(ctx context.Context, attachmentID, mime, fileName, checksum string, size int64, ttl time.Duration) (UploadInit, error) {
	if attachmentID == "" {
		attachmentID = uuid.New().String()
	}
	token := uuid.New().String()
	expires := time.Now().Add(ttl).UTC()
	objectKey := fmt.Sprintf("attachments/%s/%s.bin", attachmentID, uuid.New().String())
	_, err := s.db.Exec(ctx, `
		INSERT INTO attachments (
			attachment_id,
			object_key,
			mime_type,
			size_bytes,
			file_name,
			status,
			checksum_sha256,
			created_at
		)
		VALUES ($1::uuid, $2, $3, $4, NULLIF($5, ''), 'PENDING_UPLOAD', NULLIF($6, ''), now())
		ON CONFLICT (attachment_id)
		DO UPDATE SET
			object_key = EXCLUDED.object_key,
			mime_type = EXCLUDED.mime_type,
			size_bytes = EXCLUDED.size_bytes,
			file_name = COALESCE(EXCLUDED.file_name, attachments.file_name),
			checksum_sha256 = COALESCE(EXCLUDED.checksum_sha256, attachments.checksum_sha256),
			status = 'PENDING_UPLOAD'
	`, attachmentID, objectKey, mime, size, fileName, strings.ToLower(strings.TrimSpace(checksum)))
	if err != nil {
		return UploadInit{}, err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO upload_tokens (
			token,
			attachment_id,
			mime_type,
			size_bytes,
			object_key,
			upload_method,
			expected_checksum_sha256,
			expires_at,
			created_at
		)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'PUT', NULLIF($6, ''), $7, now())
	`, token, attachmentID, mime, size, objectKey, strings.ToLower(strings.TrimSpace(checksum)), expires)
	if err != nil {
		return UploadInit{}, err
	}
	return UploadInit{
		UploadID:     token,
		AttachmentID: attachmentID,
		Token:        token,
		UploadURL:    fmt.Sprintf("%s/v1/media/uploads/%s", s.publicBase, token),
		Method:       "PUT",
		Headers: map[string]string{
			"Content-Type": mime,
		},
		ObjectKey: objectKey,
		ExpiresAt: expires.Format(time.RFC3339Nano),
	}, nil
}

func (s *Service) UploadObject(ctx context.Context, token string, body io.Reader) (UploadInit, error) {
	var attachmentID, mimeType, objectKey, expectedChecksum string
	var expectedSize int64
	var expiresAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT attachment_id::text, mime_type, COALESCE(size_bytes, 0), COALESCE(object_key, ''), COALESCE(expected_checksum_sha256, ''), expires_at
		FROM upload_tokens
		WHERE token = $1::uuid AND completed_at IS NULL AND expires_at > now()
	`, token).Scan(&attachmentID, &mimeType, &expectedSize, &objectKey, &expectedChecksum, &expiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UploadInit{}, ErrUploadTokenNotFound
		}
		return UploadInit{}, err
	}

	info, err := s.store.PutObject(ctx, objectKey, body)
	if err != nil {
		return UploadInit{}, err
	}
	if expectedSize > 0 && expectedSize != info.SizeBytes {
		return UploadInit{}, ErrUploadIncomplete
	}
	if expectedChecksum != "" && !strings.EqualFold(expectedChecksum, info.SHA256Hex) {
		return UploadInit{}, ErrChecksumMismatch
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE attachments
		SET status = 'UPLOADED',
		    checksum_sha256 = COALESCE(NULLIF($2, ''), checksum_sha256),
		    size_bytes = CASE WHEN $3::bigint > 0 THEN $3 ELSE size_bytes END
		WHERE attachment_id = $1::uuid
	`, attachmentID, info.SHA256Hex, info.SizeBytes); err != nil {
		return UploadInit{}, err
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE upload_tokens
		SET completed_object_size = $2,
		    expected_checksum_sha256 = COALESCE(NULLIF($3, ''), expected_checksum_sha256)
		WHERE token = $1::uuid
	`, token, info.SizeBytes, info.SHA256Hex); err != nil {
		return UploadInit{}, err
	}
	return UploadInit{
		UploadID:     token,
		AttachmentID: attachmentID,
		Token:        token,
		UploadURL:    fmt.Sprintf("%s/v1/media/uploads/%s", s.publicBase, token),
		Method:       "PUT",
		ObjectKey:    objectKey,
		ExpiresAt:    expiresAt.UTC().Format(time.RFC3339Nano),
		Headers:      map[string]string{"Content-Type": mimeType},
	}, nil
}

func (s *Service) CompleteUpload(ctx context.Context, token string) (string, error) {
	var attachmentID, objectKey, expectedChecksum string
	var expectedSize int64
	err := s.db.QueryRow(ctx, `
		SELECT attachment_id::text, COALESCE(size_bytes, 0), COALESCE(object_key, ''), COALESCE(expected_checksum_sha256, '')
		FROM upload_tokens
		WHERE token = $1::uuid AND completed_at IS NULL AND expires_at > now()
	`, token).Scan(&attachmentID, &expectedSize, &objectKey, &expectedChecksum)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrUploadTokenNotFound
		}
		return "", err
	}
	info, err := s.store.StatObject(ctx, objectKey)
	if err != nil {
		return "", err
	}
	if expectedSize > 0 && expectedSize != info.SizeBytes {
		return "", ErrUploadIncomplete
	}
	if expectedChecksum != "" && !strings.EqualFold(expectedChecksum, info.SHA256Hex) {
		if _, updateErr := s.db.Exec(ctx, `UPDATE attachments SET status = 'QUARANTINED' WHERE attachment_id = $1::uuid`, attachmentID); updateErr != nil {
			return "", updateErr
		}
		return "", ErrChecksumMismatch
	}
	if _, err := s.db.Exec(ctx, `UPDATE upload_tokens SET completed_at = now() WHERE token = $1::uuid`, token); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(ctx, `
		UPDATE attachments
		SET status = 'AVAILABLE',
		    upload_completed_at = now(),
		    available_at = now(),
		    checksum_sha256 = COALESCE(NULLIF($2, ''), checksum_sha256),
		    size_bytes = CASE WHEN $3::bigint > 0 THEN $3 ELSE size_bytes END
		WHERE attachment_id = $1::uuid
	`, attachmentID, info.SHA256Hex, info.SizeBytes); err != nil {
		return "", err
	}
	return attachmentID, nil
}

func (s *Service) CreateDownload(ctx context.Context, userID, attachmentID string, ttl time.Duration) (Download, error) {
	var objectKey, fileName, mimeType string
	var sizeBytes int64
	err := s.db.QueryRow(ctx, `
		SELECT a.object_key, COALESCE(a.file_name, ''), a.mime_type, COALESCE(a.size_bytes, 0)
		FROM attachments a
		JOIN messages m ON m.id = a.message_id
		JOIN conversation_members cm ON cm.conversation_id = m.conversation_id
		WHERE a.attachment_id = $1::uuid
		  AND cm.user_id = $2::uuid
		  AND a.deleted_at IS NULL
		  AND a.redacted_at IS NULL
		  AND a.status = 'AVAILABLE'
	`, attachmentID, userID).Scan(&objectKey, &fileName, &mimeType, &sizeBytes)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Download{}, ErrAttachmentForbidden
		}
		return Download{}, err
	}
	expiresAt := time.Now().Add(ttl).UTC()
	token := s.signDownloadToken(attachmentID, expiresAt)
	return Download{
		AttachmentID: attachmentID,
		DownloadURL:  fmt.Sprintf("%s/v1/media/downloads/%s", s.publicBase, token),
		ExpiresAt:    expiresAt.Format(time.RFC3339Nano),
		FileName:     fileName,
		MimeType:     mimeType,
		SizeBytes:    sizeBytes,
	}, nil
}

func (s *Service) OpenDownload(ctx context.Context, token string) (io.ReadCloser, string, string, int64, error) {
	attachmentID, expiresAt, err := s.parseDownloadToken(token)
	if err != nil {
		return nil, "", "", 0, err
	}
	if time.Now().After(expiresAt) {
		return nil, "", "", 0, ErrAttachmentForbidden
	}
	var objectKey, fileName, mimeType string
	var sizeBytes int64
	err = s.db.QueryRow(ctx, `
		SELECT object_key, COALESCE(file_name, ''), mime_type, COALESCE(size_bytes, 0)
		FROM attachments
		WHERE attachment_id = $1::uuid
		  AND deleted_at IS NULL
		  AND redacted_at IS NULL
		  AND status = 'AVAILABLE'
	`, attachmentID).Scan(&objectKey, &fileName, &mimeType, &sizeBytes)
	if err != nil {
		return nil, "", "", 0, err
	}
	reader, info, err := s.store.OpenObject(ctx, objectKey)
	if err != nil {
		return nil, "", "", 0, err
	}
	if sizeBytes == 0 {
		sizeBytes = info.SizeBytes
	}
	return reader, fileName, mimeType, sizeBytes, nil
}

func (s *Service) SweepOldRedacted(ctx context.Context, retentionDays int) (int64, error) {
	rows, err := s.db.Query(ctx, `
		SELECT COALESCE(object_key, '')
		FROM attachments
		WHERE (deleted_at IS NOT NULL OR redacted_at IS NOT NULL)
		  AND COALESCE(deleted_at, redacted_at) < now() - ($1::int || ' days')::interval
	`, retentionDays)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var objectKey string
		if err := rows.Scan(&objectKey); err != nil {
			return 0, err
		}
		_ = s.store.DeleteObject(ctx, objectKey)
	}
	res, err := s.db.Exec(ctx, `DELETE FROM attachments WHERE (deleted_at IS NOT NULL OR redacted_at IS NOT NULL) AND COALESCE(deleted_at, redacted_at) < now() - ($1::int || ' days')::interval`, retentionDays)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected(), nil
}

func (s *Service) signDownloadToken(attachmentID string, expiresAt time.Time) string {
	payload := attachmentID + "|" + strconv.FormatInt(expiresAt.Unix(), 10)
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + "|" + sig))
}

func (s *Service) parseDownloadToken(token string) (string, time.Time, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", time.Time{}, err
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		return "", time.Time{}, ErrAttachmentForbidden
	}
	payload := parts[0] + "|" + parts[1]
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	if !hmac.Equal([]byte(parts[2]), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return "", time.Time{}, ErrAttachmentForbidden
	}
	epoch, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", time.Time{}, err
	}
	return parts[0], time.Unix(epoch, 0).UTC(), nil
}

func encodeHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	b, _ := json.Marshal(headers)
	return string(b)
}
