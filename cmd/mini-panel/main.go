package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mini-panel/internal/panel"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	command := "serve"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		command = os.Args[1]
	}

	switch command {
	case "serve":
		args := os.Args[1:]
		if len(args) > 0 && args[0] == "serve" {
			args = args[1:]
		}
		return serve(args)
	case "create-admin":
		return createAdmin(os.Args[2:])
	default:
		return fmt.Errorf("unknown command %q; use serve or create-admin", command)
	}
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", envString("MINIPANEL_ADDR", "127.0.0.1:8080"), "HTTP listen address")
	dbPath := fs.String("db", envString("MINIPANEL_DB", "mini-panel.db"), "SQLite database path")
	domainCtl := fs.String("domainctl", envString("MINIPANEL_DOMAINCTL", "/usr/local/lib/mini-panel/domainctl"), "domainctl helper path")
	dbCtl := fs.String("dbctl", envString("MINIPANEL_DBCTL", "/usr/local/lib/mini-panel/dbctl"), "dbctl helper path")
	uploadDir := fs.String("upload-dir", envString("MINIPANEL_UPLOAD_DIR", "/var/lib/mini-panel/uploads"), "upload directory")
	appRoot := fs.String("app-root", envString("MINIPANEL_APP_ROOT", "/var/lib/mini-panel/apps"), "managed application root")
	adminerURL := fs.String("adminer-url", envString("MINIPANEL_ADMINER_URL", ""), "internal Adminer URL")
	useSudo := fs.Bool("sudo", envBool("MINIPANEL_DOMAINCTL_SUDO", true), "run domainctl through sudo -n")
	secureCookie := fs.Bool("secure-cookie", envBool("MINIPANEL_SECURE_COOKIE", false), "mark session cookie secure")
	sessionKey := fs.String("session-key", envString("MINIPANEL_SESSION_KEY", ""), "session signing key")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := panel.OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	app, err := panel.New(panel.Config{
		Store:        store,
		DomainCtl:    *domainCtl,
		DBCtl:        *dbCtl,
		UseSudo:      *useSudo,
		SecureCookie: *secureCookie,
		SessionKey:   []byte(*sessionKey),
		UploadDir:    *uploadDir,
		AppRoot:      *appRoot,
		AdminerURL:   *adminerURL,
		Version:      version,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              *addr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("mini-panel listening on http://%s", *addr)
	return server.ListenAndServe()
}

func createAdmin(args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ExitOnError)
	dbPath := fs.String("db", envString("MINIPANEL_DB", "mini-panel.db"), "SQLite database path")
	username := fs.String("username", envString("MINIPANEL_ADMIN_USERNAME", "admin"), "admin username")
	password := fs.String("password", envString("MINIPANEL_ADMIN_PASSWORD", ""), "admin password; prefer MINIPANEL_ADMIN_PASSWORD")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *password == "" {
		return fmt.Errorf("admin password is required via -password or MINIPANEL_ADMIN_PASSWORD")
	}

	store, err := panel.OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.CreateAdmin(context.Background(), *username, *password); err != nil {
		return err
	}
	fmt.Printf("admin %q created\n", *username)
	return nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
