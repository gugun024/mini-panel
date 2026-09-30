package panel

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) handleDomainsPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	upstream, err := NormalizeUpstream(r.FormValue("upstream_url"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upstream harus http://127.0.0.1:<port> atau http://localhost:<port>.", "")
		return
	}
	if err := a.domainCtl.Add(r.Context(), domain, upstream); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal membuat config Nginx: "+err.Error(), "")
		return
	}
	if _, err := a.store.CreateDomain(r.Context(), domain, upstream); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) handleDomainDeployPost(w http.ResponseWriter, r *http.Request) {
	deployType := strings.TrimSpace(r.FormValue("deploy_type"))
	if deployType == "" {
		deployType = deployTypeFromRuntimeSource(r.FormValue("runtime"), r.FormValue("source"))
	}
	switch deployType {
	case "static_site":
		a.handleStaticDomainJob(w, r)
	case "reverse_proxy":
		a.handleReverseProxyJob(w, r)
	case "node_git":
		a.handleNodeGitJob(w, r)
	case "node_zip":
		a.handleNodeZipJob(w, r)
	case "go_binary":
		a.handleGoBinaryJob(w, r)
	case "static_zip":
		setFormValue(r, "site_type", "static")
		a.handleZipJob(w, r)
	case "php_zip":
		setFormValue(r, "site_type", "php")
		a.handleZipJob(w, r)
	case "php_git":
		a.handlePHPGitJob(w, r)
	default:
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Tipe deployment tidak valid.", "")
	}
}

func deployTypeFromRuntimeSource(runtimeInput, sourceInput string) string {
	runtime := strings.TrimSpace(runtimeInput)
	source := strings.TrimSpace(sourceInput)
	switch runtime + ":" + source {
	case "static:blank":
		return "static_site"
	case "static:upload":
		return "static_zip"
	case "php:upload":
		return "php_zip"
	case "php:git":
		return "php_git"
	case "node:git":
		return "node_git"
	case "node:upload":
		return "node_zip"
	case "go:binary":
		return "go_binary"
	case "proxy:port":
		return "reverse_proxy"
	default:
		return ""
	}
}

func setFormValue(r *http.Request, key, value string) {
	if r.Form != nil {
		r.Form.Set(key, value)
	}
	if r.PostForm != nil {
		r.PostForm.Set(key, value)
	}
	if r.MultipartForm != nil {
		r.MultipartForm.Value[key] = []string{value}
	}
}

func (a *App) handleStaticDomainJob(w http.ResponseWriter, r *http.Request) {
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	a.createDeployJob(w, r, "static_site", domain, func(ctx context.Context, logf jobLogger) error {
		logf("creating static site for %s", domain)
		out, err := a.domainCtl.RunOutput(ctx, "create-static", domain)
		logJobOutput(logf, out)
		if err != nil {
			return err
		}
		appPath := filepath.Join(a.appRoot, domain, "public")
		_, err = a.store.CreateDomainRecord(ctx, domain, appPath, "static", 0, appPath)
		if err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handleReverseProxyJob(w http.ResponseWriter, r *http.Request) {
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	upstream, err := NormalizeUpstream(r.FormValue("upstream_url"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upstream harus http://127.0.0.1:<port> atau http://localhost:<port>.", "")
		return
	}
	a.createDeployJob(w, r, "reverse_proxy", domain, func(ctx context.Context, logf jobLogger) error {
		logf("creating reverse proxy %s -> %s", domain, upstream)
		out, err := a.domainCtl.RunOutput(ctx, "add", domain, upstream)
		logJobOutput(logf, out)
		if err != nil {
			return err
		}
		if _, err := a.store.CreateDomain(ctx, domain, upstream); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handleGoBinaryJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upload tidak valid atau terlalu besar.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	port, err := NormalizeAppPort(r.FormValue("port"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Port harus di antara 1024 dan 65535.", "")
		return
	}
	if !a.portAvailable(port) {
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Port sudah digunakan di VPS.", "")
		return
	}
	uploadPath, err := a.saveUploadedFile(r, "binary", "mini-panel-go-"+domain+"-")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	a.createDeployJob(w, r, "go_binary", domain, func(ctx context.Context, logf jobLogger) error {
		logf("installing Go binary on port %d", port)
		out, err := a.domainCtl.RunOutput(ctx, "install-go", domain, uploadPath, strconv.Itoa(port))
		logJobOutput(logf, out)
		if err != nil {
			_ = os.Remove(uploadPath)
			return err
		}
		upstream := "http://127.0.0.1:" + strconv.Itoa(port)
		if _, err := a.store.CreateDomainRecord(ctx, domain, upstream, "go_binary", port, filepath.Join(a.appRoot, domain, "app")); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handleNodeGitJob(w http.ResponseWriter, r *http.Request) {
	domain, gitURL, branch, port, installCommand, buildCommand, startCommand, ok := a.parseNodeGitDeploy(w, r)
	if !ok {
		return
	}
	a.createDeployJob(w, r, "node_git", domain, func(ctx context.Context, logf jobLogger) error {
		logf("deploying Node.js app from %s", gitURL)
		out, err := a.domainCtl.RunOutput(ctx, "install-node", domain, gitURL, branch, strconv.Itoa(port), installCommand, buildCommand, startCommand)
		logJobOutput(logf, out)
		if err != nil {
			return err
		}
		upstream := "http://127.0.0.1:" + strconv.Itoa(port)
		if _, err := a.store.CreateDomainRecord(ctx, domain, upstream, "node_next", port, filepath.Join(a.appRoot, domain, "source")); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handleNodeZipJob(w http.ResponseWriter, r *http.Request) {
	domain, port, installCommand, buildCommand, startCommand, uploadPath, ok := a.parseNodeZipDeploy(w, r)
	if !ok {
		return
	}
	a.createDeployJob(w, r, "node_zip", domain, func(ctx context.Context, logf jobLogger) error {
		logf("deploying Node.js app from uploaded ZIP")
		out, err := a.domainCtl.RunOutput(ctx, "install-node-zip", domain, uploadPath, strconv.Itoa(port), installCommand, buildCommand, startCommand)
		logJobOutput(logf, out)
		if err != nil {
			_ = os.Remove(uploadPath)
			return err
		}
		upstream := "http://127.0.0.1:" + strconv.Itoa(port)
		if _, err := a.store.CreateDomainRecord(ctx, domain, upstream, "node_next", port, filepath.Join(a.appRoot, domain, "source")); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handleZipJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upload tidak valid atau terlalu besar.", "")
		return
	}
	siteType := strings.TrimSpace(r.FormValue("site_type"))
	if siteType != "static" && siteType != "php" {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Tipe site harus Static atau PHP.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	uploadPath, err := a.saveUploadedFile(r, "archive", "mini-panel-zip-"+domain+"-")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	a.createDeployJob(w, r, siteType+"_zip", domain, func(ctx context.Context, logf jobLogger) error {
		logf("installing %s ZIP", siteType)
		out, err := a.domainCtl.RunOutput(ctx, "install-zip", siteType, domain, uploadPath)
		logJobOutput(logf, out)
		if err != nil {
			_ = os.Remove(uploadPath)
			return err
		}
		appPath := filepath.Join(a.appRoot, domain, "public")
		if _, err := a.store.CreateDomainRecord(ctx, domain, appPath, siteType, 0, appPath); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func (a *App) handlePHPGitJob(w http.ResponseWriter, r *http.Request) {
	domain, gitURL, branch, publicDir, installCommand, ok := a.parsePHPGitDeploy(w, r)
	if !ok {
		return
	}
	a.createDeployJob(w, r, "php_git", domain, func(ctx context.Context, logf jobLogger) error {
		logf("deploying PHP app from %s", gitURL)
		out, err := a.domainCtl.RunOutput(ctx, "install-php-git", domain, gitURL, branch, publicDir, installCommand)
		logJobOutput(logf, out)
		if err != nil {
			return err
		}
		appPath := filepath.Join(a.appRoot, domain, "source", publicDir)
		if _, err := a.store.CreateDomainRecord(ctx, domain, appPath, "php_git", 0, appPath); err != nil {
			_ = a.domainCtl.Delete(ctx, domain)
			return err
		}
		logf("domain record created")
		return nil
	})
}

func logJobOutput(logf jobLogger, out string) {
	out = strings.TrimSpace(out)
	if out != "" {
		logf("%s", out)
	}
}

func (a *App) handleStaticDomainPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	if err := a.domainCtl.CreateStatic(r.Context(), domain); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal membuat website: "+err.Error(), "")
		return
	}
	appPath := filepath.Join(a.appRoot, domain, "public")
	if _, err := a.store.CreateDomainRecord(r.Context(), domain, appPath, "static", 0, appPath); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) handleGoBinaryPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upload tidak valid atau terlalu besar.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	port, err := NormalizeAppPort(r.FormValue("port"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Port harus di antara 1024 dan 65535.", "")
		return
	}
	if !a.portAvailable(port) {
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Port sudah digunakan di VPS.", "")
		return
	}

	file, header, err := r.FormFile("binary")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "File binary Go wajib diupload.", "")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 128<<20 {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Ukuran binary harus lebih dari 0 dan maksimal 128 MB.", "")
		return
	}

	if err := os.MkdirAll(a.uploadDir, 0750); err != nil {
		http.Error(w, "could not prepare upload directory", http.StatusInternalServerError)
		return
	}
	token, err := randomToken()
	if err != nil {
		http.Error(w, "could not create upload token", http.StatusInternalServerError)
		return
	}
	uploadPath := filepath.Join(a.uploadDir, "mini-panel-go-"+domain+"-"+token)
	out, err := os.OpenFile(uploadPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, "could not save upload", http.StatusInternalServerError)
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		_ = os.Remove(uploadPath)
		http.Error(w, "could not save upload", http.StatusInternalServerError)
		return
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(uploadPath)
		http.Error(w, "could not save upload", http.StatusInternalServerError)
		return
	}

	if err := a.domainCtl.InstallGo(r.Context(), domain, uploadPath, port); err != nil {
		_ = os.Remove(uploadPath)
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal install Go binary: "+err.Error(), "")
		return
	}
	upstream := "http://127.0.0.1:" + strconv.Itoa(port)
	if _, err := a.store.CreateDomainRecord(r.Context(), domain, upstream, "go_binary", port, filepath.Join(a.appRoot, domain, "app")); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) parseNodeZipDeploy(w http.ResponseWriter, r *http.Request) (string, int, string, string, string, string, bool) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upload tidak valid atau terlalu besar.", "")
		return "", 0, "", "", "", "", false
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return "", 0, "", "", "", "", false
	}
	port, err := NormalizeAppPort(r.FormValue("port"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Port harus di antara 1024 dan 65535.", "")
		return "", 0, "", "", "", "", false
	}
	if !a.portAvailable(port) {
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Port sudah digunakan di VPS.", "")
		return "", 0, "", "", "", "", false
	}
	installCommand, err := NormalizeDeployCommand(r.FormValue("install_command"), "npm install")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Install command tidak valid.", "")
		return "", 0, "", "", "", "", false
	}
	buildCommand, err := NormalizeDeployCommand(r.FormValue("build_command"), "npm run build")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Build command tidak valid.", "")
		return "", 0, "", "", "", "", false
	}
	startCommand, err := NormalizeDeployCommand(r.FormValue("start_command"), "npm start")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Start command tidak valid.", "")
		return "", 0, "", "", "", "", false
	}
	uploadPath, err := a.saveUploadedFile(r, "archive", "mini-panel-node-zip-"+domain+"-")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return "", 0, "", "", "", "", false
	}
	return domain, port, installCommand, buildCommand, startCommand, uploadPath, true
}

func (a *App) handleNodeGitPost(w http.ResponseWriter, r *http.Request) {
	domain, gitURL, branch, port, installCommand, buildCommand, startCommand, ok := a.parseNodeGitDeploy(w, r)
	if !ok {
		return
	}

	if err := a.domainCtl.InstallNode(r.Context(), domain, gitURL, branch, port, installCommand, buildCommand, startCommand); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal deploy Node/Next: "+err.Error(), "")
		return
	}
	upstream := "http://127.0.0.1:" + strconv.Itoa(port)
	if _, err := a.store.CreateDomainRecord(r.Context(), domain, upstream, "node_next", port, filepath.Join(a.appRoot, domain, "source")); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) parseNodeGitDeploy(w http.ResponseWriter, r *http.Request) (string, string, string, int, string, string, string, bool) {
	if err := r.ParseForm(); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return "", "", "", 0, "", "", "", false
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return "", "", "", 0, "", "", "", false
	}
	gitURL, err := NormalizeGitURL(r.FormValue("git_url"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Git URL harus HTTPS, contoh https://github.com/user/app.git.", "")
		return "", "", "", 0, "", "", "", false
	}
	branch, err := NormalizeBranch(r.FormValue("branch"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Branch tidak valid.", "")
		return "", "", "", 0, "", "", "", false
	}
	port, err := NormalizeAppPort(r.FormValue("port"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Port harus di antara 1024 dan 65535.", "")
		return "", "", "", 0, "", "", "", false
	}
	if !a.portAvailable(port) {
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Port sudah digunakan di VPS.", "")
		return "", "", "", 0, "", "", "", false
	}
	installCommand, err := NormalizeDeployCommand(r.FormValue("install_command"), "npm install")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Install command tidak valid.", "")
		return "", "", "", 0, "", "", "", false
	}
	buildCommand, err := NormalizeDeployCommand(r.FormValue("build_command"), "npm run build")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Build command tidak valid.", "")
		return "", "", "", 0, "", "", "", false
	}
	startCommand, err := NormalizeDeployCommand(r.FormValue("start_command"), "npm start")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Start command tidak valid.", "")
		return "", "", "", 0, "", "", "", false
	}
	return domain, gitURL, branch, port, installCommand, buildCommand, startCommand, true
}

func (a *App) handleZipPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Upload tidak valid atau terlalu besar.", "")
		return
	}
	siteType := strings.TrimSpace(r.FormValue("site_type"))
	if siteType != "static" && siteType != "php" {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Tipe site harus Static atau PHP.", "")
		return
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	uploadPath, err := a.saveUploadedFile(r, "archive", "mini-panel-zip-"+domain+"-")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}

	if err := a.domainCtl.InstallZip(r.Context(), siteType, domain, uploadPath); err != nil {
		_ = os.Remove(uploadPath)
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal deploy ZIP: "+err.Error(), "")
		return
	}
	appPath := filepath.Join(a.appRoot, domain, "public")
	if _, err := a.store.CreateDomainRecord(r.Context(), domain, appPath, siteType, 0, appPath); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) handlePHPGitPost(w http.ResponseWriter, r *http.Request) {
	domain, gitURL, branch, publicDir, installCommand, ok := a.parsePHPGitDeploy(w, r)
	if !ok {
		return
	}

	if err := a.domainCtl.InstallPHPGit(r.Context(), domain, gitURL, branch, publicDir, installCommand); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal deploy PHP/Laravel: "+err.Error(), "")
		return
	}
	appPath := filepath.Join(a.appRoot, domain, "source", publicDir)
	if _, err := a.store.CreateDomainRecord(r.Context(), domain, appPath, "php_git", 0, appPath); err != nil {
		_ = a.domainCtl.Delete(r.Context(), domain)
		a.renderDomainsWithMessage(w, r, http.StatusConflict, "Domain sudah ada atau gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) parsePHPGitDeploy(w http.ResponseWriter, r *http.Request) (string, string, string, string, string, bool) {
	if err := r.ParseForm(); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return "", "", "", "", "", false
	}
	domain, err := NormalizeDomain(r.FormValue("domain"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return "", "", "", "", "", false
	}
	gitURL, err := NormalizeGitURL(r.FormValue("git_url"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Git URL harus HTTPS, contoh https://github.com/user/app.git.", "")
		return "", "", "", "", "", false
	}
	branch, err := NormalizeBranch(r.FormValue("branch"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Branch tidak valid.", "")
		return "", "", "", "", "", false
	}
	publicDir, err := NormalizePublicDir(r.FormValue("public_dir"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Document root tidak valid.", "")
		return "", "", "", "", "", false
	}
	installCommand, err := NormalizeDeployCommand(r.FormValue("install_command"), "composer install --no-dev --optimize-autoloader")
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Install command tidak valid.", "")
		return "", "", "", "", "", false
	}
	return domain, gitURL, branch, publicDir, installCommand, true
}
