package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"ohmf/services/gateway/internal/limit"
	"ohmf/services/gateway/internal/messages"
	appmw "ohmf/services/gateway/internal/middleware"
	"ohmf/services/gateway/internal/observability"
	"ohmf/services/gateway/internal/token"
)

const presenceTTL = 90 * time.Second

type Handler struct {
	tokens     *token.Service
	messages   *messages.Service
	redis      *redis.Client
	limiter    *limit.TokenBucket
	enableSend bool
	upgrader   websocket.Upgrader

	mu      sync.RWMutex
	clients map[string]map[*client]struct{}
}

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

type wsEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type sendMessagePayload struct {
	ConversationID    string         `json:"conversation_id"`
	IdempotencyKey    string         `json:"idempotency_key"`
	ContentType       string         `json:"content_type"`
	Content           map[string]any `json:"content"`
	ClientGeneratedID string         `json:"client_generated_id"`
}

type presenceSubscribePayload struct {
	ConversationIDs []string `json:"conversation_ids"`
}

func NewHandler(tokens *token.Service, messageService *messages.Service, redisClient *redis.Client, limiter *limit.TokenBucket, enableSend bool) *Handler {
	return &Handler{
		tokens:     tokens,
		messages:   messageService,
		redis:      redisClient,
		limiter:    limiter,
		enableSend: enableSend,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool { return true },
		},
		clients: map[string]map[*client]struct{}{},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := ipOnly(r.RemoteAddr)
	if err := h.allowConnect(ctx, ip); err != nil {
		http.Error(w, "rate_limited", http.StatusTooManyRequests)
		return
	}
	userID, err := h.authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	c := &client{
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 128),
	}
	h.register(c)
	h.markPresence(ctx, userID)

	go h.writeLoop(c)
	go h.subscribeDelivery(ctx, c)
	h.readLoop(c, ip)
}

func (h *Handler) allowConnect(ctx context.Context, ip string) error {
	if h.limiter == nil {
		return nil
	}
	decision, err := h.limiter.Allow(ctx, "rate:ws:connect:ip:"+ip, 60, time.Minute, 120, 1)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return limit.ErrRateLimited
	}
	return nil
}

func (h *Handler) authenticate(r *http.Request) (string, error) {
	accessToken := strings.TrimSpace(r.URL.Query().Get("access_token"))
	if accessToken == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			accessToken = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		}
	}
	if accessToken == "" {
		return "", errors.New("missing access token")
	}
	claims, err := h.tokens.ParseAccess(accessToken)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

func (h *Handler) readLoop(c *client, ip string) {
	defer h.unregister(c)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(_ string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		h.markPresence(context.Background(), c.userID)
		return nil
	})

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	go func() {
		for range heartbeat.C {
			h.markPresence(context.Background(), c.userID)
			_ = c.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
		}
	}()

	for {
		_, payload, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if err := h.allowControlEvent(context.Background(), c.userID); err != nil {
			h.sendJSON(c, "error", map[string]any{"code": "rate_limited", "message": "ws control rate limit"})
			continue
		}
		var env wsEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid event envelope"})
			continue
		}
		observability.RecordWSMessage("received", env.Event)
		switch env.Event {
		// Support legacy/spec alias: "subscribe" maps to presence_subscribe
		case "subscribe":
			var raw interface{}
			if err := json.Unmarshal(env.Data, &raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid subscribe payload"})
				continue
			}
			if err := appmw.ValidateData("ws-subscribe", raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": err.Error()})
				continue
			}
			var req presenceSubscribePayload
			if err := json.Unmarshal(env.Data, &req); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid subscribe payload"})
				continue
			}
			for _, convID := range req.ConversationIDs {
				if convID == "" {
					continue
				}
				_ = h.redis.Set(context.Background(), "presence:conv:"+convID+":user:"+c.userID, "1", presenceTTL).Err()
			}
			h.sendJSON(c, "subscribe_ack", map[string]any{"status": "ok", "conversation_ids": req.ConversationIDs})

		case "auth":
			h.sendJSON(c, "auth", map[string]any{"status": "ok", "user_id": c.userID})
		case "send_message":
			if !h.enableSend {
				h.sendJSON(c, "error", map[string]any{"code": "ws_send_disabled", "message": "ws send disabled"})
				continue
			}
			// validate payload against schema
			var raw interface{}
			if err := json.Unmarshal(env.Data, &raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid send_message payload"})
				continue
			}
			if err := appmw.ValidateData("ws-send_message", raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": err.Error()})
				continue
			}
			var req sendMessagePayload
			if err := json.Unmarshal(env.Data, &req); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid send_message payload"})
				continue
			}
			result, err := h.messages.Send(context.Background(), c.userID, req.ConversationID, req.IdempotencyKey, req.ContentType, req.Content, req.ClientGeneratedID, "ws-"+time.Now().UTC().Format(time.RFC3339Nano), ip)
			if err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "send_failed", "message": err.Error()})
				continue
			}
			h.sendJSON(c, "ack", map[string]any{
				"message_id":      result.Message.MessageID,
				"conversation_id": result.Message.ConversationID,
				"server_order":    result.Message.ServerOrder,
				"status":          "SENT",
				"queued":          result.Queued,
				"ack_timeout_ms":  result.AckTimeoutMS,
			})
		case "presence_subscribe":
			var raw interface{}
			if err := json.Unmarshal(env.Data, &raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid presence_subscribe payload"})
				continue
			}
			if err := appmw.ValidateData("ws-subscribe", raw); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": err.Error()})
				continue
			}
			var req presenceSubscribePayload
			if err := json.Unmarshal(env.Data, &req); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid presence_subscribe payload"})
				continue
			}
			for _, convID := range req.ConversationIDs {
				if convID == "" {
					continue
				}
				_ = h.redis.Set(context.Background(), "presence:conv:"+convID+":user:"+c.userID, "1", presenceTTL).Err()
			}
			h.sendJSON(c, "presence_update", map[string]any{"status": "online", "user_id": c.userID})

		case "resync":
			// Minimal resync acknowledgement: clients may request a resync cursor
			// for a conversation after reconnect. Full resync requires fetching
			// missing events/messages which is out of scope for this lightweight
			// handler; respond with an ack so clients can fallback to REST sync.
			var payload map[string]any
			if err := json.Unmarshal(env.Data, &payload); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid resync payload"})
				continue
			}
			// echo back any conversation ids provided
			conv, _ := payload["conversation_id"]
			h.sendJSON(c, "resync_ack", map[string]any{"conversation_id": conv})

		case "typing.started":
			var p struct {
				ConversationID string `json:"conversation_id"`
			}
			if err := json.Unmarshal(env.Data, &p); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid typing payload"})
				continue
			}
			if p.ConversationID == "" {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "conversation_id required"})
				continue
			}
			// ensure actor is a member
			if ok, err := h.messages.IsMember(context.Background(), c.userID, p.ConversationID); err != nil || !ok {
				if err != nil {
					h.sendJSON(c, "error", map[string]any{"code": "server_error", "message": "membership_check_failed"})
				} else {
					h.sendJSON(c, "error", map[string]any{"code": "forbidden", "message": "not a member"})
				}
				continue
			}
			// set ephemeral typing key and publish
			if h.redis != nil {
				_ = h.redis.Set(context.Background(), "typing:conv:"+p.ConversationID+":user:"+c.userID, "1", 5*time.Second).Err()
				_ = h.redis.Publish(context.Background(), "typing:conv:"+p.ConversationID, map[string]any{"type": "typing.started", "conversation_id": p.ConversationID, "user_id": c.userID}).Err()
			}
			// local fanout to connected members
			h.mu.RLock()
			for uid, bucket := range h.clients {
				if uid == c.userID {
					continue
				}
				// check membership for recipient
				if ok, _ := h.messages.IsMember(context.Background(), uid, p.ConversationID); !ok {
					continue
				}
				for cli := range bucket {
					h.sendJSON(cli, "typing.started", map[string]any{"conversation_id": p.ConversationID, "user_id": c.userID})
				}
			}
			h.mu.RUnlock()

		case "typing.stopped":
			var p struct {
				ConversationID string `json:"conversation_id"`
			}
			if err := json.Unmarshal(env.Data, &p); err != nil {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "invalid typing payload"})
				continue
			}
			if p.ConversationID == "" {
				h.sendJSON(c, "error", map[string]any{"code": "invalid_request", "message": "conversation_id required"})
				continue
			}
			if h.redis != nil {
				_ = h.redis.Del(context.Background(), "typing:conv:"+p.ConversationID+":user:"+c.userID).Err()
				_ = h.redis.Publish(context.Background(), "typing:conv:"+p.ConversationID, map[string]any{"type": "typing.stopped", "conversation_id": p.ConversationID, "user_id": c.userID}).Err()
			}
			h.mu.RLock()
			for uid, bucket := range h.clients {
				if uid == c.userID {
					continue
				}
				if ok, _ := h.messages.IsMember(context.Background(), uid, p.ConversationID); !ok {
					continue
				}
				for cli := range bucket {
					h.sendJSON(cli, "typing.stopped", map[string]any{"conversation_id": p.ConversationID, "user_id": c.userID})
				}
			}
			h.mu.RUnlock()
		default:
			h.sendJSON(c, "error", map[string]any{"code": "unsupported_event", "message": env.Event})
		}
	}
}

func (h *Handler) writeLoop(c *client) {
	defer func() {
		_ = c.conn.Close()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (h *Handler) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	bucket := h.clients[c.userID]
	if bucket == nil {
		bucket = map[*client]struct{}{}
		h.clients[c.userID] = bucket
	}
	bucket[c] = struct{}{}
	observability.IncWSConnection()
}

func (h *Handler) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if bucket, ok := h.clients[c.userID]; ok {
		delete(bucket, c)
		if len(bucket) == 0 {
			delete(h.clients, c.userID)
			_ = h.redis.Del(context.Background(), "presence:user:"+c.userID).Err()
		}
	}
	observability.DecWSConnection()
	close(c.send)
	_ = c.conn.Close()
}

func (h *Handler) sendJSON(c *client, event string, data any) {
	payload, err := json.Marshal(map[string]any{
		"event": event,
		"data":  data,
	})
	if err != nil {
		return
	}
	observability.RecordWSMessage("sent", event)
	select {
	case c.send <- payload:
	default:
	}
}

func (h *Handler) markPresence(ctx context.Context, userID string) {
	if h.redis == nil || userID == "" {
		return
	}
	_ = h.redis.Set(ctx, "presence:user:"+userID, "online", presenceTTL).Err()
}

func (h *Handler) subscribeDelivery(ctx context.Context, c *client) {
	if h.redis == nil {
		return
	}
	pubsub := h.redis.Subscribe(ctx, "delivery:user:"+c.userID)
	defer pubsub.Close()
	ch := pubsub.Channel()
	for msg := range ch {
		h.sendJSON(c, "delivery_update", json.RawMessage(msg.Payload))
	}
}

func (h *Handler) allowControlEvent(ctx context.Context, userID string) error {
	if h.limiter == nil {
		return nil
	}
	decision, err := h.limiter.Allow(ctx, "rate:ws:control:user:"+userID, 20, time.Second, 40, 1)
	if err != nil {
		return err
	}
	if !decision.Allowed {
		return limit.ErrRateLimited
	}
	return nil
}

func ipOnly(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
