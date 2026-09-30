package panel

import "net/http"

const defaultPanelRepoURL = "https://github.com/Gugun09/mini-panel"

func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	a.renderSettingsWithMessage(w, r, http.StatusOK, "", "")
}

func (a *App) handlePanelUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderSettingsWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	version, err := NormalizeReleaseVersion(r.FormValue("version"))
	if err != nil {
		a.renderSettingsWithMessage(w, r, http.StatusBadRequest, "Versi release tidak valid. Gunakan latest atau tag seperti v0.1.6.", "")
		return
	}
	repoURL, err := NormalizeRepoURL(r.FormValue("repo_url"))
	if err != nil {
		a.renderSettingsWithMessage(w, r, http.StatusBadRequest, "Repo URL tidak valid. Gunakan URL GitHub HTTPS, contoh https://github.com/Gugun09/mini-panel.", "")
		return
	}
	if err := a.domainCtl.PanelUpdate(r.Context(), version, repoURL); err != nil {
		a.renderSettingsWithMessage(w, r, http.StatusBadGateway, "Gagal memulai update panel: "+err.Error(), "")
		return
	}
	a.renderSettingsWithMessage(w, r, http.StatusOK, "", "Update panel dimulai. Service mini-panel akan restart otomatis jika installer selesai.")
}

func (a *App) renderSettingsWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	a.render(w, status, "settings", pageData{
		Session:      sessionFromContext(r.Context()),
		Error:        errMsg,
		Notice:       okMsg,
		PanelVersion: a.version,
		RepoURL:      defaultPanelRepoURL,
	})
}
