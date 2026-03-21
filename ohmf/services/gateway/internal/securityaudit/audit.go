package securityaudit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Append(ctx context.Context, exec executor, actorUserID, targetUserID, eventType string, payload any) error {
	if exec == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = exec.Exec(ctx, `
		WITH prev AS (
			SELECT event_hash
			FROM security_audit_events
			WHERE target_user_id = NULLIF($2, '')::uuid
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		)
		INSERT INTO security_audit_events (
			actor_user_id,
			target_user_id,
			event_type,
			payload,
			prev_event_hash,
			event_hash
		)
		VALUES (
			NULLIF($1, '')::uuid,
			NULLIF($2, '')::uuid,
			$3,
			$4::jsonb,
			NULLIF((SELECT event_hash FROM prev), ''),
			encode(
				digest(
					COALESCE(NULLIF($1, ''), '') || '|' ||
					COALESCE(NULLIF($2, ''), '') || '|' ||
					$3 || '|' ||
					COALESCE((SELECT event_hash FROM prev), '') || '|' ||
					$4::text,
					'sha256'
				),
				'hex'
			)
		)
	`, actorUserID, targetUserID, eventType, string(raw))
	return err
}
