package panel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAuthRequiredForDomains(t *testing.T) {
	app := newTestApp(t, "")
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	rec := httptest.NewRecorder()

	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != "/login" {
		t.Fatalf("Location = %q, want /login", got)
	}
}

func TestLoginAndCSRFProtection(t *testing.T) {
	app := newTestApp(t, "")
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}

	cookie := login(t, app, "admin", "very-long-password")

	req := httptest.NewRequest(http.MethodPost, "/domains", strings.NewReader(url.Values{
		"domain":       {"example.com"},
		"upstream_url": {"http://127.0.0.1:3000"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestLoginRequiresCSRF(t *testing.T) {
	app := newTestApp(t, "")
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	rec := postForm(t, app, "/login", url.Values{
		"username": {"admin"},
		"password": {"very-long-password"},
	}, nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if !strings.Contains(rec.Body.String(), `name="csrf_token"`) {
		t.Fatalf("fresh CSRF token not found in body: %s", rec.Body.String())
	}
}

func TestDomainAddDeleteWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	form := url.Values{
		"csrf_token":   {csrf},
		"domain":       {"Example.com"},
		"upstream_url": {"http://127.0.0.1:3000"},
	}
	rec := postForm(t, app, "/domains", form, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Domain != "example.com" || domains[0].UpstreamURL != "http://127.0.0.1:3000" {
		t.Fatalf("unexpected domains: %+v", domains)
	}

	rec = postForm(t, app, fmt.Sprintf("/domains/%d/delete", domains[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err = app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 0 {
		t.Fatalf("expected no domains, got %+v", domains)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	want := []string{
		"add example.com http://127.0.0.1:3000",
		"delete example.com",
	}
	if len(lines) != len(want) {
		t.Fatalf("log lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestStaticDomainCreateWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/static", url.Values{
		"csrf_token": {csrf},
		"domain":     {"Site.Example.com"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Domain != "site.example.com" || domains[0].SiteType != "static" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	if want := filepath.Join(app.appRoot, "site.example.com", "public"); domains[0].AppPath != want || domains[0].UpstreamURL != want {
		t.Fatalf("unexpected app paths: %+v, want %q", domains[0], want)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logData)), "create-static site.example.com"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestUnifiedDeployStaticDomainWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/deploy", url.Values{
		"csrf_token": {csrf},
		"runtime":    {"static"},
		"source":     {"blank"},
		"domain":     {"site.example.com"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); !strings.HasPrefix(got, "/jobs/") {
		t.Fatalf("Location = %q, want /jobs/<id>", got)
	}
	waitForJobs(t, app)
	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "static" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logData)), "create-static site.example.com"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	jobs, err := app.store.ListJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != "success" || !strings.Contains(jobs[0].Log, "job finished") {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
}

func TestGoBinaryUploadWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "csrf_token", csrf)
	mustWriteField(t, writer, "domain", "api.example.com")
	mustWriteField(t, writer, "port", "19001")
	fileWriter, err := writer.CreateFormFile("binary", "api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("#!/bin/sh\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/domains/go-binary", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "go_binary" || domains[0].AppPort != 19001 {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(logData))
	if !strings.HasPrefix(line, "install-go api.example.com ") || !strings.HasSuffix(line, " 19001") {
		t.Fatalf("unexpected domainctl log: %q", line)
	}
}

func TestNodeGitDeployWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/node-git", url.Values{
		"csrf_token":      {csrf},
		"domain":          {"web.example.com"},
		"git_url":         {"https://github.com/example/app.git"},
		"branch":          {"main"},
		"port":            {"19002"},
		"install_command": {"npm install"},
		"build_command":   {"npm run build"},
		"start_command":   {"npm start"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "node_next" || domains[0].AppPort != 19002 {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(logData))
	want := "install-node web.example.com https://github.com/example/app.git main 19002 npm install npm run build npm start"
	if line != want {
		t.Fatalf("log = %q, want %q", line, want)
	}
}

func TestNodeZipDeployWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "csrf_token", csrf)
	mustWriteField(t, writer, "runtime", "node")
	mustWriteField(t, writer, "source", "upload")
	mustWriteField(t, writer, "domain", "zipnode.example.com")
	mustWriteField(t, writer, "port", "19003")
	mustWriteField(t, writer, "install_command", "npm install")
	mustWriteField(t, writer, "build_command", "npm run build")
	mustWriteField(t, writer, "start_command", "npm start")
	fileWriter, err := writer.CreateFormFile("archive", "app.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("fake zip content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/domains/deploy", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)
	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "node_next" || domains[0].AppPort != 19003 {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(logData))
	if !strings.HasPrefix(line, "install-node-zip zipnode.example.com ") || !strings.HasSuffix(line, " 19003 npm install npm run build npm start") {
		t.Fatalf("unexpected domainctl log: %q", line)
	}
}

func TestNodeGitRejectsUnsafeGitURL(t *testing.T) {
	app := newTestApp(t, "")
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/node-git", url.Values{
		"csrf_token": {csrf},
		"domain":     {"web.example.com"},
		"git_url":    {"https://github.com/example/app.git;rm"},
		"branch":     {"main"},
		"port":       {"19002"},
	}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestStaticZipUploadWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "csrf_token", csrf)
	mustWriteField(t, writer, "site_type", "static")
	mustWriteField(t, writer, "domain", "static.example.com")
	fileWriter, err := writer.CreateFormFile("archive", "site.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("fake zip content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/domains/zip", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "static" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(logData))
	if !strings.HasPrefix(line, "install-zip static static.example.com ") {
		t.Fatalf("unexpected domainctl log: %q", line)
	}
}

func TestPHPGitDeployWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/php-git", url.Values{
		"csrf_token":      {csrf},
		"domain":          {"laravel.example.com"},
		"git_url":         {"https://github.com/example/laravel.git"},
		"branch":          {"main"},
		"public_dir":      {"public"},
		"install_command": {"composer install --no-dev --optimize-autoloader"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].SiteType != "php_git" {
		t.Fatalf("unexpected domains: %+v", domains)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "install-php-git laravel.example.com https://github.com/example/laravel.git main public composer install --no-dev --optimize-autoloader"
	if got := strings.TrimSpace(string(logData)); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestIssueSSLWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "ssl.example.com", "http://127.0.0.1:3000", "reverse_proxy", 0, ""); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/domains/1/ssl", url.Values{
		"csrf_token": {csrf},
		"email":      {"admin@example.com"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(logData)), "issue-ssl ssl.example.com admin@example.com"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	domain, err := app.store.GetDomain(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !domain.SSLEnabled || domain.SSLUpdatedAt == nil {
		t.Fatalf("domain SSL status was not stored: %+v", domain)
	}
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "HTTPS") {
		t.Fatalf("domains page missing HTTPS badge: %s", rec.Body.String())
	}
}

func TestDomainDetailAndServiceControls(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "svc.example.com", "http://127.0.0.1:19001", "node_next", 19001, filepath.Join(app.appRoot, "svc.example.com", "source")); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")

	req := httptest.NewRequest(http.MethodGet, "/domains/1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Service") || !strings.Contains(body, "active") || !strings.Contains(body, "app log line") || !strings.Contains(body, "Command Runner") {
		t.Fatalf("detail body missing service/log data: %s", body)
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(body)[1]

	rec = postForm(t, app, "/domains/1/service", url.Values{
		"csrf_token": {csrf},
		"action":     {"restart"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("service status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "service restart svc.example.com") {
		t.Fatalf("service command not logged: %s", string(logData))
	}

	rec = postForm(t, app, "/domains/1/command", url.Values{
		"csrf_token": {csrf},
		"command":    {"npm run migrate"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("command status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)
	logData, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "run-command svc.example.com") || !strings.Contains(string(logData), "npm run migrate") {
		t.Fatalf("run-command not logged: %s", string(logData))
	}
	jobs, err := app.store.ListJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) == 0 || !strings.Contains(jobs[0].Log, "command output") {
		t.Fatalf("command output missing from job log: %+v", jobs)
	}
}

func TestDomainAttachDatabaseAppliesEnv(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "svc.example.com", "http://127.0.0.1:19001", "node_next", 19001, filepath.Join(app.appRoot, "svc.example.com", "source")); err != nil {
		t.Fatal(err)
	}
	db, err := app.store.CreateDatabase(context.Background(), "app_db", "app_user", "database-password-123")
	if err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")

	req := httptest.NewRequest(http.MethodGet, "/domains/1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())[1]

	rec = postForm(t, app, "/domains/1/attach-database", url.Values{
		"csrf_token":  {csrf},
		"database_id": {fmt.Sprintf("%d", db.ID)},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("attach status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	envVars, err := app.store.ListDomainEnvVars(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	envMap := map[string]DomainEnvVar{}
	for _, envVar := range envVars {
		envMap[envVar.Name] = envVar
	}
	if envMap["DB_NAME"].Value != "app_db" || envMap["DB_USER"].Value != "app_user" || envMap["DB_PASSWORD"].Value != "database-password-123" || !envMap["DB_PASSWORD"].IsSecret {
		t.Fatalf("unexpected env vars: %+v", envMap)
	}
	if !strings.Contains(envMap["DATABASE_URL"].Value, "mysql://app_user:database-password-123@127.0.0.1:3306/app_db") {
		t.Fatalf("unexpected DATABASE_URL: %q", envMap["DATABASE_URL"].Value)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "apply-env svc.example.com ") || !strings.Contains(string(logData), " service") {
		t.Fatalf("apply-env command not logged: %s", string(logData))
	}
	if strings.Contains(rec.Body.String(), "database-password-123") {
		t.Fatalf("secret password leaked in response body")
	}
}

func TestDomainEnvSupportsMultilineSecret(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "svc.example.com", "http://127.0.0.1:19001", "node_next", 19001, filepath.Join(app.appRoot, "svc.example.com", "source")); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")

	req := httptest.NewRequest(http.MethodGet, "/domains/1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())[1]
	key := "-----BEGIN PRIVATE KEY-----\r\nline-one\r\nline-two\r\n-----END PRIVATE KEY-----"
	normalizedKey := "-----BEGIN PRIVATE KEY-----\nline-one\nline-two\n-----END PRIVATE KEY-----"

	rec = postForm(t, app, "/domains/1/env", url.Values{
		"csrf_token": {csrf},
		"name":       {"PRIVATE_KEY"},
		"value":      {key},
		"is_secret":  {"on"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("env status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	envVars, err := app.store.ListDomainEnvVars(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(envVars) != 1 || envVars[0].Name != "PRIVATE_KEY" || envVars[0].Value != normalizedKey || !envVars[0].IsSecret {
		t.Fatalf("unexpected env vars: %+v", envVars)
	}
	if strings.Contains(rec.Body.String(), "BEGIN PRIVATE KEY") {
		t.Fatalf("secret multiline value leaked in response body")
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "apply-env svc.example.com ") || !strings.Contains(string(logData), " service") {
		t.Fatalf("apply-env command not logged: %s", string(logData))
	}
}

func TestFileManagerEditUploadMkdirDelete(t *testing.T) {
	app := newTestApp(t, "")
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "files.example.com", "http://127.0.0.1:3000", "static", 0, ""); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(app.appRoot, "files.example.com", "public")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(root, "index.html")
	if err := os.WriteFile(indexPath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromFilesPage(t, app, cookie, "/files/1")

	rec := postForm(t, app, "/files/1/mkdir", url.Values{
		"csrf_token": {csrf},
		"path":       {"."},
		"name":       {"assets"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("mkdir status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "assets")); err != nil {
		t.Fatal(err)
	}

	rec = postForm(t, app, "/files/1/save", url.Values{
		"csrf_token": {csrf},
		"path":       {"index.html"},
		"content":    {"updated"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Fatalf("index content = %q, want updated", string(data))
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	mustWriteField(t, writer, "csrf_token", csrf)
	mustWriteField(t, writer, "path", ".")
	fileWriter, err := writer.CreateFormFile("file", "app.css")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("body{}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/files/1/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("upload status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "app.css")); err != nil {
		t.Fatal(err)
	}

	rec = postForm(t, app, "/files/1/delete", url.Values{
		"csrf_token": {csrf},
		"path":       {"app.css"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "app.css")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uploaded file still exists or stat failed with unexpected error: %v", err)
	}
}

func TestFileManagerRejectsTraversal(t *testing.T) {
	app := newTestApp(t, "")
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "files.example.com", "http://127.0.0.1:3000", "static", 0, ""); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(app.appRoot, "files.example.com", "public")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromFilesPage(t, app, cookie, "/files/1")

	rec := postForm(t, app, "/files/1/save", url.Values{
		"csrf_token": {csrf},
		"path":       {"../secret.txt"},
		"content":    {"bad"},
	}, cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func newTestApp(t *testing.T, domainCtl string) *App {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "mini-panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if domainCtl == "" {
		domainCtl = buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "domainctl.log"))
	}
	app, err := New(Config{
		Store:      store,
		DomainCtl:  domainCtl,
		DBCtl:      buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "dbctl.log")),
		UseSudo:    false,
		SessionKey: []byte("test-session-key-that-is-long-enough"),
		UploadDir:  filepath.Join(t.TempDir(), "uploads"),
		AppRoot:    filepath.Join(t.TempDir(), "apps"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func login(t *testing.T, app *App, username, password string) *http.Cookie {
	t.Helper()
	csrf := loginCSRF(t, app)
	rec := postForm(t, app, "/login", url.Values{
		"csrf_token": {csrf},
		"username":   {username},
		"password":   {password},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("session cookie not set")
	return nil
}

func loginCSRF(t *testing.T, app *App) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login form status = %d, want %d", rec.Code, http.StatusOK)
	}
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	matches := re.FindStringSubmatch(rec.Body.String())
	if len(matches) != 2 {
		t.Fatalf("login csrf token not found in body: %s", rec.Body.String())
	}
	return matches[1]
}

func csrfFromDomainsPage(t *testing.T, app *App, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/domains", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("domains status = %d, want %d", rec.Code, http.StatusOK)
	}
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	matches := re.FindStringSubmatch(rec.Body.String())
	if len(matches) != 2 {
		t.Fatalf("csrf token not found in body: %s", rec.Body.String())
	}
	return matches[1]
}

func csrfFromFilesPage(t *testing.T, app *App, cookie *http.Cookie, target string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("files status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	re := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)
	matches := re.FindStringSubmatch(rec.Body.String())
	if len(matches) != 2 {
		t.Fatalf("csrf token not found in body: %s", rec.Body.String())
	}
	return matches[1]
}

func postForm(t *testing.T, app *App, target string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	return rec
}

func mustWriteField(t *testing.T, writer *multipart.Writer, name, value string) {
	t.Helper()
	if err := writer.WriteField(name, value); err != nil {
		t.Fatal(err)
	}
}

func waitForJobs(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := app.store.ListJobs(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) > 0 && jobs[0].Status != "queued" && jobs[0].Status != "running" {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for job")
}

func TestDatabaseCreateDeleteWithFakeDBCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "dbctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	store, err := OpenStore(filepath.Join(t.TempDir(), "mini-panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app, err := New(Config{
		Store:      store,
		DomainCtl:  buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "domainctl.log")),
		DBCtl:      fake,
		UseSudo:    false,
		SessionKey: []byte("test-session-key-that-is-long-enough"),
		UploadDir:  filepath.Join(t.TempDir(), "uploads"),
		AppRoot:    filepath.Join(t.TempDir(), "apps"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	req := httptest.NewRequest(http.MethodGet, "/databases", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("databases status = %d, want %d", rec.Code, http.StatusOK)
	}
	csrf := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(rec.Body.String())[1]
	rec = postForm(t, app, "/databases", url.Values{
		"csrf_token": {csrf},
		"name":       {"app_db"},
		"username":   {"app_user"},
		"password":   {"database-password-123"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	dbs, err := app.store.ListDatabases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(dbs) != 1 || dbs[0].Name != "app_db" || dbs[0].Username != "app_user" || dbs[0].Password != "database-password-123" {
		t.Fatalf("unexpected dbs: %+v", dbs)
	}
	rec = postForm(t, app, fmt.Sprintf("/databases/%d/delete", dbs[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(logData)), "\n")
	want := []string{
		"create app_db app_user database-password-123",
		"delete app_db app_user",
	}
	if len(lines) != len(want) {
		t.Fatalf("log lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestDomainBackupRestoreDeleteWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "site.example.com", filepath.Join(app.appRoot, "site.example.com", "public"), "static", 0, filepath.Join(app.appRoot, "site.example.com", "public")); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	req := httptest.NewRequest(http.MethodGet, "/backups", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backups status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = postForm(t, app, "/backups/domain", url.Values{
		"csrf_token": {csrf},
		"domain_id":  {"1"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("backup status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)
	backups, err := app.store.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].TargetType != "domain" || backups[0].TargetName != "site.example.com" || backups[0].Path == "" || backups[0].SizeBytes != 123 {
		t.Fatalf("unexpected backups: %+v", backups)
	}
	if _, err := app.store.DeleteDomain(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	rec = postForm(t, app, fmt.Sprintf("/backups/%d/restore", backups[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("restore status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)
	domains, err := app.store.ListDomains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Domain != "site.example.com" || domains[0].SiteType != "static" {
		t.Fatalf("restore did not recreate domain record: %+v", domains)
	}

	rec = postForm(t, app, fmt.Sprintf("/backups/%d/delete", backups[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}

	backups, err = app.store.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("expected no backups, got %+v", backups)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "backup site.example.com") ||
		!strings.Contains(logText, "restore site.example.com /var/lib/mini-panel/backups/mini-panel-test-backup.tar.gz") ||
		!strings.Contains(logText, "delete-backup /var/lib/mini-panel/backups/mini-panel-test-backup.tar.gz") {
		t.Fatalf("backup commands missing from log: %s", logText)
	}
}

func TestDatabaseBackupRestoreWithFakeDBCtl(t *testing.T) {
	dbLogPath := filepath.Join(t.TempDir(), "dbctl.log")
	dbFake := buildFakeDomainCtl(t, dbLogPath)
	store, err := OpenStore(filepath.Join(t.TempDir(), "mini-panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app, err := New(Config{
		Store:      store,
		DomainCtl:  buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "domainctl.log")),
		DBCtl:      dbFake,
		UseSudo:    false,
		SessionKey: []byte("test-session-key-that-is-long-enough"),
		UploadDir:  filepath.Join(t.TempDir(), "uploads"),
		AppRoot:    filepath.Join(t.TempDir(), "apps"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDatabase(context.Background(), "app_db", "app_user", "database-password-123"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	rec := postForm(t, app, "/backups/database", url.Values{
		"csrf_token":  {csrf},
		"database_id": {"1"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("backup status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)
	backups, err := app.store.ListBackups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].TargetType != "database" || backups[0].TargetName != "app_db" {
		t.Fatalf("unexpected backups: %+v", backups)
	}

	rec = postForm(t, app, fmt.Sprintf("/backups/%d/restore", backups[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("restore status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	waitForJobs(t, app)

	logData, err := os.ReadFile(dbLogPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "backup app_db app_user database-password-123") ||
		!strings.Contains(logText, "restore app_db app_user database-password-123 /var/lib/mini-panel/backups/mini-panel-test-backup.tar.gz") {
		t.Fatalf("database backup commands missing from log: %s", logText)
	}
}

func TestCronJobCreateDeleteWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(app.appRoot, "site.example.com", "source")
	if _, err := app.store.CreateDomainRecord(context.Background(), "site.example.com", "http://127.0.0.1:3000", "node_next", 3000, appPath); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	req := httptest.NewRequest(http.MethodGet, "/cron", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cron status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = postForm(t, app, "/cron", url.Values{
		"csrf_token": {csrf},
		"domain_id":  {"1"},
		"schedule":   {"*/5 * * * *"},
		"command":    {"npm run task"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	cronJobs, err := app.store.ListCronJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cronJobs) != 1 || cronJobs[0].DomainName != "site.example.com" || cronJobs[0].Schedule != "*/5 * * * *" || cronJobs[0].Command != "npm run task" {
		t.Fatalf("unexpected cron jobs: %+v", cronJobs)
	}

	rec = postForm(t, app, fmt.Sprintf("/cron/%d/delete", cronJobs[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	cronJobs, err = app.store.ListCronJobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cronJobs) != 0 {
		t.Fatalf("expected no cron jobs, got %+v", cronJobs)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "cron-create 1 site.example.com */5 * * * * "+appPath+" npm run task") ||
		!strings.Contains(logText, "cron-delete 1 site.example.com") {
		t.Fatalf("cron commands missing from log: %s", logText)
	}
}

func TestWorkerCreateServiceDeleteWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(app.appRoot, "site.example.com", "source")
	if _, err := app.store.CreateDomainRecord(context.Background(), "site.example.com", "http://127.0.0.1:3000", "node_next", 3000, appPath); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	req := httptest.NewRequest(http.MethodGet, "/workers", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workers status = %d, want %d", rec.Code, http.StatusOK)
	}

	rec = postForm(t, app, "/workers", url.Values{
		"csrf_token": {csrf},
		"domain_id":  {"1"},
		"name":       {"queue"},
		"command":    {"npm run worker"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	workers, err := app.store.ListWorkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 || workers[0].DomainName != "site.example.com" || workers[0].Name != "queue" || workers[0].Command != "npm run worker" {
		t.Fatalf("unexpected workers: %+v", workers)
	}

	rec = postForm(t, app, fmt.Sprintf("/workers/%d/service", workers[0].ID), url.Values{
		"csrf_token": {csrf},
		"action":     {"stop"},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("service status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	worker, err := app.store.GetWorker(context.Background(), workers[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if worker.Status != "stopped" {
		t.Fatalf("worker status = %q, want stopped", worker.Status)
	}

	rec = postForm(t, app, fmt.Sprintf("/workers/%d/delete", workers[0].ID), url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete status = %d, want %d; body: %s", rec.Code, http.StatusSeeOther, rec.Body.String())
	}
	workers, err = app.store.ListWorkers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 0 {
		t.Fatalf("expected no workers, got %+v", workers)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	if !strings.Contains(logText, "worker-create 1 site.example.com queue "+appPath+" npm run worker") ||
		!strings.Contains(logText, "worker-service stop 1 site.example.com queue") ||
		!strings.Contains(logText, "worker-delete 1 site.example.com queue") {
		t.Fatalf("worker commands missing from log: %s", logText)
	}
}

func TestMonitorSettingsAndFirewallWithFakeDomainCtl(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "domainctl.log")
	fake := buildFakeDomainCtl(t, logPath)
	app := newTestApp(t, fake)
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDomainRecord(context.Background(), "svc.example.com", "http://127.0.0.1:3000", "node_next", 3000, filepath.Join(app.appRoot, "svc.example.com", "source")); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")
	csrf := csrfFromDomainsPage(t, app, cookie)

	for _, target := range []string{"/monitor", "/settings", "/firewall"} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		app.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d; body: %s", target, rec.Code, http.StatusOK, rec.Body.String())
		}
	}

	rec := postForm(t, app, "/settings/update", url.Values{
		"csrf_token": {csrf},
		"version":    {"latest"},
		"repo_url":   {"https://github.com/Gugun09/mini-panel"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings update status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = postForm(t, app, "/firewall/enable", url.Values{
		"csrf_token": {csrf},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("firewall enable status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = postForm(t, app, "/firewall/allow", url.Values{
		"csrf_token": {csrf},
		"port":       {"8080"},
		"protocol":   {"tcp"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("firewall allow status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = postForm(t, app, "/firewall/delete", url.Values{
		"csrf_token": {csrf},
		"port":       {"8080"},
		"protocol":   {"tcp"},
	}, cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("firewall delete status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(logData)
	for _, want := range []string{
		"panel-update latest https://github.com/Gugun09/mini-panel",
		"firewall-enable",
		"firewall-allow 8080 tcp",
		"firewall-delete 8080 tcp",
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("expected %q in log: %s", want, logText)
		}
	}
}

func TestDatabaseAdminerLinkAndProxy(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "adminer backend %s %s", r.Method, r.URL.String())
	}))
	t.Cleanup(backend.Close)

	store, err := OpenStore(filepath.Join(t.TempDir(), "mini-panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app, err := New(Config{
		Store:      store,
		DomainCtl:  buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "domainctl.log")),
		DBCtl:      buildFakeDomainCtl(t, filepath.Join(t.TempDir(), "dbctl.log")),
		UseSudo:    false,
		SessionKey: []byte("test-session-key-that-is-long-enough"),
		UploadDir:  filepath.Join(t.TempDir(), "uploads"),
		AppRoot:    filepath.Join(t.TempDir(), "apps"),
		AdminerURL: backend.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.CreateAdmin(context.Background(), "admin", "very-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := app.store.CreateDatabase(context.Background(), "app_db", "app_user", "database-password-123"); err != nil {
		t.Fatal(err)
	}
	cookie := login(t, app, "admin", "very-long-password")

	req := httptest.NewRequest(http.MethodGet, "/databases", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("databases status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "/databases/1/adminer") {
		t.Fatalf("adminer link missing: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/databases/1/adminer", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("adminer redirect status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/adminer/?") || !strings.Contains(location, "username=app_user") || !strings.Contains(location, "db=app_db") || strings.Contains(location, "database-password-123") {
		t.Fatalf("unexpected adminer redirect: %q", location)
	}

	req = httptest.NewRequest(http.MethodGet, "/adminer/?db=app_db", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("adminer proxy status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "adminer backend GET /?db=app_db" {
		t.Fatalf("adminer proxy body = %q", got)
	}
}

func TestDomainCtlServiceArgumentCount(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "domainctl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `[[ $# -eq 3 ]] || die "usage: domainctl service <status|start|stop|restart> <domain>"`) {
		t.Fatal("domainctl service command must accept exactly 3 arguments: service <action> <domain>")
	}
}

func TestDomainCtlNodeZipCleansFailedDeploy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "domainctl"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "local deploy_complete=0") ||
		!strings.Contains(content, "trap 'if [[ \"$deploy_complete\" != \"1\" ]]; then cleanup_app_service \"$domain\" >/dev/null 2>&1 || true; fi' RETURN") ||
		!strings.Contains(content, "deploy_complete=1") {
		t.Fatal("domainctl install-node-zip must cleanup partial app directory/service on failed deploy")
	}
}

func TestUninstallScriptsHaveSafetyGuards(t *testing.T) {
	bootstrap, err := os.ReadFile(filepath.Join("..", "..", "uninstall.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bootstrapText := string(bootstrap)
	if !strings.Contains(bootstrapText, "scripts/uninstall-ubuntu.sh") ||
		!strings.Contains(bootstrapText, "UNINSTALL_ARGS") {
		t.Fatal("uninstall bootstrap must download source and pass args to scripts/uninstall-ubuntu.sh")
	}

	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "uninstall-ubuntu.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		`die "refusing non-interactive uninstall without --yes"`,
		`grep -Fxq "# managed-by: mini-panel" "$file"`,
		`grep -Fxq "# managed-by: mini-panel-installer" /etc/nginx/sites-available/mini-panel-admin.conf`,
		`SELECT name, username FROM databases;`,
		"DROP DATABASE IF EXISTS \\`$db_name\\`;",
		`DROP USER IF EXISTS '$db_user'@'localhost';`,
		`[[ "$REMOVE_PACKAGES" == "true" ]] || return 0`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("uninstall safety guard missing %q", want)
		}
	}
}

func buildFakeDomainCtl(t *testing.T, logPath string) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "fake-domainctl.go")
	bin := filepath.Join(dir, "fake-domainctl")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	code := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func writeLog(line string) {
	f, err := os.OpenFile(%q, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "missing command")
		os.Exit(2)
	}
	if len(os.Args) >= 4 && os.Args[1] == "service" && os.Args[2] == "status" {
		fmt.Println("active")
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "ssl-status" {
		fmt.Println("notAfter=Jun 01 00:00:00 2026 GMT")
		return
	}
	if len(os.Args) >= 5 && os.Args[1] == "run-command" {
		fmt.Println("command output")
	}
	if len(os.Args) >= 4 && os.Args[1] == "logs" && os.Args[2] == "nginx" {
		fmt.Println("nginx log line")
		return
	}
	if len(os.Args) >= 4 && os.Args[1] == "logs" && os.Args[2] == "app" {
		fmt.Println("app log line")
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "firewall-status" {
		fmt.Println("Status: active")
		fmt.Println("[ 1] 22/tcp ALLOW IN Anywhere")
		return
	}
	line := strings.Join(os.Args[1:], " ") + "\n"
	writeLog(line)
	if len(os.Args) >= 2 && os.Args[1] == "backup" {
		fmt.Println("path=/var/lib/mini-panel/backups/mini-panel-test-backup.tar.gz")
		fmt.Println("size_bytes=123")
	}
}
`, logPath)
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", bin, source)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake domainctl: %v\n%s", err, out.String())
	}
	return bin
}
