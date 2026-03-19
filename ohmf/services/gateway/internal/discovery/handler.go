package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"ohmf/services/gateway/internal/middleware"
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



type requestBody struct {
	Algorithm string    `json:"algorithm"`
	Contacts  []Contact `json:"contacts"`
}

type responseBody struct {
	Matches []Match `json:"matches"`
}

type Handler struct {
	db     *pgxpool.Pool
	pepper string
}

func NewHandler(db *pgxpool.Pool, pepper string) *Handler {
	return &Handler{db: db, pepper: pepper}
} }

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) {
	// require auth to reduce abuse
	if _, ok := middleware.UserIDFromContext(r.Context()); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid_json", http.StatusBadRequest)
		return
	}
	if body.Algorithm != "SHA256_PEPPERED_V1" {
		http.Error(w, "unsupported_algorithm", http.StatusBadRequest)
		return
	}
	matches, err := h.Discover(r.Context(), body.Contacts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(responseBody{Matches: matches})
}

func (h *Handler) Discover(ctx context.Context, contacts []Contact) ([]Match, error) {
	in := make(map[string]struct{}, len(contacts))
	for _, c := range contacts {
		in[c.Hash] = struct{}{}
	}

	rows, err := h.db.Query(ctx, `
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
		sum := sha256.Sum256(append([]byte(h.pepper), []byte(phone)...))
		hash := hex.EncodeToString(sum[:])
		if _, ok := in[hash]; ok {
			out = append(out, Match{Hash: hash, UserID: uid, DisplayName: name})
		}
	}
	return out, nil
}
