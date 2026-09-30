package panel

import (
	"net/http"
	"strings"
	"time"
)

func (a *App) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if a.currentSession(r) != nil {
		http.Redirect(w, r, "/domains", http.StatusSeeOther)
		return
	}
	a.renderLogin(w, http.StatusOK, "")
}

func (a *App) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	now := time.Now().UTC()
	if a.loginLimiter.blocked(ip, now) {
		a.renderLogin(w, http.StatusTooManyRequests, "Terlalu banyak percobaan login. Coba lagi beberapa menit lagi.")
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderLogin(w, http.StatusBadRequest, "Invalid form.")
		return
	}
	if !a.validLoginCSRF(r.FormValue("csrf_token"), now) {
		a.renderLogin(w, http.StatusForbidden, "Form login kedaluwarsa. Silakan login ulang.")
		return
	}
	admin, err := a.store.AuthenticateAdmin(r.Context(), strings.TrimSpace(r.FormValue("username")), r.FormValue("password"))
	if err != nil {
		a.loginLimiter.recordFailure(ip, now)
		a.renderLogin(w, http.StatusUnauthorized, "Username atau password salah.")
		return
	}
	a.loginLimiter.recordSuccess(ip)
	// Opportunistic cleanup so expired sessions don't accumulate forever.
	_ = a.store.PruneExpiredSessions(r.Context(), now)
	token, err := randomToken()
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	csrf, err := randomToken()
	if err != nil {
		http.Error(w, "could not create session", http.StatusInternalServerError)
		return
	}
	expires := time.Now().UTC().Add(sessionTTL)
	if err := a.store.CreateSession(r.Context(), admin.ID, token, csrf, expires); err != nil {
		http.Error(w, "could not save session", http.StatusInternalServerError)
		return
	}
	a.setSessionCookie(w, token, expires)
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) renderLogin(w http.ResponseWriter, status int, errMsg string) {
	token, err := a.loginCSRFToken(time.Now().UTC())
	if err != nil {
		http.Error(w, "could not create CSRF token", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "login", pageData{
		Error:     errMsg,
		LoginCSRF: token,
	})
}

func (a *App) handleLogoutPost(w http.ResponseWriter, r *http.Request) {
	if session := sessionFromContext(r.Context()); session != nil {
		_ = a.store.DeleteSession(r.Context(), session.Token)
	}
	a.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session := a.currentSession(r)
		if session == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(withSession(r.Context(), session)))
	}
}

func (a *App) currentSession(r *http.Request) *Session {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	token, ok := decodeCookie(a.sessionKey, cookie.Value)
	if !ok {
		return nil
	}
	session, err := a.store.GetSession(r.Context(), token, time.Now().UTC())
	if err != nil {
		return nil
	}
	return session
}

func (a *App) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session := sessionFromContext(r.Context())
		if session == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			_ = r.ParseMultipartForm(128 << 20)
		} else {
			_ = r.ParseForm()
		}
		if r.FormValue("csrf_token") != session.CSRFToken {
			http.Error(w, ErrInvalidCSRF.Error(), http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
