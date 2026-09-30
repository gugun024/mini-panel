package panel

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type contextKey string

const (
	sessionCookieName = "mini_panel_session"
	sessionContextKey = contextKey("session")
	sessionTTL        = 12 * time.Hour
	loginCSRFMaxAge   = 15 * time.Minute
)

const (
	loginMaxAttempts = 5
	loginWindow      = 10 * time.Minute
	loginBlockFor    = 10 * time.Minute
)

type loginAttempt struct {
	count        int
	windowStart  time.Time
	blockedUntil time.Time
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*loginAttempt)}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, ok := strings.Cut(xff, ","); ok || ip != "" {
			if trimmed := strings.TrimSpace(ip); trimmed != "" {
				return trimmed
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (l *loginLimiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		return false
	}
	if now.Before(a.blockedUntil) {
		return true
	}
	if now.Sub(a.windowStart) > loginWindow {
		delete(l.attempts, ip)
		return false
	}
	return false
}

func (l *loginLimiter) recordFailure(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok || now.Sub(a.windowStart) > loginWindow {
		a = &loginAttempt{windowStart: now}
		l.attempts[ip] = a
	}
	a.count++
	if a.count >= loginMaxAttempts {
		a.blockedUntil = now.Add(loginBlockFor)
	}
}

func (l *loginLimiter) recordSuccess(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSession     = errors.New("invalid session")
	ErrInvalidCSRF        = errors.New("invalid CSRF token")
)

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func signToken(key []byte, token string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func encodeCookie(key []byte, token string) string {
	return token + "." + signToken(key, token)
}

func decodeCookie(key []byte, value string) (string, bool) {
	token, sig, ok := strings.Cut(value, ".")
	if !ok || token == "" || sig == "" {
		return "", false
	}
	expected := signToken(key, token)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}
	return token, true
}

func (a *App) loginCSRFToken(now time.Time) (string, error) {
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}
	issued := strconv.FormatInt(now.UTC().Unix(), 10)
	payload := issued + "." + nonce
	return payload + "." + signToken(a.sessionKey, "login:"+payload), nil
}

func (a *App) validLoginCSRF(token string, now time.Time) bool {
	issuedText, rest, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	nonce, sig, ok := strings.Cut(rest, ".")
	if !ok || nonce == "" || sig == "" {
		return false
	}
	payload := issuedText + "." + nonce
	expected := signToken(a.sessionKey, "login:"+payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return false
	}
	issuedUnix, err := strconv.ParseInt(issuedText, 10, 64)
	if err != nil {
		return false
	}
	issued := time.Unix(issuedUnix, 0).UTC()
	return !issued.After(now.UTC().Add(1*time.Minute)) && now.UTC().Sub(issued) <= loginCSRFMaxAge
}

func sessionFromContext(ctx context.Context) *Session {
	session, _ := ctx.Value(sessionContextKey).(*Session)
	return session
}

func withSession(ctx context.Context, session *Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, session)
}

func (a *App) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    encodeCookie(a.sessionKey, token),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

func (a *App) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secureCookie,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
