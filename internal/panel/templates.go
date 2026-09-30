package panel

import (
	"embed"
	"html/template"
)

type pageData struct {
	Session        *Session
	Domains        []Domain
	Domain         *Domain
	Databases      []Database
	Backups        []Backup
	CronJobs       []CronJob
	Workers        []Worker
	Metrics        SystemMetrics
	Services       []ServiceState
	DomainServices []ServiceState
	DNSCheck       *DNSCheck
	SSLStatus      string
	FirewallStatus string
	FirewallRules  string
	PanelVersion   string
	RepoURL        string
	EnvVars        []DomainEnvVar
	Jobs           []Job
	Job            *Job
	FileDomains    []Domain
	SelectedDomain *Domain
	FileView       FileView
	ServiceStatus  string
	NginxLogs      string
	AppLogs        string
	CanService     bool
	AdminerEnabled bool
	Error          string
	Notice         string
	LoginCSRF      string
}

//go:embed templates/*.html
var templateFS embed.FS

func parseTemplates() (*template.Template, error) {
	return template.New("mini-panel").ParseFS(templateFS, "templates/*.html")
}
