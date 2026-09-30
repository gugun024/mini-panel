package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (a *App) handleBackups(w http.ResponseWriter, r *http.Request) {
	a.renderBackupsWithMessage(w, r, http.StatusOK, "", "")
}

func (a *App) handleDomainBackupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("domain_id"), 10, 64)
	if err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	domain, err := a.store.GetDomain(r.Context(), id)
	if err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusNotFound, "Domain tidak ditemukan.", "")
		return
	}
	a.startBackupJob(w, r, "backup_domain", domain.Domain, func(ctx context.Context, logf jobLogger) error {
		logf("creating domain backup for %s", domain.Domain)
		out, err := a.domainCtl.Backup(ctx, domain.Domain)
		if err != nil {
			return err
		}
		path, sizeBytes, err := parseBackupOutput(out)
		if err != nil {
			return err
		}
		metadata, err := domainBackupMetadata(domain)
		if err != nil {
			return err
		}
		if _, err := a.store.CreateBackup(ctx, "domain", domain.ID, domain.Domain, path, metadata, sizeBytes); err != nil {
			return err
		}
		logf("backup saved: %s", path)
		return nil
	})
}

func (a *App) handleDatabaseBackupPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	id, err := strconv.ParseInt(r.FormValue("database_id"), 10, 64)
	if err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusBadRequest, "Database tidak valid.", "")
		return
	}
	database, err := a.store.GetDatabase(r.Context(), id)
	if err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusNotFound, "Database tidak ditemukan.", "")
		return
	}
	if database.Password == "" {
		a.renderBackupsWithMessage(w, r, http.StatusBadRequest, "Database lama belum punya password tersimpan.", "")
		return
	}
	a.startBackupJob(w, r, "backup_database", database.Name, func(ctx context.Context, logf jobLogger) error {
		logf("creating database backup for %s", database.Name)
		out, err := a.dbCtl.Backup(ctx, database.Name, database.Username, database.Password)
		if err != nil {
			return err
		}
		path, sizeBytes, err := parseBackupOutput(out)
		if err != nil {
			return err
		}
		if _, err := a.store.CreateBackup(ctx, "database", database.ID, database.Name, path, "", sizeBytes); err != nil {
			return err
		}
		logf("backup saved: %s", path)
		return nil
	})
}

func (a *App) handleBackupRestorePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	backup, err := a.store.GetBackup(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.startBackupJob(w, r, "restore_"+backup.TargetType, backup.TargetName, func(ctx context.Context, logf jobLogger) error {
		logf("restoring %s backup for %s", backup.TargetType, backup.TargetName)
		switch backup.TargetType {
		case "domain":
			if err := a.domainCtl.Restore(ctx, backup.TargetName, backup.Path); err != nil {
				return err
			}
			return a.ensureRestoredDomainRecord(ctx, backup)
		case "database":
			database, err := a.store.GetDatabase(ctx, backup.TargetID)
			if err != nil {
				return fmt.Errorf("database record is required for restore: %w", err)
			}
			if database.Password == "" {
				return fmt.Errorf("database password is not stored")
			}
			return a.dbCtl.Restore(ctx, database.Name, database.Username, database.Password, backup.Path)
		default:
			return fmt.Errorf("unsupported backup type: %s", backup.TargetType)
		}
	})
}

type domainBackupMeta struct {
	UpstreamURL string `json:"upstream_url"`
	SiteType    string `json:"site_type"`
	AppPort     int    `json:"app_port"`
	AppPath     string `json:"app_path"`
	SSLEnabled  bool   `json:"ssl_enabled"`
}

func domainBackupMetadata(domain *Domain) (string, error) {
	payload, err := json.Marshal(domainBackupMeta{
		UpstreamURL: domain.UpstreamURL,
		SiteType:    domain.SiteType,
		AppPort:     domain.AppPort,
		AppPath:     domain.AppPath,
		SSLEnabled:  domain.SSLEnabled,
	})
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (a *App) ensureRestoredDomainRecord(ctx context.Context, backup *Backup) error {
	if _, err := a.store.GetDomainByName(ctx, backup.TargetName); err == nil {
		return nil
	} else if !errorsIsNoRows(err) {
		return err
	}
	if backup.Metadata == "" {
		return nil
	}
	var meta domainBackupMeta
	if err := json.Unmarshal([]byte(backup.Metadata), &meta); err != nil {
		return err
	}
	if meta.SiteType == "" || meta.UpstreamURL == "" {
		return nil
	}
	domain, err := a.store.CreateDomainRecord(ctx, backup.TargetName, meta.UpstreamURL, meta.SiteType, meta.AppPort, meta.AppPath)
	if err != nil {
		return err
	}
	if meta.SSLEnabled {
		return a.store.MarkDomainSSL(ctx, domain.ID, true)
	}
	return nil
}

func errorsIsNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func (a *App) handleBackupDeletePost(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	backup, err := a.store.GetBackup(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.domainCtl.DeleteBackup(r.Context(), backup.Path); err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus file backup: "+err.Error(), "")
		return
	}
	if _, err := a.store.DeleteBackup(r.Context(), id); err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusInternalServerError, "File backup terhapus, tapi metadata panel gagal dihapus.", "")
		return
	}
	http.Redirect(w, r, "/backups", http.StatusSeeOther)
}

func (a *App) startBackupJob(w http.ResponseWriter, r *http.Request, jobType, target string, work func(context.Context, jobLogger) error) {
	job, err := a.store.CreateJob(r.Context(), jobType, target)
	if err != nil {
		a.renderBackupsWithMessage(w, r, http.StatusInternalServerError, "Gagal membuat job backup.", "")
		return
	}
	go a.runJob(job.ID, work)
	http.Redirect(w, r, "/jobs/"+strconv.FormatInt(job.ID, 10), http.StatusSeeOther)
}

func (a *App) renderBackupsWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	databases, err := a.store.ListDatabases(r.Context())
	if err != nil {
		http.Error(w, "could not list databases", http.StatusInternalServerError)
		return
	}
	backups, err := a.store.ListBackups(r.Context())
	if err != nil {
		http.Error(w, "could not list backups", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "backups", pageData{
		Session:   sessionFromContext(r.Context()),
		Domains:   domains,
		Databases: databases,
		Backups:   backups,
		Error:     errMsg,
		Notice:    okMsg,
	})
}

func parseBackupOutput(output string) (string, int64, error) {
	var path string
	var sizeBytes int64
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "path":
			path = value
		case "size_bytes":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return "", 0, fmt.Errorf("invalid backup size")
			}
			sizeBytes = parsed
		}
	}
	if path == "" {
		return "", 0, fmt.Errorf("backup path missing from helper output")
	}
	return path, sizeBytes, nil
}
