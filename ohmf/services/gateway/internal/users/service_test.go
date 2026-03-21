package users

import (
	"context"
	"testing"
	"time"

	pgxmock "github.com/pashagolub/pgxmock/v4"
)

func TestExportAccountIncludesMessagesConversationsAndSecurityMetadata(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := &Service{db: mock}
	now := time.Date(2026, 3, 20, 13, 0, 0, 0, time.UTC)
	later := now.Add(90 * time.Minute)

	mock.ExpectQuery(`FROM users\s+WHERE id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id",
			"primary_phone_e164",
			"display_name",
			"avatar_url",
			"phone_verified_at",
			"created_at",
			"updated_at",
			"deletion_state",
			"deletion_requested_at",
			"deletion_effective_at",
			"deletion_completed_at",
			"deletion_reason",
		}).AddRow("user-1", "+15550001111", "James", "https://example.com/avatar.png", now, now, later, "ACTIVE", nil, nil, nil, ""))

	mock.ExpectQuery(`FROM devices\s+WHERE user_id = \$1::uuid\s+ORDER BY updated_at DESC, created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id",
			"platform",
			"device_name",
			"client_version",
			"capabilities",
			"sms_role_state",
			"push_token",
			"push_provider",
			"public_key",
			"last_seen_at",
			"created_at",
			"updated_at",
		}).AddRow("device-1", "ios", "Phone", "1.0.0", []string{"chat", "push"}, "PRIMARY", "push-token", "apns", "pubkey", now, now, later))

	mock.ExpectQuery(`FROM refresh_tokens\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "device_id", "expires_at", "revoked_at", "created_at"}).
			AddRow("token-1", "device-1", later, nil, now))

	mock.ExpectQuery(`FROM two_factor_methods\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "method_type", "identifier", "enabled", "created_at"}).
			AddRow("2fa-1", "totp", "SECRET-SEED", true, now))

	mock.ExpectQuery(`FROM account_recovery_codes\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "code", "used", "used_at", "created_at", "expires_at"}).
			AddRow("code-1", "ABCD1234", false, nil, now, later))

	mock.ExpectQuery(`FROM account_deletion_audit\s+WHERE user_id = \$1::uuid\s+ORDER BY requested_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "requested_at", "effective_at", "completed_at", "status", "reason"}).
			AddRow("request-1", now, later, nil, "PENDING", "self_service_request"))

	mock.ExpectQuery(`FROM conversations c`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"conversation_id",
			"type",
			"title",
			"avatar_url",
			"description",
			"creator_user_id",
			"encryption_state",
			"encryption_epoch",
			"allow_message_effects",
			"theme",
			"retention_seconds",
			"expires_at",
			"settings_version",
			"settings_updated_at",
			"updated_at",
			"last_message_preview",
			"unread_count",
			"nickname",
			"viewer_role",
			"closed",
			"archived",
			"pinned",
			"muted_until",
			"joined_at",
			"last_read_server_order",
			"last_delivered_server_order",
			"read_at",
			"delivery_at",
			"blocked_by_viewer",
			"blocked_by_other",
			"participants",
			"external_phones",
		}).AddRow(
			"conversation-1",
			"GROUP",
			"Launch",
			"https://example.com/group.png",
			"Project chat",
			"user-1",
			"PLAINTEXT",
			int64(1),
			true,
			"midnight",
			int64(86400),
			nil,
			int64(2),
			later,
			later,
			"Last message",
			int64(4),
			"Project",
			"ADMIN",
			false,
			true,
			false,
			nil,
			now,
			int64(9),
			int64(11),
			now,
			later,
			false,
			true,
			[]byte(`["user-1","user-2"]`),
			[]byte(`["+15550002222"]`),
		))

	mock.ExpectQuery(`FROM messages m`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"message_id",
			"conversation_id",
			"sender_user_id",
			"sender_device_id",
			"content_type",
			"content",
			"client_generated_id",
			"transport",
			"server_order",
			"created_at",
			"edited_at",
			"deleted_at",
			"visibility_state",
			"attachments",
			"reactions",
			"read_receipts",
			"effects",
		}).AddRow(
			"message-1",
			"conversation-1",
			"user-1",
			"device-1",
			"text",
			[]byte(`{"text":"hello","forwarded_from":{"message_id":"source-1"}}`),
			"client-1",
			"OHMF",
			int64(42),
			now,
			later,
			nil,
			"",
			[]byte(`[{"attachment_id":"att-1","object_key":"obj","thumbnail_key":"thumb","mime_type":"image/png","size_bytes":120,"created_at":"2026-03-20T13:00:00Z","deleted_at":null,"redacted_at":null}]`),
			[]byte(`[{"user_id":"user-2","emoji":"😀","created_at":"2026-03-20T13:05:00Z"}]`),
			[]byte(`[{"reader_user_id":"user-2","read_at":"2026-03-20T13:10:00Z"}]`),
			[]byte(`[{"triggered_by_user_id":"user-2","effect_type":"bubble_confetti","triggered_at":"2026-03-20T13:15:00Z"}]`),
		))

	payload, err := svc.ExportAccount(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ExportAccount failed: %v", err)
	}

	user := payload["user"].(map[string]any)
	if user["user_id"] != "user-1" {
		t.Fatalf("unexpected user id: %#v", user["user_id"])
	}
	if user["deletion_state"] != "ACTIVE" {
		t.Fatalf("unexpected deletion state: %#v", user["deletion_state"])
	}

	devices := payload["devices"].([]map[string]any)
	if len(devices) != 1 || devices[0]["device_id"] != "device-1" {
		t.Fatalf("unexpected devices payload: %#v", devices)
	}

	security := payload["security"].(map[string]any)
	if len(security["refresh_tokens"].([]map[string]any)) != 1 {
		t.Fatalf("expected one refresh token, got %#v", security["refresh_tokens"])
	}
	methods := security["two_factor_methods"].([]map[string]any)
	if len(methods) != 1 || methods[0]["identifier_redacted"] != true {
		t.Fatalf("expected redacted TOTP metadata, got %#v", methods)
	}
	recoveryCodes := security["recovery_codes"].([]map[string]any)
	if len(recoveryCodes) != 1 || recoveryCodes[0]["code"] != "ABCD1234" {
		t.Fatalf("unexpected recovery codes: %#v", recoveryCodes)
	}

	conversations := payload["conversations"].([]map[string]any)
	if len(conversations) != 1 {
		t.Fatalf("expected one conversation, got %#v", conversations)
	}
	participants := conversations[0]["participants"].([]string)
	if len(participants) != 2 {
		t.Fatalf("unexpected participants: %#v", participants)
	}
	if conversations[0]["blocked"] != true {
		t.Fatalf("expected blocked conversation export to be preserved")
	}

	messages := payload["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("expected one message, got %#v", messages)
	}
	content := messages[0]["content"].(map[string]any)
	if content["text"] != "hello" {
		t.Fatalf("unexpected message content: %#v", content)
	}
	attachments := messages[0]["attachments"].([]map[string]any)
	if len(attachments) != 1 {
		t.Fatalf("unexpected attachments: %#v", attachments)
	}
	reactions := messages[0]["reactions"].([]map[string]any)
	if len(reactions) != 1 {
		t.Fatalf("unexpected reactions: %#v", reactions)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestDeleteAccountSchedulesDeletionAndRevokesCredentials(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := &Service{db: mock}

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT primary_phone_e164\s+FROM users\s+WHERE id = \$1::uuid\s+FOR UPDATE`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"primary_phone_e164"}).AddRow("+15550001111"))
	mock.ExpectExec(`INSERT INTO account_deletion_audit`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`INSERT INTO security_audit_events`).
		WithArgs("user-1", "user-1", "account_delete_requested", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`UPDATE refresh_tokens SET revoked_at = now\(\) WHERE user_id = \$1::uuid AND revoked_at IS NULL`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 3))
	mock.ExpectExec(`DELETE FROM devices WHERE user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec(`DELETE FROM account_recovery_codes WHERE user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 4))
	mock.ExpectExec(`DELETE FROM two_factor_methods WHERE user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM idempotency_keys WHERE actor_user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 5))
	mock.ExpectExec(`DELETE FROM user_blocks WHERE blocker_user_id = \$1::uuid OR blocked_user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec(`DELETE FROM phone_verification_challenges WHERE phone_e164 = \$1`).
		WithArgs("+15550001111").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM external_contacts WHERE phone_e164 = \$1`).
		WithArgs("+15550001111").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`UPDATE users\s+SET primary_phone_e164 = \$2`).
		WithArgs("user-1", "deleted:user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	if err := svc.DeleteAccount(context.Background(), "user-1"); err != nil {
		t.Fatalf("DeleteAccount failed: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateExportArtifactPersistsSnapshotAndAudit(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := &Service{db: mock}
	now := time.Date(2026, 3, 20, 13, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)

	mock.ExpectQuery(`FROM users\s+WHERE id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "primary_phone_e164", "display_name", "avatar_url", "phone_verified_at", "created_at", "updated_at",
			"deletion_state", "deletion_requested_at", "deletion_effective_at", "deletion_completed_at", "deletion_reason",
		}).AddRow("user-1", "+15550001111", "James", "", now, now, now, "ACTIVE", nil, nil, nil, ""))
	mock.ExpectQuery(`FROM devices\s+WHERE user_id = \$1::uuid\s+ORDER BY updated_at DESC, created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "platform", "device_name", "client_version", "capabilities", "sms_role_state", "push_token", "push_provider", "public_key", "last_seen_at", "created_at", "updated_at"}))
	mock.ExpectQuery(`FROM refresh_tokens\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "device_id", "expires_at", "revoked_at", "created_at"}))
	mock.ExpectQuery(`FROM two_factor_methods\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "method_type", "identifier", "enabled", "created_at"}))
	mock.ExpectQuery(`FROM account_recovery_codes\s+WHERE user_id = \$1::uuid\s+ORDER BY created_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "code", "used", "used_at", "created_at", "expires_at"}))
	mock.ExpectQuery(`FROM account_deletion_audit\s+WHERE user_id = \$1::uuid\s+ORDER BY requested_at DESC`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "requested_at", "effective_at", "completed_at", "status", "reason"}))
	mock.ExpectQuery(`FROM conversations c`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"conversation_id", "type", "title", "avatar_url", "description", "creator_user_id", "encryption_state",
			"encryption_epoch", "allow_message_effects", "theme", "retention_seconds", "expires_at", "settings_version",
			"settings_updated_at", "updated_at", "last_message_preview", "unread_count", "nickname", "viewer_role",
			"closed", "archived", "pinned", "muted_until", "joined_at", "last_read_server_order", "last_delivered_server_order",
			"read_at", "delivery_at", "blocked_by_viewer", "blocked_by_other", "participants", "external_phones",
		}))
	mock.ExpectQuery(`FROM messages m`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{
			"message_id", "conversation_id", "sender_user_id", "sender_device_id", "content_type", "content",
			"client_generated_id", "transport", "server_order", "created_at", "edited_at", "deleted_at",
			"visibility_state", "attachments", "reactions", "read_receipts", "effects",
		}))
	mock.ExpectQuery(`INSERT INTO account_exports`).
		WithArgs("user-1", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"id", "expires_at"}).AddRow("export-1", later))
	mock.ExpectExec(`INSERT INTO security_audit_events`).
		WithArgs("user-1", "user-1", "account_export_created", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	result, err := svc.CreateExportArtifact(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("CreateExportArtifact failed: %v", err)
	}
	if result["export_id"] != "export-1" {
		t.Fatalf("unexpected export id: %#v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestFinalizeDeletionMarksCompletedAndDeletesOperationalArtifacts(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	defer mock.Close()

	svc := &Service{db: mock}
	past := time.Now().UTC().Add(-time.Hour)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT COALESCE\(deletion_state, 'ACTIVE'\), deletion_effective_at`).
		WithArgs("user-1").
		WillReturnRows(pgxmock.NewRows([]string{"deletion_state", "deletion_effective_at"}).AddRow("PENDING", past))
	mock.ExpectExec(`DELETE FROM account_exports WHERE user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`DELETE FROM device_pairing_sessions WHERE user_id = \$1::uuid`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec(`UPDATE account_deletion_audit`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec(`UPDATE users`).
		WithArgs("user-1").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec(`INSERT INTO security_audit_events`).
		WithArgs("user-1", "user-1", "account_delete_finalized", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectCommit()

	if err := svc.FinalizeDeletion(context.Background(), "user-1"); err != nil {
		t.Fatalf("FinalizeDeletion failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
