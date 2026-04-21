package messages

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestWaitAckReturnsPersistedAck(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	pipeline := &AsyncPipeline{redis: client}
	ack := PersistedAck{
		EventID:        "evt-1",
		MessageID:      "msg-1",
		ConversationID: "conv-1",
		ServerOrder:    42,
		Status:         "SENT",
		Transport:      "OHMF",
		PersistedAtMS:  time.Now().UnixMilli(),
	}
	body, err := json.Marshal(ack)
	if err != nil {
		t.Fatalf("marshal ack: %v", err)
	}
	mr.Set(AckRedisKey("evt-1"), string(body))

	got, ok, err := pipeline.WaitAck(context.Background(), "evt-1", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitAck returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected ack to be found")
	}
	if got.MessageID != ack.MessageID || got.ServerOrder != ack.ServerOrder {
		t.Fatalf("unexpected ack payload: %#v", got)
	}
}

func TestWaitAckTimesOutWhenAckMissing(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	pipeline := &AsyncPipeline{redis: client}
	startedAt := time.Now()
	_, ok, err := pipeline.WaitAck(context.Background(), "missing", 120*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitAck returned error: %v", err)
	}
	if ok {
		t.Fatal("expected ack lookup to time out")
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("ack timeout took too long: %v", elapsed)
	}
}

func TestWaitAckFallsBackToQueuedOnRedisError(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()
	mr.Close()

	pipeline := &AsyncPipeline{redis: client}
	_, ok, err := pipeline.WaitAck(context.Background(), "evt-err", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitAck returned error: %v", err)
	}
	if ok {
		t.Fatal("expected no ack when redis is unavailable")
	}
}
