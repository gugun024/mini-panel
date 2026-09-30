package panel

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

func (a *App) handleDomainDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	domain, err := a.store.GetDomain(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.domainCtl.Delete(r.Context(), domain.Domain); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus config Nginx: "+err.Error(), "")
		return
	}
	if _, err := a.store.DeleteDomain(r.Context(), id); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusInternalServerError, "Config terhapus, tapi data domain gagal dihapus.", "")
		return
	}
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (a *App) handleDomainDetail(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	a.renderDomainDetail(w, r, http.StatusOK, domain, "", "")
}

func (a *App) handleDomainService(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	if !domainHasService(domain) {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Domain ini tidak punya service aplikasi.", "")
		return
	}
	action := strings.TrimSpace(r.FormValue("action"))
	if action != "start" && action != "stop" && action != "restart" {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Aksi service tidak valid.", "")
		return
	}
	if err := a.domainCtl.Service(r.Context(), action, domain.Domain); err != nil {
		a.renderDomainDetail(w, r, http.StatusBadGateway, domain, "Gagal menjalankan service: "+err.Error(), "")
		return
	}
	a.renderDomainDetail(w, r, http.StatusOK, domain, "", "Service berhasil "+action+".")
}

func (a *App) handleDomainCommand(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	if domain.AppPath == "" {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Domain ini tidak punya folder aplikasi yang dikelola panel.", "")
		return
	}
	command, err := NormalizeCronCommand(r.FormValue("command"))
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, "Command tidak valid.", "")
		return
	}
	workdir, err := a.cronWorkdir(domain)
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusBadRequest, domain, err.Error(), "")
		return
	}
	job, err := a.store.CreateJob(r.Context(), "command", domain.Domain)
	if err != nil {
		a.renderDomainDetail(w, r, http.StatusInternalServerError, domain, "Gagal membuat job command.", "")
		return
	}
	go a.runJob(job.ID, func(ctx context.Context, logf jobLogger) error {
		logf("domain: %s", domain.Domain)
		logf("workdir: %s", workdir)
		logf("command: %s", command)
		out, err := a.domainCtl.RunCommand(ctx, domain.Domain, workdir, command)
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.TrimSpace(line) != "" {
				logf("%s", line)
			}
		}
		return err
	})
	http.Redirect(w, r, "/jobs/"+strconv.FormatInt(job.ID, 10), http.StatusSeeOther)
}

func (a *App) handleDomainSSL(w http.ResponseWriter, r *http.Request) {
	domain, ok := a.domainByPathID(w, r)
	if !ok {
		return
	}
	email, err := NormalizeEmail(r.FormValue("email"))
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadRequest, "Email SSL tidak valid.", "")
		return
	}
	if err := a.domainCtl.IssueSSL(r.Context(), domain.Domain, email); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusBadGateway, "Gagal issue SSL: "+err.Error(), "")
		return
	}
	if err := a.store.MarkDomainSSL(r.Context(), domain.ID, true); err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusInternalServerError, "SSL terpasang, tapi status panel gagal disimpan.", "")
		return
	}
	a.renderDomainsWithMessage(w, r, http.StatusOK, "", "SSL berhasil dipasang untuk "+domain.Domain+".")
}

func (a *App) domainByPathID(w http.ResponseWriter, r *http.Request) (*Domain, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	domain, err := a.store.GetDomain(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return nil, false
	}
	return domain, true
}

func (a *App) renderDomainDetail(w http.ResponseWriter, r *http.Request, status int, domain *Domain, errMsg, okMsg string) {
	envVars, err := a.store.ListDomainEnvVars(r.Context(), domain.ID)
	if err != nil {
		http.Error(w, "could not list env vars", http.StatusInternalServerError)
		return
	}
	databases, err := a.store.ListDatabases(r.Context())
	if err != nil {
		http.Error(w, "could not list databases", http.StatusInternalServerError)
		return
	}
	serviceStatus := "n/a"
	appLogs := ""
	canService := domainHasService(domain)
	if canService {
		if out, err := a.domainCtl.Output(r.Context(), "service", "status", domain.Domain); err == nil && out != "" {
			serviceStatus = out
		} else if err != nil {
			serviceStatus = "unknown"
		}
		if out, err := a.domainCtl.Output(r.Context(), "logs", "app", domain.Domain); err == nil {
			appLogs = out
		}
	}
	nginxLogs := ""
	if out, err := a.domainCtl.Output(r.Context(), "logs", "nginx", domain.Domain); err == nil {
		nginxLogs = out
	}
	sslStatus := ""
	if out, err := a.domainCtl.SSLStatus(r.Context(), domain.Domain); err == nil {
		sslStatus = out
	} else if domain.SSLEnabled {
		sslStatus = err.Error()
	}
	dnsCheck := checkDomainDNS(r.Context(), domain.Domain)
	a.render(w, status, "domain_detail", pageData{
		Session:       sessionFromContext(r.Context()),
		Domain:        domain,
		Databases:     databases,
		EnvVars:       envVars,
		DNSCheck:      &dnsCheck,
		SSLStatus:     sslStatus,
		ServiceStatus: serviceStatus,
		NginxLogs:     nginxLogs,
		AppLogs:       appLogs,
		CanService:    canService,
		Error:         errMsg,
		Notice:        okMsg,
	})
}

func domainHasService(domain *Domain) bool {
	return domain.SiteType == "go_binary" || domain.SiteType == "node_next"
}
