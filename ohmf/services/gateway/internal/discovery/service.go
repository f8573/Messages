package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Contact struct {
	Hash  string `json:"hash"`
	Label string `json:"label,omitempty"`
}

type Match struct {
	Hash        string `json:"hash"`
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
}

type Service struct {
	db     *pgxpool.Pool
	pepper string
}

func NewService(db *pgxpool.Pool, pepper string) *Service { return &Service{db: db, pepper: pepper} }

// Discover accepts client-supplied hashes and returns matched users.
// Algorithm expected: SHA256_PEPPERED_V1 — server computes hex(SHA256(pepper || phone_e164)).
func (s *Service) Discover(ctx context.Context, contacts []Contact) ([]Match, error) {
	// build a set of incoming hashes for quick lookup
	in := map[string]struct{}{}
	for _, c := range contacts {
		in[c.Hash] = struct{}{}
	}

	// fetch all external contacts and join to users to return user info
	rows, err := s.db.Query(ctx, `
        SELECT ec.phone_e164, u.id::text, COALESCE(u.display_name, '')
        FROM external_contacts ec
        LEFT JOIN users u ON u.primary_phone_e164 = ec.phone_e164
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Match
	for rows.Next() {
		var phone, uid, name string
		if err := rows.Scan(&phone, &uid, &name); err != nil {
			return nil, err
		}
		// compute peppered SHA256
		h := sha256.Sum256(append([]byte(s.pepper), []byte(phone)...))
		hexh := hex.EncodeToString(h[:])
		if _, ok := in[hexh]; ok {
			out = append(out, Match{Hash: hexh, UserID: uid, DisplayName: name})
		}
	}
	return out, nil
}
