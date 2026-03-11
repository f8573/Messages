package middleware

import (
	"context"
	"net/http"
	"strings"

	"ohmf/services/gateway/internal/token"
)

type ctxKey string

const (
	userIDKey       ctxKey = "user_id"
	userProfilesKey ctxKey = "user_profiles"
)

func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

// WithUserID returns a new context with the provided user id set. This is
// convenient for tests to simulate an authenticated request.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// WithProfiles returns a new context with the provided user profiles set.
// This is useful for tests to simulate users with specific profiles.
func WithProfiles(ctx context.Context, profiles []string) context.Context {
	return context.WithValue(ctx, userProfilesKey, profiles)
}

func ProfilesFromContext(ctx context.Context) ([]string, bool) {
	v, ok := ctx.Value(userProfilesKey).([]string)
	return v, ok
}

// HasProfile reports whether the context contains the given profile.
func HasProfile(ctx context.Context, profile string) bool {
	if ps, ok := ProfilesFromContext(ctx); ok {
		for _, p := range ps {
			if p == profile {
				return true
			}
		}
	}
	return false
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
