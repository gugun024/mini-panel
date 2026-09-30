package panel

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
)

func (a *App) handleDomainEnvPost(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Invalid form.", "")
		return
	}
	name, err := NormalizeEnvName(r.FormValue("name"))
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Nama environment variable tidak valid.", "")
		return
	}
	value, err := NormalizeEnvValue(r.FormValue("value"))
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Nilai environment variable tidak valid.", "")
		return
	}
	if _, err := a.store.UpsertDomainEnvVar(r.Context(), domain.ID, name, value, r.FormValue("is_secret") == "on"); err != nil {
		a.renderDomainDetail(w, r, http.StatusInternalServerError, domain, "Gagal menyimpan environment variable.", "")
		return
	}
	if err := a.applyDomainEnv(r, domain); err != nil {
		a.renderDomainDetail(w, r, http.StatusBadGateway, domain, "Env tersimpan, tapi gagal diterapkan: "+err.Error(), "")
		return
	}
	a.renderDomainDetail(w, r, http.StatusOK, domain, "", "Environment variable berhasil disimpan.")
}

func (a *App) handleDomainEnvDelete(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	envID, err := strconv.ParseInt(r.PathValue("env_id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.store.DeleteDomainEnvVar(r.Context(), domain.ID, envID); err != nil {
		a.renderDomainDetail(w, r, http.StatusInternalServerError, domain, "Gagal menghapus environment variable.", "")
		return
	}
	if err := a.applyDomainEnv(r, domain); err != nil {
		a.renderDomainDetail(w, r, http.StatusBadGateway, domain, "Env terhapus, tapi gagal diterapkan: "+err.Error(), "")
		return
	}
	a.renderDomainDetail(w, r, http.StatusOK, domain, "", "Environment variable berhasil dihapus.")
}

func (a *App) handleDomainAttachDatabase(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	dbID, err := strconv.ParseInt(r.FormValue("database_id"), 10, 64)
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Database tidak valid.", "")
		return
	}
	db, err := a.store.GetDatabase(r.Context(), dbID)
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Database tidak ditemukan.", "")
		return
	}
	if db.Password == "" {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Database lama belum punya password tersimpan. Buat database baru dari panel atau isi env manual.", "")
		return
	}
	values := map[string]struct {
		value  string
		secret bool
	}{
		"DB_CONNECTION": {value: "mysql"},
		"DB_HOST":       {value: "127.0.0.1"},
		"DB_PORT":       {value: "3306"},
		"DB_NAME":       {value: db.Name},
		"DB_USER":       {value: db.Username},
		"DB_PASSWORD":   {value: db.Password, secret: true},
		"DB_DATABASE":   {value: db.Name},
		"DB_USERNAME":   {value: db.Username},
		"DATABASE_URL":  {value: databaseURL(db), secret: true},
	}
	for name, item := range values {
		if _, err := a.store.UpsertDomainEnvVar(r.Context(), domain.ID, name, item.value, item.secret); err != nil {
			a.renderDomainDetail(w, r, http.StatusInternalServerError, domain, "Gagal menyimpan env database.", "")
			return
		}
	}
	if err := a.applyDomainEnv(r, domain); err != nil {
		a.renderDomainDetail(w, r, http.StatusBadGateway, domain, "Database tersimpan di env, tapi gagal diterapkan: "+err.Error(), "")
		return
	}
	a.renderDomainDetail(w, r, http.StatusOK, domain, "", "Database berhasil dipasang ke environment domain.")
}

func (a *App) applyDomainEnv(r *http.Request, domain *Domain) error {
	mode := envApplyMode(domain)
	if mode == "none" {
		return nil
	}
	envVars, err := a.store.ListDomainEnvVars(r.Context(), domain.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.uploadDir, 0750); err != nil {
		return err
	}
	tempFile, err := os.CreateTemp(a.uploadDir, "mini-panel-env-")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	payload := make([]domainEnvPayloadItem, 0, len(envVars))
	for _, envVar := range envVars {
		payload = append(payload, domainEnvPayloadItem{Name: envVar.Name, Value: envVar.Value})
	}
	if err := json.NewEncoder(tempFile).Encode(payload); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Close(); err != nil {
		return err
	}
	return a.domainCtl.ApplyEnv(r.Context(), domain.Domain, tempPath, mode)
}

func envApplyMode(domain *Domain) string {
	switch domain.SiteType {
	case "go_binary", "node_next":
		return "service"
	case "php_git":
		return "dotenv-source"
	case "php":
		return "dotenv-public"
	default:
		return "none"
	}
}

type domainEnvPayloadItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func databaseURL(db *Database) string {
	u := &url.URL{
		Scheme: "mysql",
		User:   url.UserPassword(db.Username, db.Password),
		Host:   "127.0.0.1:3306",
		Path:   "/" + db.Name,
	}
	return u.String()
}
