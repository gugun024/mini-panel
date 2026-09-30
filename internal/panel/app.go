package panel

import (
	"crypto/rand"
	"errors"
	"html/template"
	"log"
	"net/http"
	"net/url"
)

type Config struct {
	Store        *Store
	DomainCtl    string
	DBCtl        string
	UseSudo      bool
	SecureCookie bool
	SessionKey   []byte
	UploadDir    string
	AppRoot      string
	AdminerURL   string
	Version      string
}

type App struct {
	store        *Store
	domainCtl    DomainController
	dbCtl        DBController
	templates    *template.Template
	sessionKey   []byte
	secureCookie bool
	loginLimiter *loginLimiter
	uploadDir    string
	appRoot      string
	adminerURL   *url.URL
	version      string
}

func New(cfg Config) (*App, error) {
	if cfg.Store == nil {
		return nil, errors.New("store is required")
	}
	sessionKey := cfg.SessionKey
	if len(sessionKey) == 0 {
		generated := make([]byte, 32)
		if _, err := rand.Read(generated); err != nil {
			return nil, err
		}
		sessionKey = generated
		log.Print("MINIPANEL_SESSION_KEY is not set; sessions will be invalid after restart")
	}

	tmpl, err := parseTemplates()
	if err != nil {
		return nil, err
	}
	var adminerURL *url.URL
	if cfg.AdminerURL != "" {
		parsed, err := url.Parse(cfg.AdminerURL)
		if err != nil {
			return nil, err
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("adminer URL must use http or https")
		}
		adminerURL = parsed
	}
	return &App{
		store:        cfg.Store,
		domainCtl:    DomainController{Path: cfg.DomainCtl, UseSudo: cfg.UseSudo},
		dbCtl:        DBController{Path: cfg.DBCtl, UseSudo: cfg.UseSudo},
		templates:    tmpl,
		sessionKey:   sessionKey,
		secureCookie: cfg.SecureCookie,
		loginLimiter: newLoginLimiter(),
		uploadDir:    uploadDir(cfg.UploadDir),
		appRoot:      appRoot(cfg.AppRoot),
		adminerURL:   adminerURL,
		version:      cfg.Version,
	}, nil
}

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler()))
	mux.HandleFunc("/", a.handleRoot)
	mux.HandleFunc("GET /login", a.handleLoginForm)
	mux.HandleFunc("POST /login", a.handleLoginPost)
	mux.HandleFunc("POST /logout", a.requireAuth(a.requireCSRF(a.handleLogoutPost)))
	mux.HandleFunc("GET /domains", a.requireAuth(a.handleDomains))
	mux.HandleFunc("POST /domains/deploy", a.requireAuth(a.requireCSRF(a.handleDomainDeployPost)))
	mux.HandleFunc("POST /domains", a.requireAuth(a.requireCSRF(a.handleDomainsPost)))
	mux.HandleFunc("POST /domains/static", a.requireAuth(a.requireCSRF(a.handleStaticDomainPost)))
	mux.HandleFunc("POST /domains/go-binary", a.requireAuth(a.requireCSRF(a.handleGoBinaryPost)))
	mux.HandleFunc("POST /domains/node-git", a.requireAuth(a.requireCSRF(a.handleNodeGitPost)))
	mux.HandleFunc("POST /domains/zip", a.requireAuth(a.requireCSRF(a.handleZipPost)))
	mux.HandleFunc("POST /domains/php-git", a.requireAuth(a.requireCSRF(a.handlePHPGitPost)))
	mux.HandleFunc("GET /domains/{id}", a.requireAuth(a.handleDomainDetail))
	mux.HandleFunc("POST /domains/{id}/service", a.requireAuth(a.requireCSRF(a.handleDomainService)))
	mux.HandleFunc("POST /domains/{id}/command", a.requireAuth(a.requireCSRF(a.handleDomainCommand)))
	mux.HandleFunc("POST /domains/{id}/ssl", a.requireAuth(a.requireCSRF(a.handleDomainSSL)))
	mux.HandleFunc("POST /domains/{id}/env", a.requireAuth(a.requireCSRF(a.handleDomainEnvPost)))
	mux.HandleFunc("POST /domains/{id}/env/{env_id}/delete", a.requireAuth(a.requireCSRF(a.handleDomainEnvDelete)))
	mux.HandleFunc("POST /domains/{id}/attach-database", a.requireAuth(a.requireCSRF(a.handleDomainAttachDatabase)))
	mux.HandleFunc("POST /domains/{id}/delete", a.requireAuth(a.requireCSRF(a.handleDomainDelete)))
	mux.HandleFunc("GET /jobs", a.requireAuth(a.handleJobs))
	mux.HandleFunc("GET /jobs/{id}", a.requireAuth(a.handleJobDetail))
	mux.HandleFunc("GET /backups", a.requireAuth(a.handleBackups))
	mux.HandleFunc("POST /backups/domain", a.requireAuth(a.requireCSRF(a.handleDomainBackupPost)))
	mux.HandleFunc("POST /backups/database", a.requireAuth(a.requireCSRF(a.handleDatabaseBackupPost)))
	mux.HandleFunc("POST /backups/{id}/restore", a.requireAuth(a.requireCSRF(a.handleBackupRestorePost)))
	mux.HandleFunc("POST /backups/{id}/delete", a.requireAuth(a.requireCSRF(a.handleBackupDeletePost)))
	mux.HandleFunc("GET /cron", a.requireAuth(a.handleCronJobs))
	mux.HandleFunc("POST /cron", a.requireAuth(a.requireCSRF(a.handleCronJobPost)))
	mux.HandleFunc("POST /cron/{id}/delete", a.requireAuth(a.requireCSRF(a.handleCronJobDelete)))
	mux.HandleFunc("GET /workers", a.requireAuth(a.handleWorkers))
	mux.HandleFunc("POST /workers", a.requireAuth(a.requireCSRF(a.handleWorkerPost)))
	mux.HandleFunc("POST /workers/{id}/service", a.requireAuth(a.requireCSRF(a.handleWorkerService)))
	mux.HandleFunc("POST /workers/{id}/delete", a.requireAuth(a.requireCSRF(a.handleWorkerDelete)))
	mux.HandleFunc("GET /monitor", a.requireAuth(a.handleMonitor))
	mux.HandleFunc("GET /settings", a.requireAuth(a.handleSettings))
	mux.HandleFunc("POST /settings/update", a.requireAuth(a.requireCSRF(a.handlePanelUpdate)))
	mux.HandleFunc("GET /firewall", a.requireAuth(a.handleFirewall))
	mux.HandleFunc("POST /firewall/enable", a.requireAuth(a.requireCSRF(a.handleFirewallEnable)))
	mux.HandleFunc("POST /firewall/allow", a.requireAuth(a.requireCSRF(a.handleFirewallAllow)))
	mux.HandleFunc("POST /firewall/delete", a.requireAuth(a.requireCSRF(a.handleFirewallDelete)))
	mux.HandleFunc("GET /databases", a.requireAuth(a.handleDatabases))
	mux.HandleFunc("POST /databases", a.requireAuth(a.requireCSRF(a.handleDatabasesPost)))
	mux.HandleFunc("GET /databases/{id}/adminer", a.requireAuth(a.handleDatabaseAdminer))
	mux.HandleFunc("POST /databases/{id}/delete", a.requireAuth(a.requireCSRF(a.handleDatabaseDelete)))
	mux.HandleFunc("GET /adminer", a.requireAuth(a.handleAdminerRedirect))
	mux.HandleFunc("/adminer/", a.requireAuth(a.handleAdminerProxy))
	mux.HandleFunc("GET /files", a.requireAuth(a.handleFiles))
	mux.HandleFunc("GET /files/{id}", a.requireAuth(a.handleFilesDomain))
	mux.HandleFunc("POST /files/{id}/save", a.requireAuth(a.requireCSRF(a.handleFileSave)))
	mux.HandleFunc("POST /files/{id}/mkdir", a.requireAuth(a.requireCSRF(a.handleFileMkdir)))
	mux.HandleFunc("POST /files/{id}/upload", a.requireAuth(a.requireCSRF(a.handleFileUpload)))
	mux.HandleFunc("POST /files/{id}/delete", a.requireAuth(a.requireCSRF(a.handleFileDelete)))
	return securityHeaders(mux)
}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}
