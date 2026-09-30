package panel

import (
	"net/http"
)

func (a *App) handleDomains(w http.ResponseWriter, r *http.Request) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusOK, "domains", pageData{
		Session: sessionFromContext(r.Context()),
		Domains: domains,
	})
}

func (a *App) renderDomainsWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	domains, err := a.store.ListDomains(r.Context())
	if err != nil {
		http.Error(w, "could not list domains", http.StatusInternalServerError)
		return
	}
	a.render(w, status, "domains", pageData{
		Session: sessionFromContext(r.Context()),
		Domains: domains,
		Error:   errMsg,
		Notice:  okMsg,
	})
}
