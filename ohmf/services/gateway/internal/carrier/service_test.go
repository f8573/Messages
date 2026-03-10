package carrier

import (
	"context"
	"errors"
	"testing"
	"time"

	pgx "github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock"
)

func TestSetServerMessageLink_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	carrierID := "11111111-1111-1111-1111-111111111111"
	serverID := "22222222-2222-2222-2222-222222222222"
	createdAt := time.Now().UTC()

	// Expect initial SELECT to check device_authoritative and existing server_message_id
	mockPool.ExpectQuery("SELECT device_authoritative").WithArgs(carrierID).
		WillReturnRows(pgxmock.NewRows([]string{"device_authoritative", "server_message_id"}).AddRow(true, nil))

	// Expect the UPDATE ... RETURNING query
	mockPool.ExpectQuery("UPDATE carrier_messages").WithArgs(carrierID, serverID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "device_id", "thread_key", "carrier_message_id", "direction", "transport", "text", "media_json", "created_at", "device_authoritative", "server_message_id", "raw_payload",
		}).
			AddRow(carrierID, "33333333-3333-3333-3333-333333333333", "thread-1", "cm-1", "IN", "SMS", "hello", []byte("null"), createdAt, true, func() *string { s := serverID; return &s }(), []byte("null")))

	// Expect the audit insert with the actor value
	mockPool.ExpectExec("INSERT INTO carrier_message_links_audit").WithArgs(carrierID, serverID, "tester").WillReturnResult(pgxmock.NewResult("INSERT", 1))

	cm, err := svc.SetServerMessageLink(context.Background(), carrierID, serverID, "tester")
	if err != nil {
		t.Fatalf("SetServerMessageLink returned error: %v", err)
	}
	if cm.ServerMessageID == nil || *cm.ServerMessageID != serverID {
		t.Fatalf("expected server_message_id %s, got %v", serverID, cm.ServerMessageID)
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSetServerMessageLink_AuditInsertFails(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	carrierID := "11111111-1111-1111-1111-111111111111"
	serverID := "22222222-2222-2222-2222-222222222222"
	createdAt := time.Now().UTC()

	// Expect the UPDATE ... RETURNING query
	// Expect initial SELECT to check device_authoritative and existing server_message_id
	mockPool.ExpectQuery("SELECT device_authoritative").WithArgs(carrierID).
		WillReturnRows(pgxmock.NewRows([]string{"device_authoritative", "server_message_id"}).AddRow(true, nil))

	mockPool.ExpectQuery("UPDATE carrier_messages").WithArgs(carrierID, serverID).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "device_id", "thread_key", "carrier_message_id", "direction", "transport", "text", "media_json", "created_at", "device_authoritative", "server_message_id", "raw_payload",
		}).
			AddRow(carrierID, "33333333-3333-3333-3333-333333333333", "thread-1", "cm-1", "IN", "SMS", "hello", []byte("null"), createdAt, true, func() *string { s := serverID; return &s }(), []byte("null")))

	// Simulate audit insert failure — should be logged but not returned as error
	mockPool.ExpectExec("INSERT INTO carrier_message_links_audit").WithArgs(carrierID, serverID, "tester").WillReturnError(errors.New("insert failed"))

	cm, err := svc.SetServerMessageLink(context.Background(), carrierID, serverID, "tester")
	if err != nil {
		t.Fatalf("SetServerMessageLink returned error despite audit failure: %v", err)
	}
	if cm.ServerMessageID == nil || *cm.ServerMessageID != serverID {
		t.Fatalf("expected server_message_id %s, got %v", serverID, cm.ServerMessageID)
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestSetServerMessageLink_NoMatch(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	carrierID := "00000000-0000-0000-0000-000000000000"
	serverID := "22222222-2222-2222-2222-222222222222"

	// Simulate no rows returned (no device_authoritative match)
	mockPool.ExpectQuery("SELECT device_authoritative").WithArgs(carrierID).
		WillReturnError(pgx.ErrNoRows)

	_, err = svc.SetServerMessageLink(context.Background(), carrierID, serverID, "tester")
	if err == nil {
		t.Fatalf("expected error when no matching carrier message, got nil")
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}

}

func TestListCarrierMessageLinks_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	carrierID := "11111111-1111-1111-1111-111111111111"
	auditID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	serverID := "22222222-2222-2222-2222-222222222222"
	setAt := time.Now().UTC()

	mockPool.ExpectQuery("SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor").WithArgs(carrierID, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "carrier_message_id", "server_message_id", "set_at", "actor"}).
			AddRow(auditID, carrierID, func() *string { s := serverID; return &s }(), setAt, "tester"))

	rows, err := svc.ListCarrierMessageLinks(context.Background(), carrierID, 100)
	if err != nil {
		t.Fatalf("ListCarrierMessageLinks returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(rows))
	}
	if rows[0].Actor != "tester" {
		t.Fatalf("unexpected actor: %s", rows[0].Actor)
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListCarrierMessageLinks_Empty(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	carrierID := "00000000-0000-0000-0000-000000000000"

	mockPool.ExpectQuery("SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor").WithArgs(carrierID, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "carrier_message_id", "server_message_id", "set_at", "actor"}))
	rows, err := svc.ListCarrierMessageLinks(context.Background(), carrierID, 100)
	if err != nil {
		t.Fatalf("ListCarrierMessageLinks returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 audit rows, got %d", len(rows))
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListCarrierMessageLinksByActor_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	actor := "tester"
	auditID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	carrierID := "11111111-1111-1111-1111-111111111111"
	serverID := "22222222-2222-2222-2222-222222222222"
	setAt := time.Now().UTC()

	mockPool.ExpectQuery("SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor").WithArgs(actor, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "carrier_message_id", "server_message_id", "set_at", "actor"}).
			AddRow(auditID, carrierID, func() *string { s := serverID; return &s }(), setAt, actor))

	rows, err := svc.ListCarrierMessageLinksByActor(context.Background(), actor, 100)
	if err != nil {
		t.Fatalf("ListCarrierMessageLinksByActor returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(rows))
	}
	if rows[0].Actor != actor {
		t.Fatalf("unexpected actor: %s", rows[0].Actor)
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestListCarrierMessageLinksByActor_Empty(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgx mock: %v", err)
	}
	defer mockPool.Close()

	svc := NewService(&mockAdapter{p: mockPool})

	actor := "noone"

	mockPool.ExpectQuery("SELECT id::text, carrier_message_id::text, server_message_id::text, set_at, actor").WithArgs(actor, 100).
		WillReturnRows(pgxmock.NewRows([]string{"id", "carrier_message_id", "server_message_id", "set_at", "actor"}))
	rows, err := svc.ListCarrierMessageLinksByActor(context.Background(), actor, 100)
	if err != nil {
		t.Fatalf("ListCarrierMessageLinksByActor returned error: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 audit rows, got %d", len(rows))
	}

	if err := mockPool.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
