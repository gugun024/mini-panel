package panel

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func serveStatic(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := http.StripPrefix("/static/", staticHandler())
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestAllTemplatesRender executes every page template with representative
// data so a broken template action fails the build instead of 500-ing at
// runtime. It also asserts the modern layout contract: no inline
// <script>/<style>/event handlers (CSP has no 'unsafe-inline').
func TestAllTemplatesRender(t *testing.T) {
	tmpl, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}

	sess := &Session{Token: "t", CSRFToken: "csrf123", AdminID: 1, Username: "admin", ExpiresAt: time.Now().Add(time.Hour)}
	dom := Domain{ID: 1, Domain: "example.com", UpstreamURL: "http://127.0.0.1:3000", NginxPath: "/etc/nginx/sites-enabled/example.com", SiteType: "node", AppPath: "/srv/www/example.com", Status: "active", SSLEnabled: true}
	job := Job{ID: 7, Type: "deploy", Domain: "example.com", Status: "running", Log: "line1\nline2", CreatedAt: time.Now(), UpdatedAt: time.Now()}

	pages := map[string]pageData{
		"login":         {LoginCSRF: "lcsrf", Error: "bad creds"},
		"domains":       {Session: sess, Domains: []Domain{dom}, Notice: "ok"},
		"domain_detail": {Session: sess, Domain: &dom, Databases: []Database{{ID: 1, Name: "db", Username: "u"}}, EnvVars: []DomainEnvVar{{ID: 1, Name: "K", Value: "v"}}, DNSCheck: &DNSCheck{Matches: true, Records: []string{"1.2.3.4"}}, CanService: true, SSLStatus: "cert", NginxLogs: "n", AppLogs: "a", ServiceStatus: "active"},
		"jobs":          {Session: sess, Jobs: []Job{job}},
		"job_detail":    {Session: sess, Job: &job},
		"monitor":       {Session: sess, Metrics: SystemMetrics{CPUPercent: "3%", MemoryPercent: "40%", MemoryUsed: "1G", MemoryTotal: "4G", DiskPercent: "20%", DiskUsed: "10G", DiskTotal: "50G", LoadAverage: "0.1", Uptime: "2d"}, Services: []ServiceState{{Name: "nginx", Status: "active"}}, DomainServices: []ServiceState{{Name: "app", Status: "active"}}},
		"firewall":      {Session: sess, FirewallStatus: "Status: active"},
		"settings":      {Session: sess, PanelVersion: "v0.1.0", RepoURL: "https://github.com/x/y"},
		"workers":       {Session: sess, Workers: []Worker{{ID: 1, DomainName: "example.com", Name: "queue", Command: "php artisan queue:work", Workdir: "/srv", Status: "active"}}, Domains: []Domain{dom}},
		"cron":          {Session: sess, CronJobs: []CronJob{{ID: 1, DomainName: "example.com", Schedule: "* * * * *", Command: "true", Workdir: "/srv", Status: "active"}}, Domains: []Domain{dom}},
		"backups":       {Session: sess, Backups: []Backup{{ID: 1, TargetType: "domain", TargetName: "example.com", Path: "/b/x.tar.gz", SizeBytes: 42}}, Domains: []Domain{dom}, Databases: []Database{{ID: 1, Name: "db", Username: "u"}}},
		"databases":     {Session: sess, Databases: []Database{{ID: 1, Name: "db", Username: "u", Status: "active"}}, AdminerEnabled: true},
		"files":         {Session: sess, FileDomains: []Domain{dom}, SelectedDomain: &dom, FileView: FileView{RootLabel: "app", CurrentPath: "/", ParentPath: "/", Entries: []FileEntry{{Name: "index.html", Path: "/index.html", Size: 10, ModTime: "now"}}}},
	}

	for name, data := range pages {
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
			t.Errorf("template %q: %v", name, err)
			continue
		}
		out := buf.String()
		if !strings.Contains(out, "<!doctype html>") && !strings.Contains(out, "<!DOCTYPE html>") {
			t.Errorf("template %q: missing doctype", name)
		}
		if strings.Contains(out, "{{") {
			t.Errorf("template %q: unrendered action remains", name)
		}
	}

	// CSP contract: no inline scripts/styles/event handlers anywhere.
	var all bytes.Buffer
	for name, data := range pages {
		if err := tmpl.ExecuteTemplate(&all, name, data); err != nil {
			t.Fatal(err)
		}
	}
	rendered := all.String()
	for _, bad := range []string{"<script>", "onclick=", "onchange=", "onsubmit=", `style="`} {
		if strings.Contains(rendered, bad) {
			t.Errorf("rendered output contains %q (breaks strict CSP)", bad)
		}
	}
	if !strings.Contains(rendered, `/static/app.css`) || !strings.Contains(rendered, `/static/app.js`) {
		t.Errorf("rendered output missing static asset references")
	}
}

// TestStaticHandlerServesEmbeddedAssets ensures /static/* serves the
// embedded CSS/JS with the right content types.
func TestStaticHandlerServesEmbeddedAssets(t *testing.T) {
	for path, wantCT := range map[string]string{
		"/static/app.css": "text/css",
		"/static/app.js":  "text/javascript",
	} {
		rec := serveStatic(t, path)
		if rec.Code != 200 {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, wantCT) {
			t.Errorf("GET %s: Content-Type = %q, want %q", path, ct, wantCT)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s: empty body", path)
		}
	}
	// Unknown files must 404, not leak the FS.
	if rec := serveStatic(t, "/static/../../go.mod"); rec.Code != 404 && rec.Code != 400 {
		t.Errorf("GET traversal: status = %d, want 404/400", rec.Code)
	}
}
