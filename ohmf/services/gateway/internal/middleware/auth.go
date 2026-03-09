package middleware

import (
	"context"
	"net/http"
	"strings"

	"ohmf/services/gateway/internal/token"
)

type ctxKey string

const (
	userIDKey ctxKey = "user_id"
	userProfilesKey ctxKey = "user_profiles"
)

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

func ProfilesFromContext(ctx context.Context) ([]string, bool) {
	v, ok := ctx.Value(userProfilesKey).([]string)
	return v, ok
}

func RequireAuth(tokens *token.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" || !strings.HasPrefix(h, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			claims, err := tokens.ParseAccess(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
			// attach user id and claimed profiles into the request context
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			if len(claims.Profiles) > 0 {
				ctx = context.WithValue(ctx, userProfilesKey, claims.Profiles)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
