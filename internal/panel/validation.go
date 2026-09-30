package panel

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidDomain   = errors.New("invalid domain")
	ErrInvalidUpstream = errors.New("invalid upstream")
	ErrInvalidPort     = errors.New("invalid port")
	ErrInvalidGitURL   = errors.New("invalid git URL")
	ErrInvalidCommand  = errors.New("invalid command")
	ErrInvalidBranch   = errors.New("invalid branch")
	ErrInvalidPath     = errors.New("invalid path")
	ErrInvalidEmail    = errors.New("invalid email")
	ErrInvalidDBName   = errors.New("invalid database name")
	ErrInvalidEnvName  = errors.New("invalid environment variable name")
	ErrInvalidEnvValue = errors.New("invalid environment variable value")
	ErrInvalidCron     = errors.New("invalid cron")
	ErrInvalidName     = errors.New("invalid name")
)

var safeBranchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
var safeRelativePathPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
var safeEnvNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,63}$`)
var safeCronFieldPattern = regexp.MustCompile(`^[0-9*/,\-]+$`)
var safeWorkerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
var safeReleaseVersionPattern = regexp.MustCompile(`^v[0-9]+(\.[0-9]+){1,3}(-[A-Za-z0-9._-]+)?$`)

func NormalizeDomain(input string) (string, error) {
	domain := strings.TrimSpace(strings.ToLower(input))
	domain = strings.TrimSuffix(domain, ".")
	if len(domain) == 0 || len(domain) > 253 {
		return "", ErrInvalidDomain
	}
	if !strings.Contains(domain, ".") {
		return "", fmt.Errorf("%w: must contain at least one dot", ErrInvalidDomain)
	}
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return "", ErrInvalidDomain
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidDomain
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", ErrInvalidDomain
		}
	}
	return domain, nil
}

func NormalizeUpstream(input string) (string, error) {
	raw := strings.TrimSpace(input)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidUpstream
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%w: path is not supported", ErrInvalidUpstream)
	}
	host := strings.ToLower(u.Hostname())
	if host != "localhost" && host != "127.0.0.1" {
		return "", fmt.Errorf("%w: host must be localhost or 127.0.0.1", ErrInvalidUpstream)
	}
	portText := u.Port()
	if portText == "" {
		return "", fmt.Errorf("%w: port is required", ErrInvalidUpstream)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("%w: invalid port", ErrInvalidUpstream)
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}).String(), nil
}

func NormalizeAppPort(input string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || port < 1024 || port > 65535 {
		return 0, ErrInvalidPort
	}
	return port, nil
}

func NormalizeFirewallPort(input string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || port < 1 || port > 65535 {
		return 0, ErrInvalidPort
	}
	return port, nil
}

func NormalizeGitURL(input string) (string, error) {
	raw := strings.TrimSpace(input)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidGitURL
	}
	if !strings.Contains(u.Host, ".") || u.Path == "" || u.Path == "/" {
		return "", ErrInvalidGitURL
	}
	for _, r := range raw {
		if r <= 32 || strings.ContainsRune(";&|`$<>\\", r) {
			return "", ErrInvalidGitURL
		}
	}
	return raw, nil
}

func NormalizeBranch(input string) (string, error) {
	branch := strings.TrimSpace(input)
	if branch == "" {
		return "main", nil
	}
	if !safeBranchPattern.MatchString(branch) || strings.Contains(branch, "..") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") {
		return "", ErrInvalidBranch
	}
	return branch, nil
}

func NormalizeDeployCommand(input, fallback string) (string, error) {
	command := strings.TrimSpace(input)
	if command == "" {
		command = fallback
	}
	if len(command) > 300 {
		return "", ErrInvalidCommand
	}
	for _, r := range command {
		if r == 0 || r == '\n' || r == '\r' {
			return "", ErrInvalidCommand
		}
	}
	return command, nil
}

func NormalizePublicDir(input string) (string, error) {
	path := strings.TrimSpace(input)
	if path == "" {
		return "public", nil
	}
	if !safeRelativePathPattern.MatchString(path) || strings.Contains(path, "..") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", ErrInvalidPath
	}
	return path, nil
}

func NormalizeEmail(input string) (string, error) {
	email := strings.TrimSpace(strings.ToLower(input))
	if len(email) < 6 || len(email) > 254 || strings.ContainsAny(email, " \r\n\t;&|`$<>\\") {
		return "", ErrInvalidEmail
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", ErrInvalidEmail
	}
	if _, err := NormalizeDomain(email[at+1:]); err != nil {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func NormalizeDBIdentifier(input string) (string, error) {
	value := strings.TrimSpace(input)
	if len(value) < 1 || len(value) > 48 {
		return "", ErrInvalidDBName
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return "", ErrInvalidDBName
	}
	return value, nil
}

func NormalizeEnvName(input string) (string, error) {
	name := strings.TrimSpace(strings.ToUpper(input))
	if !safeEnvNamePattern.MatchString(name) {
		return "", ErrInvalidEnvName
	}
	return name, nil
}

func NormalizeEnvValue(input string) (string, error) {
	value := strings.ReplaceAll(input, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	if len(value) > 65536 || strings.Contains(value, "\x00") {
		return "", ErrInvalidEnvValue
	}
	return value, nil
}

func NormalizeCronSchedule(input string) (string, error) {
	schedule := strings.Join(strings.Fields(input), " ")
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return "", ErrInvalidCron
	}
	for _, field := range fields {
		if field == "" || len(field) > 32 || !safeCronFieldPattern.MatchString(field) {
			return "", ErrInvalidCron
		}
	}
	return schedule, nil
}

func NormalizeCronCommand(input string) (string, error) {
	command := strings.TrimSpace(input)
	if len(command) == 0 || len(command) > 500 {
		return "", ErrInvalidCommand
	}
	for _, r := range command {
		if r == 0 || r == '\n' || r == '\r' {
			return "", ErrInvalidCommand
		}
	}
	return command, nil
}

func NormalizeWorkerName(input string) (string, error) {
	name := strings.TrimSpace(strings.ToLower(input))
	name = strings.ReplaceAll(name, "_", "-")
	if !safeWorkerNamePattern.MatchString(name) || strings.Contains(name, "--") {
		return "", ErrInvalidName
	}
	return name, nil
}

func NormalizeFirewallProtocol(input string) (string, error) {
	protocol := strings.TrimSpace(strings.ToLower(input))
	if protocol != "tcp" && protocol != "udp" {
		return "", ErrInvalidName
	}
	return protocol, nil
}

func NormalizeReleaseVersion(input string) (string, error) {
	version := strings.TrimSpace(input)
	if version == "" {
		return "latest", nil
	}
	if version == "latest" || safeReleaseVersionPattern.MatchString(version) {
		return version, nil
	}
	return "", ErrInvalidName
}

func NormalizeRepoURL(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		raw = "https://github.com/Gugun09/mini-panel"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", ErrInvalidGitURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ErrInvalidGitURL
	}
	for _, part := range parts {
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
				continue
			}
			return "", ErrInvalidGitURL
		}
	}
	return strings.TrimSuffix(raw, ".git"), nil
}

func nginxConfigPath(domain string) string {
	return "/etc/nginx/sites-available/mini-panel-" + domain + ".conf"
}
