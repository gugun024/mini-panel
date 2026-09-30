package panel

import "net/http"

func (a *App) handleFirewall(w http.ResponseWriter, r *http.Request) {
	a.renderFirewallWithMessage(w, r, http.StatusOK, "", "")
}

func (a *App) handleFirewallEnable(w http.ResponseWriter, r *http.Request) {
	if err := a.domainCtl.FirewallEnable(r.Context()); err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadGateway, "Gagal mengaktifkan firewall: "+err.Error(), "")
		return
	}
	a.renderFirewallWithMessage(w, r, http.StatusOK, "", "Firewall aktif. Rule dasar SSH, HTTP, dan HTTPS sudah dipastikan terbuka.")
}

func (a *App) handleFirewallAllow(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	port, protocol, ok := a.firewallPortProtocol(w, r)
	if !ok {
		return
	}
	if err := a.domainCtl.FirewallAllow(r.Context(), port, protocol); err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadGateway, "Gagal menambah firewall rule: "+err.Error(), "")
		return
	}
	a.renderFirewallWithMessage(w, r, http.StatusOK, "", "Firewall rule ditambahkan.")
}

func (a *App) handleFirewallDelete(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadRequest, "Invalid form.", "")
		return
	}
	port, protocol, ok := a.firewallPortProtocol(w, r)
	if !ok {
		return
	}
	if err := a.domainCtl.FirewallDelete(r.Context(), port, protocol); err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadGateway, "Gagal menghapus firewall rule: "+err.Error(), "")
		return
	}
	a.renderFirewallWithMessage(w, r, http.StatusOK, "", "Firewall rule dihapus.")
}

func (a *App) firewallPortProtocol(w http.ResponseWriter, r *http.Request) (int, string, bool) {
	port, err := NormalizeFirewallPort(r.FormValue("port"))
	if err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadRequest, "Port firewall tidak valid.", "")
		return 0, "", false
	}
	protocol, err := NormalizeFirewallProtocol(r.FormValue("protocol"))
	if err != nil {
		a.renderFirewallWithMessage(w, r, http.StatusBadRequest, "Protocol firewall tidak valid.", "")
		return 0, "", false
	}
	return port, protocol, true
}

func (a *App) renderFirewallWithMessage(w http.ResponseWriter, r *http.Request, status int, errMsg, okMsg string) {
	firewallStatus, err := a.domainCtl.FirewallStatus(r.Context())
	if err != nil {
		firewallStatus = err.Error()
	}
	a.render(w, status, "firewall", pageData{
		Session:        sessionFromContext(r.Context()),
		Error:          errMsg,
		Notice:         okMsg,
		FirewallStatus: firewallStatus,
	})
}
