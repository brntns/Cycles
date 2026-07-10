// Package auth implements single-user, password-from-env authentication
// with a Postgres-backed session cookie. There is no user table: the
// password lives in CYCLE_PASSWORD. Sessions are stored in the database
// (not in memory) so any app instance can validate them and a redeploy
// never logs the user out.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const CookieName = "cycle_session"

var ErrInvalidPassword = errors.New("invalid password")

type Service struct {
	pool           *pgxpool.Pool
	password       string
	cookieSecure   bool
	sessionMaxDays int

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func NewService(pool *pgxpool.Pool, password string, cookieSecure bool, sessionMaxDays int) *Service {
	return &Service{
		pool:           pool,
		password:       password,
		cookieSecure:   cookieSecure,
		sessionMaxDays: sessionMaxDays,
		attempts:       make(map[string][]time.Time),
	}
}

// Allow permits at most 5 login attempts per IP per 5-minute window.
func (s *Service) rateLimitOK(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-5 * time.Minute)

	kept := s.attempts[ip][:0]
	for _, t := range s.attempts[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 5 {
		s.attempts[ip] = kept
		return false
	}
	s.attempts[ip] = append(kept, now)
	return true
}

func (s *Service) Login(ctx context.Context, ip, password string) (token string, expires time.Time, err error) {
	if !s.rateLimitOK(ip) {
		return "", time.Time{}, errors.New("too many login attempts, try again later")
	}

	if subtle.ConstantTimeCompare([]byte(password), []byte(s.password)) != 1 {
		return "", time.Time{}, ErrInvalidPassword
	}

	token, err = randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = time.Now().AddDate(0, 0, s.sessionMaxDays)

	_, err = s.pool.Exec(ctx,
		`INSERT INTO sessions (token, expires_at) VALUES ($1, $2)`,
		token, expires,
	)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

func (s *Service) Validate(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}
	var expires time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT expires_at FROM sessions WHERE token = $1`, token,
	).Scan(&expires)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		return false
	}
	return time.Now().Before(expires)
}

func (s *Service) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(CookieName)
		if err != nil || !s.Validate(r.Context(), cookie.Value) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","detail":"login required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
