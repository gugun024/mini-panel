package panel

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (a *App) handleDatabases(w http.ResponseWriter, r *http.Request) {
	databases, err := a.store.ListDatabases(r.Context())
	if err != nil {
		http.Error(w, "could not list databases", http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusOK, "databases", pageData{
		Session:        sessionFromContext(r.Context()),
		Databases:      databases,
		AdminerEnabled: a.adminerURL != nil,
	})
}

func (a *App) handleDatabasesPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	name, err := NormalizeDBIdentifier(r.FormValue("name"))
	if err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadRequest, "Nama database tidak valid.", "")
		return
	}
	username, err := NormalizeDBIdentifier(r.FormValue("username"))
	if err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadRequest, "Username database tidak valid.", "")
		return
	}
	password := r.FormValue("password")
	if len(password) < 12 || len(password) > 128 || strings.ContainsAny(password, "\r\n\x00") {
		a.renderDatabasesWithMessage(w, r, http.StatusBadRequest, "Password database minimal 12 karakter.", "")
		return
	}
	if err := a.dbCtl.Create(r.Context(), name, username, password); err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadGateway, "Gagal membuat database: "+err.Error(), "")
		return
	}
	if _, err := a.store.CreateDatabase(r.Context(), name, username, password); err != nil {
		_ = a.dbCtl.Delete(r.Context(), name, username)
		a.renderDatabasesWithMessage(w, r, http.StatusConflict, "Database sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (a *App) handleDatabaseDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	db, err := a.store.GetDatabase(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.dbCtl.Delete(r.Context(), db.Name, db.Username); err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus database: "+err.Error(), "")
		return
	}
	if _, err := a.store.DeleteDatabase(r.Context(), id); err != nil {
		a.renderDatabasesWithMessage(w, r, http.StatusInternalServerError, "Database terhapus, tapi data panel gagal dihapus.", "")
		return
	}
	http.Redirect(w, r, "/databases", http.StatusSeeOther)
}

func (a *App) handleDatabaseAdminer(w http.ResponseWriter, r *http.Request) {
	if a.adminerURL == nil {
		a.renderDatabasesWithMessage(w, r, http.StatusBadRequest, "Adminer belum aktif di instalasi ini. Jalankan ulang installer terbaru.", "")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	db, err := a.store.GetDatabase(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	q := url.Values{}
	q.Set("server", "127.0.0.1")
	q.Set("username", db.Username)
	q.Set("db", db.Name)
	http.Redirect(w, r, "/adminer/?"+q.Encode(), http.StatusSeeOther)
}

func (a *App) renderDatabasesWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	databases, err := a.store.ListDatabases(r.Context())
	if err != nil {
		http.Error(w, "could not list databases", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "databases", pageData{
		Session:        sessionFromContext(r.Context()),
		Databases:      databases,
		AdminerEnabled: a.adminerURL != nil,
		Error:          errMsg,
		Notice:         okMsg,
	})
}
