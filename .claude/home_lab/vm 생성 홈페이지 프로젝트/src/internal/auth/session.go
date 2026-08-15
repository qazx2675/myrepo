package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"vm-portal/internal/models"
)

const (
	cookieName   = "vmportal_session"
	sessionTTL   = 12 * time.Hour
)

type ctxKey int

const userCtxKey ctxKey = 0

// CreateSession issues a new random session token for userID and stores it.
func CreateSession(db *sql.DB, userID int64) (token string, expires time.Time, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(raw)
	expires = time.Now().Add(sessionTTL)

	_, err = db.Exec(`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`, token, userID, expires)
	return token, expires, err
}

func DeleteSession(db *sql.DB, token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, token)
	return err
}

// userBySession loads the user tied to a still-valid session token.
func userBySession(db *sql.DB, token string) (*models.User, error) {
	row := db.QueryRow(`
		SELECT u.id, u.username, u.password_hash, u.rbac_level, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ? AND s.expires_at > CURRENT_TIMESTAMP`, token)

	var u models.User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.RBACLevel, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

func SetSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure omitted here on purpose: set VMPORTAL_ADDR behind TLS termination
		// and add Secure:true once the portal is served over HTTPS end-to-end.
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// WithUser is middleware that resolves the session cookie (if any) into a
// *models.User and stashes it on the request context for downstream handlers.
func WithUser(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			user, err := userBySession(db, cookie.Value)
			if err != nil || user == nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func CurrentUser(r *http.Request) (*models.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(*models.User)
	return u, ok
}
