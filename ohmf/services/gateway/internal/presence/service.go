package presence

import (
    "context"

    "github.com/redis/go-redis/v9"
)

type Service struct {
    rdb *redis.Client
}

func NewService(rdb *redis.Client) *Service {
    if rdb == nil {
        return nil
    }
    return &Service{rdb: rdb}
}

// IsUserOnline checks presence:user:{userID}
func (s *Service) IsUserOnline(ctx context.Context, userID string) (bool, error) {
    if s == nil || s.rdb == nil || userID == "" {
        return false, nil
    }
    val, err := s.rdb.Get(ctx, "presence:user:"+userID).Result()
    if err == redis.Nil {
        return false, nil
    }
    if err != nil {
        return false, err
    }
    return val != "", nil
}

// ConversationOnlineUsers returns list of user ids that have presence keys for the conversation
func (s *Service) ConversationOnlineUsers(ctx context.Context, conversationID string) ([]string, error) {
    if s == nil || s.rdb == nil || conversationID == "" {
        return nil, nil
    }
    // keys are presence:conv:{convID}:user:{userID}
    pattern := "presence:conv:" + conversationID + ":user:*"
    var cursor uint64
    var out []string
    for {
        keys, cur, err := s.rdb.Scan(ctx, cursor, pattern, 100).Result()
        if err != nil {
            return nil, err
        }
        cursor = cur
        for _, k := range keys {
            // extract trailing user id
            // key format ends with ":user:{userID}"
            // find last ':'
            idx := len(k) - 1
            for idx >= 0 && k[idx] != ':' {
                idx--
            }
            if idx >= 0 && idx+1 < len(k) {
                out = append(out, k[idx+1:])
            }
        }
        if cursor == 0 {
            break
        }
    }
    return out, nil
}
