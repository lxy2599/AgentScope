package httpapi

import (
	"context"
	"net/http"
	"time"

	"AgentScope/internal/auth"
)

type contextKey string

const (
	userIDKey    contextKey = "userID"
	sessionIDKey contextKey = "sessionID"
)

// requireAuth is middleware that verifies the session cookie and injects the
// user ID into the request context. Returns 401 if the cookie is missing or
// invalid.
func requireAuth(deps Dependencies) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Authentication required")
				return
			}

			if deps.UserSessions != nil {
				session, err := deps.UserSessions.Authenticate(r.Context(), cookie.Value)
				if err != nil {
					clearSessionCookie(w, deps.SecureCookies)
					writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid or expired session")
					return
				}
				ctx := context.WithValue(r.Context(), userIDKey, session.UserID)
				ctx = context.WithValue(ctx, sessionIDKey, session.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Compatibility fallback for deployments that have not yet migrated to
			// server-authoritative sessions. Production wiring always supplies
			// UserSessions.
			userID, err := deps.SessionSigner.VerifyUserID(cookie.Value)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Invalid session")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// userIDFromCtx extracts the authenticated user ID from the request context.
// Returns empty string if not authenticated.
func userIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

func sessionIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(sessionIDKey).(string)
	return v
}

// setSessionCookie sets an HttpOnly signed session cookie.
func setSessionCookie(w http.ResponseWriter, signedValue string, ttl time.Duration, secure bool) {
	if ttl <= 0 {
		ttl = auth.DefaultSessionTTL
	}
	maxAge := int(ttl / time.Second)
	if ttl%time.Second != 0 {
		maxAge++
	}
	if maxAge < 1 {
		maxAge = 1
	}
	expires := time.Now().UTC().Add(ttl)
	if expires.Nanosecond() != 0 {
		expires = expires.Truncate(time.Second).Add(time.Second)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    signedValue,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0).UTC(), Secure: secure, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
