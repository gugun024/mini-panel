package panel

import (
	"net/http"
	"strconv"
)

func (a *App) handleWorkers(w http.ResponseWriter, r *http.Request) {
	a.renderWorkersWithMessage(w, r, http.StatusOK, "", "")
}

func (a *App) handleWorkerPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	domainID, err := strconv.ParseInt(r.FormValue("domain_id"), 10, 64)
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, "Domain tidak valid.", "")
		return
	}
	domain, err := a.store.GetDomain(r.Context(), domainID)
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusNotFound, "Domain tidak ditemukan.", "")
		return
	}
	name, err := NormalizeWorkerName(r.FormValue("name"))
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, "Nama worker tidak valid. Gunakan huruf kecil, angka, dan hyphen.", "")
		return
	}
	command, err := NormalizeCronCommand(r.FormValue("command"))
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, "Command worker tidak valid.", "")
		return
	}
	workdir, err := a.cronWorkdir(domain)
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, err.Error(), "")
		return
	}
	worker, err := a.store.CreateWorker(r.Context(), domain.ID, domain.Domain, name, command, workdir)
	if err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusConflict, "Worker sudah ada atau gagal disimpan.", "")
		return
	}
	if err := a.domainCtl.CreateWorker(r.Context(), worker.ID, domain.Domain, name, workdir, command); err != nil {
		_, _ = a.store.DeleteWorker(r.Context(), worker.ID)
		a.renderWorkersWithMessage(w, r, http.StatusBadGateway, "Gagal membuat worker: "+err.Error(), "")
		return
	}
	http.Redirect(w, r, "/workers", http.StatusSeeOther)
}

func (a *App) handleWorkerService(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	worker, err := a.store.GetWorker(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	action := r.FormValue("action")
	switch action {
	case "start", "stop", "restart":
	default:
		a.renderWorkersWithMessage(w, r, http.StatusBadRequest, "Aksi worker tidak valid.", "")
		return
	}
	if err := a.domainCtl.WorkerService(r.Context(), action, worker.ID, worker.DomainName, worker.Name); err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadGateway, "Gagal menjalankan worker: "+err.Error(), "")
		return
	}
	status := "active"
	if action == "stop" {
		status = "stopped"
	}
	if err := a.store.UpdateWorkerStatus(r.Context(), worker.ID, status); err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusInternalServerError, "Worker berubah, tapi status panel gagal disimpan.", "")
		return
	}
	http.Redirect(w, r, "/workers", http.StatusSeeOther)
}

func (a *App) handleWorkerDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	worker, err := a.store.GetWorker(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.domainCtl.DeleteWorker(r.Context(), worker.ID, worker.DomainName, worker.Name); err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus worker: "+err.Error(), "")
		return
	}
	if _, err := a.store.DeleteWorker(r.Context(), id); err != nil {
		a.renderWorkersWithMessage(w, r, http.StatusInternalServerError, "Worker terhapus, tapi metadata panel gagal dihapus.", "")
		return
	}
	http.Redirect(w, r, "/workers", http.StatusSeeOther)
}

func (a *App) renderWorkersWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	appDomains := make([]Domain, 0, len(domains))
	for _, domain := range domains {
		if domain.AppPath != "" {
			appDomains = append(appDomains, domain)
		}
	}
	workers, err := a.store.ListWorkers(r.Context())
	if err != nil {
		http.Error(w, "could not list workers", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "workers", pageData{
		Session: sessionFromContext(r.Context()),
		Domains: appDomains,
		Workers: workers,
		Error:   errMsg,
		Notice:  okMsg,
	})
}
