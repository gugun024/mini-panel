package panel

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *App) handleCronJobs(w http.ResponseWriter, r *http.Request) {
	a.renderCronWithMessage(w, r, http.StatusOK, "", "")
}

func (a *App) handleCronJobPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	domainID, err := strconv.ParseInt(r.FormValue("domain_id"), 10, 64)
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	domain, err := a.store.GetDomain(r.Context(), domainID)
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusNotFound, "Domain tidak ditemukan.", "")
		return
	}
	schedule, err := NormalizeCronSchedule(r.FormValue("schedule"))
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadRequest, "Schedule cron tidak valid. Gunakan format 5 field, contoh: */5 * * * *", "")
		return
	}
	command, err := NormalizeCronCommand(r.FormValue("command"))
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadRequest, "Command cron tidak valid.", "")
		return
	}
	workdir, err := a.cronWorkdir(domain)
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	cronJob, err := a.store.CreateCronJob(r.Context(), domain.ID, domain.Domain, schedule, command, workdir)
	if err != nil {
		a.renderCronWithMessage(w, r, http.StatusInternalServerError, "Gagal menyimpan cron job.", "")
		return
	}
	if err := a.domainCtl.CreateCron(r.Context(), cronJob.ID, domain.Domain, schedule, workdir, command); err != nil {
		_, _ = a.store.DeleteCronJob(r.Context(), cronJob.ID)
		a.renderCronWithMessage(w, r, http.StatusBadGateway, "Gagal membuat cron job: "+err.Error(), "")
		return
	}
	http.Redirect(w, r, "/cron", http.StatusSeeOther)
}

func (a *App) handleCronJobDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cronJob, err := a.store.GetCronJob(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.domainCtl.DeleteCron(r.Context(), cronJob.ID, cronJob.DomainName); err != nil {
		a.renderCronWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus cron job: "+err.Error(), "")
		return
	}
	if _, err := a.store.DeleteCronJob(r.Context(), id); err != nil {
		a.renderCronWithMessage(w, r, http.StatusInternalServerError, "Cron file terhapus, tapi metadata panel gagal dihapus.", "")
		return
	}
	http.Redirect(w, r, "/cron", http.StatusSeeOther)
}

func (a *App) renderCronWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	cronDomains := make([]Domain, 0, len(domains))
	for _, domain := range domains {
		if domain.AppPath != "" {
			cronDomains = append(cronDomains, domain)
		}
	}
	cronJobs, err := a.store.ListCronJobs(r.Context())
	if err != nil {
		http.Error(w, "could not list cron jobs", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "cron", pageData{
		Session:  sessionFromContext(r.Context()),
		Domains:  cronDomains,
		CronJobs: cronJobs,
		Error:    errMsg,
		Notice:   okMsg,
	})
}

func (a *App) cronWorkdir(domain *Domain) (string, error) {
	if domain.AppPath == "" {
		return "", errText("Domain ini tidak punya folder aplikasi yang dikelola panel.")
	}
	workdir := filepath.Clean(domain.AppPath)
	if domain.SiteType == "go_binary" {
		workdir = filepath.Dir(workdir)
	}
	if !pathInside(filepath.Clean(a.appRoot), workdir) {
		return "", errText("Folder aplikasi tidak valid untuk cron.")
	}
	return workdir, nil
}

func pathInside(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

type errText string

func (e errText) Error() string {
	return string(e)
}
