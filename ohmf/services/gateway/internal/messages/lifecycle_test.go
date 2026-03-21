package messages

import (
	"context"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock/v4"
)

func TestValidateSendContentAcceptsStructuredTextPayload(t *testing.T) {
	err := validateSendContent("text", map[string]any{
		"text": "hello world",
		"spans": []any{
			map[string]any{"start": int64(0), "end": int64(5), "style": "bold"},
		},
		"mentions": []any{
			map[string]any{"user_id": "user-2", "start": int64(6), "end": int64(11), "display": "world"},
		},
		"expires_on_read": true,
	})
	if err != nil {
		t.Fatalf("expected structured text payload to validate, got %v", err)
	}
}

func TestResolveMessageExpirationUsesConversationRetention(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	mock.ExpectQuery(`SELECT COALESCE\(retention_seconds, 0\), expires_at FROM conversations WHERE id = \$1::uuid`).
		WithArgs("conversation-1").
		WillReturnRows(pgxmock.NewRows([]string{"retention_seconds", "expires_at"}).AddRow(int64(60), nil))

	expiry, err := resolveMessageExpiration(context.Background(), mock, "conversation-1", "text", map[string]any{
		"text": "hello world",
	})
	if err != nil {
		t.Fatalf("resolveMessageExpiration failed: %v", err)
	}
	if !expiry.Valid {
		t.Fatal("expected expiry to be set from retention policy")
	}
	delta := time.Until(expiry.Time)
	if delta < 50*time.Second || delta > 70*time.Second {
		t.Fatalf("expected expiry about one minute out, got %s", delta)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListMasksExpiredMessages(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := &Service{db: mock}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	createdAt := expiredAt.Add(-time.Minute)

	mock.ExpectQuery(`SELECT 1 FROM conversation_members WHERE conversation_id = \$1::uuid AND user_id = \$2::uuid`).
		WithArgs("conversation-1", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{"one"}).AddRow(1))
	mock.ExpectQuery(`SELECT\s+m.id::text,\s*m.conversation_id::text,\s*m.sender_user_id::text`).
		WithArgs("conversation-1", "user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id",
			"conversation_id",
			"sender_user_id",
			"sender_device_id",
			"reply_to_message_id",
			"content_type",
			"content",
			"client_generated_id",
			"transport",
			"server_order",
			"delivery_status",
			"created_at",
			"delivered_at",
			"read_at",
			"edited_at",
			"deleted_at",
			"expires_at",
			"visibility_state",
			"reply_count",
			"reactions",
		}).AddRow(
			"message-1",
			"conversation-1",
			"user-2",
			"device-2",
			"",
			"text",
			[]byte(`{"text":"hello"}`),
			"client-1",
			"OTT",
			int64(9),
			"SENT",
			createdAt,
			nil,
			nil,
			nil,
			nil,
			expiredAt,
			"",
			int64(0),
			[]byte(`{}`),
		))

	items, err := svc.List(context.Background(), "user-1", "conversation-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if !items[0].Deleted {
		t.Fatal("expected expired message to be marked deleted")
	}
	if items[0].VisibilityState != "EXPIRED" {
		t.Fatalf("expected EXPIRED visibility state, got %q", items[0].VisibilityState)
	}
	if items[0].ExpiresAt == "" {
		t.Fatal("expected expires_at on expired message")
	}
	if len(items[0].Content) != 0 {
		t.Fatalf("expected expired message content to be cleared, got %#v", items[0].Content)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
