package panel

import (
	"net/http"
	"net/http/httputil"
)

func (a *App) handleAdminerRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/adminer/", http.StatusSeeOther)
}

func (a *App) handleAdminerProxy(w http.ResponseWriter, r *http.Request) {
	if a.adminerURL == nil {
		http.Error(w, "Adminer is not configured", http.StatusBadGateway)
		return
	}
	target := *a.adminerURL
	proxy := httputil.NewSingleHostReverseProxy(&target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = "/"
		req.Host = target.Host
	}
	proxy.ServeHTTP(w, r)
}
