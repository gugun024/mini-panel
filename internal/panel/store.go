package panel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Admin struct {
	ID       int64
	Username string
}

type Domain struct {
	ID           int64
	Domain       string
	UpstreamURL  string
	NginxPath    string
	SiteType     string
	AppPort      int
	AppPath      string
	Status       string
	SSLEnabled   bool
	SSLUpdatedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Database struct {
	ID        int64
	Name      string
	Username  string
	Password  string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Backup struct {
	ID         int64
	TargetType string
	TargetID   int64
	TargetName string
	Path       string
	Metadata   string
	SizeBytes  int64
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type CronJob struct {
	ID         int64
	DomainID   int64
	DomainName string
	Schedule   string
	Command    string
	Workdir    string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Worker struct {
	ID         int64
	DomainID   int64
	DomainName string
	Name       string
	Command    string
	Workdir    string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type DomainEnvVar struct {
	ID        int64
	DomainID  int64
	Name      string
	Value     string
	IsSecret  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Job struct {
	ID         int64
	Type       string
	Domain     string
	Status     string
	Log        string
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	UpdatedAt  time.Time
}

type Session struct {
	Token     string
	CSRFToken string
	AdminID   int64
	Username  string
	ExpiresAt time.Time
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS admins (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	username TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
	csrf_token TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS domains (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	domain TEXT NOT NULL UNIQUE,
	upstream_url TEXT NOT NULL,
	nginx_path TEXT NOT NULL,
	site_type TEXT NOT NULL DEFAULT 'reverse_proxy',
	app_port INTEGER NOT NULL DEFAULT 0,
	app_path TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	ssl_enabled INTEGER NOT NULL DEFAULT 0,
	ssl_updated_at TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS databases (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	username TEXT NOT NULL UNIQUE,
	password TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS domain_env_vars (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	value TEXT NOT NULL,
	is_secret INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(domain_id, name)
);

CREATE TABLE IF NOT EXISTS backups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	target_type TEXT NOT NULL,
	target_id INTEGER NOT NULL DEFAULT 0,
	target_name TEXT NOT NULL,
	path TEXT NOT NULL UNIQUE,
	metadata TEXT NOT NULL DEFAULT '',
	size_bytes INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS cron_jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	domain_name TEXT NOT NULL,
	schedule TEXT NOT NULL,
	command TEXT NOT NULL,
	workdir TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
	domain_name TEXT NOT NULL,
	name TEXT NOT NULL,
	command TEXT NOT NULL,
	workdir TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(domain_id, name)
);

CREATE TABLE IF NOT EXISTS jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	domain TEXT NOT NULL,
	status TEXT NOT NULL,
	log TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	started_at TEXT NOT NULL DEFAULT '',
	finished_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE domains ADD COLUMN site_type TEXT NOT NULL DEFAULT 'reverse_proxy'`,
		`ALTER TABLE domains ADD COLUMN app_port INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE domains ADD COLUMN app_path TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE domains ADD COLUMN ssl_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE domains ADD COLUMN ssl_updated_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE databases ADD COLUMN password TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE backups ADD COLUMN metadata TEXT NOT NULL DEFAULT ''`,
	} {
		if _, alterErr := s.db.ExecContext(ctx, stmt); alterErr != nil && !strings.Contains(alterErr.Error(), "duplicate column") {
			return alterErr
		}
	}
	return err
}

func (s *Store) CreateWorker(ctx context.Context, domainID int64, domainName, name, command, workdir string) (*Worker, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO workers (domain_id, domain_name, name, command, workdir, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, domainID, domainName, name, command, workdir, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetWorker(ctx, id)
}

func (s *Store) ListWorkers(ctx context.Context) ([]Worker, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain_id, domain_name, name, command, workdir, status, created_at, updated_at
FROM workers
ORDER BY id DESC
LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workers []Worker
	for rows.Next() {
		worker, err := scanWorker(rows)
		if err != nil {
			return nil, err
		}
		workers = append(workers, *worker)
	}
	return workers, rows.Err()
}

func (s *Store) GetWorker(ctx context.Context, id int64) (*Worker, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain_id, domain_name, name, command, workdir, status, created_at, updated_at
FROM workers
WHERE id = ?`, id)
	return scanWorker(row)
}

func (s *Store) UpdateWorkerStatus(ctx context.Context, id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE workers SET status = ?, updated_at = ? WHERE id = ?`, status, now, id)
	return err
}

func (s *Store) DeleteWorker(ctx context.Context, id int64) (*Worker, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, domain_id, domain_name, name, command, workdir, status, created_at, updated_at
FROM workers
WHERE id = ?`, id)
	worker, err := scanWorker(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workers WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return worker, nil
}

func (s *Store) CreateCronJob(ctx context.Context, domainID int64, domainName, schedule, command, workdir string) (*CronJob, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO cron_jobs (domain_id, domain_name, schedule, command, workdir, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`, domainID, domainName, schedule, command, workdir, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetCronJob(ctx, id)
}

func (s *Store) ListCronJobs(ctx context.Context) ([]CronJob, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain_id, domain_name, schedule, command, workdir, status, created_at, updated_at
FROM cron_jobs
ORDER BY id DESC
LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cronJobs []CronJob
	for rows.Next() {
		cronJob, err := scanCronJob(rows)
		if err != nil {
			return nil, err
		}
		cronJobs = append(cronJobs, *cronJob)
	}
	return cronJobs, rows.Err()
}

func (s *Store) GetCronJob(ctx context.Context, id int64) (*CronJob, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain_id, domain_name, schedule, command, workdir, status, created_at, updated_at
FROM cron_jobs
WHERE id = ?`, id)
	return scanCronJob(row)
}

func (s *Store) DeleteCronJob(ctx context.Context, id int64) (*CronJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, domain_id, domain_name, schedule, command, workdir, status, created_at, updated_at
FROM cron_jobs
WHERE id = ?`, id)
	cronJob, err := scanCronJob(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cronJob, nil
}

func (s *Store) CreateBackup(ctx context.Context, targetType string, targetID int64, targetName, path, metadata string, sizeBytes int64) (*Backup, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO backups (target_type, target_id, target_name, path, metadata, size_bytes, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'ready', ?, ?)`, targetType, targetID, targetName, path, metadata, sizeBytes, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetBackup(ctx, id)
}

func (s *Store) ListBackups(ctx context.Context) ([]Backup, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, target_type, target_id, target_name, path, metadata, size_bytes, status, created_at, updated_at
FROM backups
ORDER BY id DESC
LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var backups []Backup
	for rows.Next() {
		backup, err := scanBackup(rows)
		if err != nil {
			return nil, err
		}
		backups = append(backups, *backup)
	}
	return backups, rows.Err()
}

func (s *Store) GetBackup(ctx context.Context, id int64) (*Backup, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, target_type, target_id, target_name, path, metadata, size_bytes, status, created_at, updated_at
FROM backups
WHERE id = ?`, id)
	return scanBackup(row)
}

func (s *Store) DeleteBackup(ctx context.Context, id int64) (*Backup, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, target_type, target_id, target_name, path, metadata, size_bytes, status, created_at, updated_at
FROM backups
WHERE id = ?`, id)
	backup, err := scanBackup(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM backups WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return backup, nil
}

func (s *Store) CreateJob(ctx context.Context, jobType, domain string) (*Job, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (type, domain, status, log, error, created_at, started_at, finished_at, updated_at)
VALUES (?, ?, 'queued', '', '', ?, '', '', ?)`, jobType, domain, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetJob(ctx, id)
}

func (s *Store) StartJob(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'running', started_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) AppendJobLog(ctx context.Context, id int64, text string) error {
	if text == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET log = log || ?, updated_at = ? WHERE id = ?`, text, now, id)
	return err
}

func (s *Store) FinishJob(ctx context.Context, id int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'success', finished_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) FailJob(ctx context.Context, id int64, message string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status = 'failed', error = ?, finished_at = ?, updated_at = ? WHERE id = ?`, message, now, now, id)
	return err
}

func (s *Store) ListJobs(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, type, domain, status, log, error, created_at, started_at, finished_at, updated_at
FROM jobs
ORDER BY id DESC
LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func (s *Store) GetJob(ctx context.Context, id int64) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, type, domain, status, log, error, created_at, started_at, finished_at, updated_at
FROM jobs
WHERE id = ?`, id)
	return scanJob(row)
}

func (s *Store) CreateDatabase(ctx context.Context, name, username, password string) (*Database, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO databases (name, username, password, status, created_at, updated_at)
VALUES (?, ?, ?, 'active', ?, ?)`, name, username, password, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetDatabase(ctx, id)
}

func (s *Store) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, username, password, status, created_at, updated_at
FROM databases
ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var databases []Database
	for rows.Next() {
		db, err := scanDatabase(rows)
		if err != nil {
			return nil, err
		}
		databases = append(databases, *db)
	}
	return databases, rows.Err()
}

func (s *Store) GetDatabase(ctx context.Context, id int64) (*Database, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, username, password, status, created_at, updated_at
FROM databases
WHERE id = ?`, id)
	return scanDatabase(row)
}

func (s *Store) DeleteDatabase(ctx context.Context, id int64) (*Database, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, name, username, password, status, created_at, updated_at
FROM databases
WHERE id = ?`, id)
	db, err := scanDatabase(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM databases WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return db, nil
}

func (s *Store) ListDomainEnvVars(ctx context.Context, domainID int64) ([]DomainEnvVar, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain_id, name, value, is_secret, created_at, updated_at
FROM domain_env_vars
WHERE domain_id = ?
ORDER BY name`, domainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vars []DomainEnvVar
	for rows.Next() {
		envVar, err := scanDomainEnvVar(rows)
		if err != nil {
			return nil, err
		}
		vars = append(vars, *envVar)
	}
	return vars, rows.Err()
}

func (s *Store) UpsertDomainEnvVar(ctx context.Context, domainID int64, name, value string, isSecret bool) (*DomainEnvVar, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	secretValue := 0
	if isSecret {
		secretValue = 1
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO domain_env_vars (domain_id, name, value, is_secret, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(domain_id, name) DO UPDATE SET
	value = excluded.value,
	is_secret = excluded.is_secret,
	updated_at = excluded.updated_at`, domainID, name, value, secretValue, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if id == 0 {
		row := s.db.QueryRowContext(ctx, `SELECT id FROM domain_env_vars WHERE domain_id = ? AND name = ?`, domainID, name)
		if err := row.Scan(&id); err != nil {
			return nil, err
		}
	}
	return s.GetDomainEnvVar(ctx, domainID, id)
}

func (s *Store) GetDomainEnvVar(ctx context.Context, domainID, id int64) (*DomainEnvVar, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain_id, name, value, is_secret, created_at, updated_at
FROM domain_env_vars
WHERE domain_id = ? AND id = ?`, domainID, id)
	return scanDomainEnvVar(row)
}

func (s *Store) DeleteDomainEnvVar(ctx context.Context, domainID, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM domain_env_vars WHERE domain_id = ? AND id = ?`, domainID, id)
	return err
}

func (s *Store) CreateAdmin(ctx context.Context, username, password string) error {
	if username == "" {
		return errors.New("username is required")
	}
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO admins (username, password_hash, created_at, updated_at)
VALUES (?, ?, ?, ?)`, username, string(hash), now, now)
	return err
}

func (s *Store) AuthenticateAdmin(ctx context.Context, username, password string) (*Admin, error) {
	var admin Admin
	var hash string
	err := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash FROM admins WHERE username = ?`, username).Scan(&admin.ID, &admin.Username, &hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &admin, nil
}

func (s *Store) CreateSession(ctx context.Context, adminID int64, token, csrf string, expiresAt time.Time) error {
	hash := hashToken(token)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions (token_hash, admin_id, csrf_token, expires_at, created_at)
VALUES (?, ?, ?, ?, ?)`, hash, adminID, csrf, expiresAt.UTC().Format(time.RFC3339Nano), now)
	return err
}

func (s *Store) GetSession(ctx context.Context, token string, now time.Time) (*Session, error) {
	hash := hashToken(token)
	var session Session
	var expiresText string
	err := s.db.QueryRowContext(ctx, `
SELECT s.admin_id, a.username, s.csrf_token, s.expires_at
FROM sessions s
JOIN admins a ON a.id = s.admin_id
WHERE s.token_hash = ?`, hash).Scan(&session.AdminID, &session.Username, &session.CSRFToken, &expiresText)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidSession
		}
		return nil, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil {
		return nil, err
	}
	if !now.Before(expiresAt) {
		_ = s.DeleteSession(ctx, token)
		return nil, ErrInvalidSession
	}
	session.Token = token
	session.ExpiresAt = expiresAt
	return &session, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(token))
	return err
}

func (s *Store) PruneExpiredSessions(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, now.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) CreateDomain(ctx context.Context, domain, upstream string) (*Domain, error) {
	return s.CreateDomainRecord(ctx, domain, upstream, "reverse_proxy", 0, "")
}

func (s *Store) CreateDomainRecord(ctx context.Context, domain, upstream, siteType string, appPort int, appPath string) (*Domain, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	nginxPath := nginxConfigPath(domain)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO domains (domain, upstream_url, nginx_path, site_type, app_port, app_path, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`, domain, upstream, nginxPath, siteType, appPort, appPath, now, now)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetDomain(ctx, id)
}

func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, domain, upstream_url, nginx_path, site_type, app_port, app_path, status, ssl_enabled, ssl_updated_at, created_at, updated_at
FROM domains
ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		domain, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		domains = append(domains, *domain)
	}
	return domains, rows.Err()
}

func (s *Store) GetDomain(ctx context.Context, id int64) (*Domain, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain, upstream_url, nginx_path, site_type, app_port, app_path, status, ssl_enabled, ssl_updated_at, created_at, updated_at
FROM domains
WHERE id = ?`, id)
	return scanDomain(row)
}

func (s *Store) GetDomainByName(ctx context.Context, name string) (*Domain, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, domain, upstream_url, nginx_path, site_type, app_port, app_path, status, ssl_enabled, ssl_updated_at, created_at, updated_at
FROM domains
WHERE domain = ?`, name)
	return scanDomain(row)
}

func (s *Store) MarkDomainSSL(ctx context.Context, id int64, enabled bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sslEnabled := 0
	sslUpdatedAt := ""
	if enabled {
		sslEnabled = 1
		sslUpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE domains
SET ssl_enabled = ?, ssl_updated_at = ?, updated_at = ?
WHERE id = ?`, sslEnabled, sslUpdatedAt, now, id)
	return err
}

func (s *Store) DeleteDomain(ctx context.Context, id int64) (*Domain, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
SELECT id, domain, upstream_url, nginx_path, site_type, app_port, app_path, status, ssl_enabled, ssl_updated_at, created_at, updated_at
FROM domains
WHERE id = ?`, id)
	domain, err := scanDomain(row)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM domains WHERE id = ?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return domain, nil
}

type domainScanner interface {
	Scan(dest ...any) error
}

func scanDomain(scanner domainScanner) (*Domain, error) {
	var domain Domain
	var createdText, updatedText, sslUpdatedText string
	var sslEnabled int
	if err := scanner.Scan(&domain.ID, &domain.Domain, &domain.UpstreamURL, &domain.NginxPath, &domain.SiteType, &domain.AppPort, &domain.AppPath, &domain.Status, &sslEnabled, &sslUpdatedText, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	domain.CreatedAt = createdAt
	domain.UpdatedAt = updatedAt
	domain.SSLEnabled = sslEnabled != 0
	if sslUpdatedText != "" {
		sslUpdatedAt, err := time.Parse(time.RFC3339Nano, sslUpdatedText)
		if err != nil {
			return nil, err
		}
		domain.SSLUpdatedAt = &sslUpdatedAt
	}
	return &domain, nil
}

func scanDatabase(scanner domainScanner) (*Database, error) {
	var db Database
	var createdText, updatedText string
	if err := scanner.Scan(&db.ID, &db.Name, &db.Username, &db.Password, &db.Status, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	db.CreatedAt = createdAt
	db.UpdatedAt = updatedAt
	return &db, nil
}

func scanBackup(scanner domainScanner) (*Backup, error) {
	var backup Backup
	var createdText, updatedText string
	if err := scanner.Scan(&backup.ID, &backup.TargetType, &backup.TargetID, &backup.TargetName, &backup.Path, &backup.Metadata, &backup.SizeBytes, &backup.Status, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	backup.CreatedAt = createdAt
	backup.UpdatedAt = updatedAt
	return &backup, nil
}

func scanCronJob(scanner domainScanner) (*CronJob, error) {
	var cronJob CronJob
	var createdText, updatedText string
	if err := scanner.Scan(&cronJob.ID, &cronJob.DomainID, &cronJob.DomainName, &cronJob.Schedule, &cronJob.Command, &cronJob.Workdir, &cronJob.Status, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	cronJob.CreatedAt = createdAt
	cronJob.UpdatedAt = updatedAt
	return &cronJob, nil
}

func scanWorker(scanner domainScanner) (*Worker, error) {
	var worker Worker
	var createdText, updatedText string
	if err := scanner.Scan(&worker.ID, &worker.DomainID, &worker.DomainName, &worker.Name, &worker.Command, &worker.Workdir, &worker.Status, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	worker.CreatedAt = createdAt
	worker.UpdatedAt = updatedAt
	return &worker, nil
}

func scanDomainEnvVar(scanner domainScanner) (*DomainEnvVar, error) {
	var envVar DomainEnvVar
	var createdText, updatedText string
	var isSecret int
	if err := scanner.Scan(&envVar.ID, &envVar.DomainID, &envVar.Name, &envVar.Value, &isSecret, &createdText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	envVar.IsSecret = isSecret != 0
	envVar.CreatedAt = createdAt
	envVar.UpdatedAt = updatedAt
	return &envVar, nil
}

func scanJob(scanner domainScanner) (*Job, error) {
	var job Job
	var createdText, startedText, finishedText, updatedText string
	if err := scanner.Scan(&job.ID, &job.Type, &job.Domain, &job.Status, &job.Log, &job.Error, &createdText, &startedText, &finishedText, &updatedText); err != nil {
		return nil, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return nil, err
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return nil, err
	}
	job.CreatedAt = createdAt
	job.UpdatedAt = updatedAt
	if startedText != "" {
		startedAt, err := time.Parse(time.RFC3339Nano, startedText)
		if err != nil {
			return nil, err
		}
		job.StartedAt = &startedAt
	}
	if finishedText != "" {
		finishedAt, err := time.Parse(time.RFC3339Nano, finishedText)
		if err != nil {
			return nil, err
		}
		job.FinishedAt = &finishedAt
	}
	return &job, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
