package messages

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
)

type CassandraStore struct {
	session *gocql.Session
}

type CassandraConfig struct {
	Hosts       string
	Keyspace    string
	Username    string
	Password    string
	Consistency string
}

func NewCassandraStore(cfg CassandraConfig) (*CassandraStore, error) {
	cluster := gocql.NewCluster(splitCSV(cfg.Hosts)...)
	cluster.Keyspace = cfg.Keyspace
	cluster.Timeout = 5 * time.Second
	cluster.ConnectTimeout = 5 * time.Second
	cluster.Consistency = parseConsistency(cfg.Consistency)
	if cfg.Username != "" {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, err
	}
	return &CassandraStore{session: session}, nil
}

func (s *CassandraStore) Close() {
	if s == nil || s.session == nil {
		return
	}
	s.session.Close()
}

func (s *CassandraStore) ListConversation(ctx context.Context, conversationID string, limit int) ([]Message, error) {
	if s == nil || s.session == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}

	convUUID, err := gocql.ParseUUID(conversationID)
	if err != nil {
		return nil, err
	}

	items := make([]Message, 0, limit)
	now := time.Now().UTC()
	buckets := []string{
		now.Format("20060102"),
		now.Add(-24 * time.Hour).Format("20060102"),
	}

	for _, bucket := range buckets {
		iter := s.session.Query(`
			SELECT
				message_id,
				sender_user_id,
				content_type,
				content_json,
				transport,
				server_order,
				created_at
			FROM messages_by_conversation
			WHERE conversation_id = ? AND bucket_yyyymmdd = ?
			ORDER BY server_order DESC
			LIMIT ?
		`, convUUID, bucket, limit).WithContext(ctx).Iter()

		var (
			msgID      gocql.UUID
			senderID   gocql.UUID
			contentTyp string
			contentRaw string
			transport  string
			serverOrd  int64
			createdAt  time.Time
		)
		for iter.Scan(&msgID, &senderID, &contentTyp, &contentRaw, &transport, &serverOrd, &createdAt) {
			msg := Message{
				MessageID:      msgID.String(),
				ConversationID: conversationID,
				SenderUserID:   senderID.String(),
				ContentType:    contentTyp,
				Transport:      transport,
				ServerOrder:    serverOrd,
				CreatedAt:      createdAt.UTC().Format(time.RFC3339),
			}
			_ = json.Unmarshal([]byte(contentRaw), &msg.Content)
			items = append(items, msg)
			if len(items) >= limit {
				break
			}
		}
		if err := iter.Close(); err != nil {
			return nil, err
		}
		if len(items) >= limit {
			break
		}
	}

	// Cassandra query reads descending order for efficiency; reverse for API compatibility.
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items, nil
}

func splitCSV(v string) []string {
	raw := strings.Split(v, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		trim := strings.TrimSpace(item)
		if trim == "" {
			continue
		}
		out = append(out, trim)
	}
	if len(out) == 0 {
		return []string{"localhost:9042"}
	}
	return out
}

func parseConsistency(v string) gocql.Consistency {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "all":
		return gocql.All
	case "quorum":
		return gocql.Quorum
	case "localquorum":
		return gocql.LocalQuorum
	case "one":
		return gocql.One
	default:
		return gocql.Quorum
	}
}

func (s *CassandraStore) EnsureSchema(ctx context.Context, keyspace string) error {
	if s == nil || s.session == nil {
		return nil
	}
	queries := []string{
		fmt.Sprintf(`CREATE KEYSPACE IF NOT EXISTS %s WITH replication = {'class':'SimpleStrategy', 'replication_factor': 1}`, keyspace),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.messages_by_conversation (
			conversation_id uuid,
			bucket_yyyymmdd text,
			server_order bigint,
			message_id uuid,
			sender_user_id uuid,
			content_type text,
			content_json text,
			transport text,
			created_at timestamp,
			PRIMARY KEY ((conversation_id, bucket_yyyymmdd), server_order)
		) WITH CLUSTERING ORDER BY (server_order DESC)
		AND compaction = {'class': 'TimeWindowCompactionStrategy'}
		AND default_time_to_live = 31536000`, keyspace),
	}
	for _, q := range queries {
		if err := s.session.Query(q).WithContext(ctx).Exec(); err != nil {
			return err
		}
	}
	return nil
}
