// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/StakoCP/stako/internal/crypto"
	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	auditMu sync.Mutex
}

func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite single writer; safe with WAL
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA synchronous=NORMAL;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS packages (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		owner_id TEXT,
		disk_mb INTEGER NOT NULL,
		bandwidth_mb INTEGER NOT NULL,
		domains INTEGER NOT NULL,
		databases INTEGER NOT NULL,
		mail_accounts INTEGER NOT NULL,
		ftp_accounts INTEGER NOT NULL,
		containers INTEGER NOT NULL,
		cpu_percent INTEGER NOT NULL,
		memory_mb INTEGER NOT NULL,
		shell_access INTEGER NOT NULL DEFAULT 0,
		php_versions TEXT NOT NULL DEFAULT '["8.3"]',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL UNIQUE,
		email TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		package_id TEXT REFERENCES packages(id),
		owner_id TEXT REFERENCES users(id),
		totp_enabled INTEGER NOT NULL DEFAULT 0,
		totp_secret TEXT NOT NULL DEFAULT '',
		shell TEXT NOT NULL DEFAULT 'nologin',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		last_login_at TEXT
	);
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		csrf_token TEXT NOT NULL,
		ip TEXT NOT NULL,
		user_agent TEXT NOT NULL,
		created_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		reauth_until TEXT
	);
	CREATE TABLE IF NOT EXISTS recovery_codes (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		code_hash TEXT NOT NULL,
		used INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		prefix TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		scopes TEXT NOT NULL,
		allowed_ips TEXT NOT NULL DEFAULT '[]',
		expires_at TEXT,
		last_used_at TEXT,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		at TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		actor_username TEXT NOT NULL,
		acting_as TEXT,
		action TEXT NOT NULL,
		object_type TEXT NOT NULL,
		object_id TEXT NOT NULL,
		result TEXT NOT NULL,
		ip TEXT NOT NULL,
		user_agent TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '{}',
		prev_hash TEXT NOT NULL,
		hash TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		created_by TEXT NOT NULL,
		payload TEXT NOT NULL DEFAULT '{}',
		log TEXT NOT NULL DEFAULT '',
		error TEXT,
		created_at TEXT NOT NULL,
		started_at TEXT,
		finished_at TEXT
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if err := s.migratePhase1(); err != nil {
		return err
	}
	if err := s.migratePhase2(); err != nil {
		return err
	}
	if err := s.migratePhase3(); err != nil {
		return err
	}
	if err := s.migratePhase4(); err != nil {
		return err
	}
	if err := s.migratePhase5(); err != nil {
		return err
	}
	if err := s.migratePhase6(); err != nil {
		return err
	}
	return s.seedDefaultPackages()
}

func (s *Store) seedDefaultPackages() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM packages`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	now := time.Now().UTC()
	defaults := []Package{
		{
			ID: "p_basic", Name: "Basic", DiskMB: 10240, BandwidthMB: -1,
			Domains: 5, Databases: 5, MailAccounts: 10, FTPAccounts: 2, Containers: 2,
			CPUPercent: 100, MemoryMB: 1024, ShellAccess: false, PHPVersions: []string{"8.4"},
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "p_standard", Name: "Standard", DiskMB: 51200, BandwidthMB: -1,
			Domains: 25, Databases: 25, MailAccounts: 50, FTPAccounts: 10, Containers: 10,
			CPUPercent: 200, MemoryMB: 4096, ShellAccess: true, PHPVersions: []string{"8.3", "8.4", "8.5"},
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for _, p := range defaults {
		if err := s.CreatePackage(context.Background(), &p); err != nil {
			return err
		}
	}
	return nil
}

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isFKConstraintErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseNullTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// --- Users ---

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var pkgID, ownerID, lastLogin sql.NullString
	var totpEnabled int
	var createdAt, updatedAt string
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Status,
		&pkgID, &ownerID, &totpEnabled, &u.TOTPSecret, &u.Shell,
		&createdAt, &updatedAt, &lastLogin,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if pkgID.Valid {
		u.PackageID = &pkgID.String
	}
	if ownerID.Valid {
		u.OwnerID = &ownerID.String
	}
	u.TOTPEnabled = totpEnabled != 0
	u.CreatedAt = parseTime(createdAt)
	u.UpdatedAt = parseTime(updatedAt)
	u.LastLoginAt = parseNullTime(lastLogin)
	return &u, nil
}

const userCols = `id, username, email, password_hash, role, status, package_id, owner_id, totp_enabled, totp_secret, shell, created_at, updated_at, last_login_at`

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = u.CreatedAt
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO users (`+userCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.Email, u.PasswordHash, u.Role, u.Status,
		u.PackageID, u.OwnerID, boolInt(u.TOTPEnabled), u.TOTPSecret, u.Shell,
		formatTime(u.CreatedAt), formatTime(u.UpdatedAt), nil,
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE username=?`, username))
}

func (s *Store) ListUsers(ctx context.Context, ownerID *string, search string, limit, offset int) ([]User, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if ownerID != nil {
		where = append(where, "(owner_id = ? OR id = ?)")
		args = append(args, *ownerID, *ownerID)
	}
	if search != "" {
		where = append(where, "(username LIKE ? OR email LIKE ?)")
		q := "%" + search + "%"
		args = append(args, q, q)
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users WHERE `+w+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, qArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	u.UpdatedAt = time.Now().UTC()
	var lastLogin any
	if u.LastLoginAt != nil {
		lastLogin = formatTime(*u.LastLoginAt)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET email=?, password_hash=?, role=?, status=?, package_id=?, totp_enabled=?, totp_secret=?, shell=?, updated_at=?, last_login_at=? WHERE id=?`,
		u.Email, u.PasswordHash, u.Role, u.Status, u.PackageID, boolInt(u.TOTPEnabled), u.TOTPSecret, u.Shell, formatTime(u.UpdatedAt), lastLogin, u.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if isFKConstraintErr(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Packages ---

func scanPackage(row interface{ Scan(...any) error }) (*Package, error) {
	var p Package
	var ownerID sql.NullString
	var shellInt int
	var phpJSON, createdAt, updatedAt string
	err := row.Scan(
		&p.ID, &p.Name, &ownerID, &p.DiskMB, &p.BandwidthMB,
		&p.Domains, &p.Databases, &p.MailAccounts, &p.FTPAccounts, &p.Containers,
		&p.CPUPercent, &p.MemoryMB, &shellInt, &phpJSON, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ownerID.Valid {
		p.OwnerID = &ownerID.String
	}
	p.ShellAccess = shellInt != 0
	_ = json.Unmarshal([]byte(phpJSON), &p.PHPVersions)
	if p.PHPVersions == nil {
		p.PHPVersions = []string{}
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	return &p, nil
}

const pkgCols = `id, name, owner_id, disk_mb, bandwidth_mb, domains, databases, mail_accounts, ftp_accounts, containers, cpu_percent, memory_mb, shell_access, php_versions, created_at, updated_at`

func (s *Store) CreatePackage(ctx context.Context, p *Package) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = p.CreatedAt
	phpJSON, _ := json.Marshal(p.PHPVersions)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO packages (`+pkgCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.OwnerID, p.DiskMB, p.BandwidthMB,
		p.Domains, p.Databases, p.MailAccounts, p.FTPAccounts, p.Containers,
		p.CPUPercent, p.MemoryMB, boolInt(p.ShellAccess), string(phpJSON),
		formatTime(p.CreatedAt), formatTime(p.UpdatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetPackageByID(ctx context.Context, id string) (*Package, error) {
	return scanPackage(s.db.QueryRowContext(ctx, `SELECT `+pkgCols+` FROM packages WHERE id=?`, id))
}

func (s *Store) ListPackages(ctx context.Context, ownerID *string) ([]Package, error) {
	q := `SELECT ` + pkgCols + ` FROM packages`
	var args []any
	if ownerID != nil {
		q += ` WHERE owner_id IS NULL OR owner_id = ?`
		args = append(args, *ownerID)
	}
	q += ` ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Package{}
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) UpdatePackage(ctx context.Context, p *Package) error {
	p.UpdatedAt = time.Now().UTC()
	phpJSON, _ := json.Marshal(p.PHPVersions)
	res, err := s.db.ExecContext(ctx,
		`UPDATE packages SET name=?, disk_mb=?, bandwidth_mb=?, domains=?, databases=?, mail_accounts=?, ftp_accounts=?, containers=?, cpu_percent=?, memory_mb=?, shell_access=?, php_versions=?, updated_at=? WHERE id=?`,
		p.Name, p.DiskMB, p.BandwidthMB, p.Domains, p.Databases, p.MailAccounts, p.FTPAccounts, p.Containers,
		p.CPUPercent, p.MemoryMB, boolInt(p.ShellAccess), string(phpJSON), formatTime(p.UpdatedAt), p.ID,
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePackage(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM packages WHERE id=?`, id)
	if isFKConstraintErr(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *Session) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, csrf_token, ip, user_agent, created_at, last_seen_at, expires_at, reauth_until) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, sess.UserID, sess.TokenHash, sess.CSRFToken, sess.IP, sess.UserAgent,
		formatTime(sess.CreatedAt), formatTime(sess.LastSeenAt), formatTime(sess.ExpiresAt), nil,
	)
	return err
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	var sess Session
	var createdAt, lastSeen, expiresAt string
	var reauth, actingAs, impID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, acting_as_user_id, impersonation_id, token_hash, csrf_token, ip, user_agent, created_at, last_seen_at, expires_at, reauth_until FROM sessions WHERE token_hash=?`,
		tokenHash,
	).Scan(&sess.ID, &sess.UserID, &actingAs, &impID, &sess.TokenHash, &sess.CSRFToken, &sess.IP, &sess.UserAgent, &createdAt, &lastSeen, &expiresAt, &reauth)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if actingAs.Valid {
		sess.ActingAsUserID = &actingAs.String
	}
	if impID.Valid {
		sess.ImpersonationID = &impID.String
	}
	sess.CreatedAt = parseTime(createdAt)
	sess.LastSeenAt = parseTime(lastSeen)
	sess.ExpiresAt = parseTime(expiresAt)
	sess.ReauthUntil = parseNullTime(reauth)
	return &sess, nil
}

func (s *Store) TouchSession(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, formatTime(now), id)
	return err
}

func (s *Store) SetSessionReauth(ctx context.Context, id string, until time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sessions SET reauth_until=? WHERE id=?`, formatTime(until), id)
	return err
}

func (s *Store) ListSessionsByUser(ctx context.Context, userID, currentID string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ip, user_agent, created_at, last_seen_at FROM sessions WHERE user_id=? AND expires_at > ? ORDER BY last_seen_at DESC`,
		userID, formatTime(time.Now().UTC()),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var sess Session
		var createdAt, lastSeen string
		if err := rows.Scan(&sess.ID, &sess.IP, &sess.UserAgent, &createdAt, &lastSeen); err != nil {
			return nil, err
		}
		sess.CreatedAt = parseTime(createdAt)
		sess.LastSeenAt = parseTime(lastSeen)
		sess.Current = sess.ID == currentID
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSession(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id != ?`, userID, keepID)
	return err
}

func (s *Store) DeleteAllSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}

// --- Recovery Codes ---

func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		id, _ := crypto.RandomID("rc")
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (id, user_id, code_hash, used) VALUES (?,?,?,0)`, id, userID, h); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID, codeHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used=1 WHERE user_id=? AND code_hash=? AND used=0`, userID, codeHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// --- API Keys ---

func (s *Store) CreateApiKey(ctx context.Context, k *ApiKey) error {
	scopesJSON, _ := json.Marshal(k.Scopes)
	ipsJSON, _ := json.Marshal(k.AllowedIPs)
	var exp any
	if k.ExpiresAt != nil {
		exp = formatTime(*k.ExpiresAt)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, user_id, name, prefix, key_hash, scopes, allowed_ips, expires_at, created_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		k.ID, k.UserID, k.Name, k.Prefix, k.KeyHash, string(scopesJSON), string(ipsJSON), exp, formatTime(k.CreatedAt),
	)
	return err
}

func (s *Store) GetApiKeyByHash(ctx context.Context, keyHash string) (*ApiKey, error) {
	var k ApiKey
	var scopesJSON, ipsJSON, createdAt string
	var exp, lastUsed sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, prefix, key_hash, scopes, allowed_ips, expires_at, last_used_at, created_at FROM api_keys WHERE key_hash=?`,
		keyHash,
	).Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.KeyHash, &scopesJSON, &ipsJSON, &exp, &lastUsed, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
	_ = json.Unmarshal([]byte(ipsJSON), &k.AllowedIPs)
	k.ExpiresAt = parseNullTime(exp)
	k.LastUsedAt = parseNullTime(lastUsed)
	k.CreatedAt = parseTime(createdAt)
	return &k, nil
}

func (s *Store) TouchApiKey(ctx context.Context, id string, now time.Time) {
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=?`, formatTime(now), id)
}

func (s *Store) ListApiKeysByUser(ctx context.Context, userID string) ([]ApiKey, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, prefix, scopes, allowed_ips, expires_at, last_used_at, created_at FROM api_keys WHERE user_id=? ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ApiKey{}
	for rows.Next() {
		var k ApiKey
		var scopesJSON, ipsJSON, createdAt string
		var exp, lastUsed sql.NullString
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &scopesJSON, &ipsJSON, &exp, &lastUsed, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopesJSON), &k.Scopes)
		_ = json.Unmarshal([]byte(ipsJSON), &k.AllowedIPs)
		k.ExpiresAt = parseNullTime(exp)
		k.LastUsedAt = parseNullTime(lastUsed)
		k.CreatedAt = parseTime(createdAt)
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteApiKey(ctx context.Context, id, userID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id=? AND user_id=?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Audit Log (tamper-evident hash chain) ---

func computeAuditHash(prevHash string, e *AuditEntry) string {
	acting := ""
	if e.ActingAs != nil {
		acting = *e.ActingAs
	}
	canonical := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		prevHash, formatTime(e.At), e.ActorID, e.ActorUsername, acting,
		e.Action, e.ObjectType, e.ObjectID, e.Result, e.IP, string(e.Detail),
	)
	return crypto.SHA256Hex(canonical)
}

func (s *Store) AppendAudit(ctx context.Context, e *AuditEntry) error {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()

	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	if len(e.Detail) == 0 {
		e.Detail = json.RawMessage(`{}`)
	}
	var prevHash string
	err := s.db.QueryRowContext(ctx, `SELECT hash FROM audit_logs ORDER BY id DESC LIMIT 1`).Scan(&prevHash)
	if errors.Is(err, sql.ErrNoRows) {
		prevHash = strings.Repeat("0", 64)
	} else if err != nil {
		return err
	}
	e.PrevHash = prevHash
	e.Hash = computeAuditHash(prevHash, e)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (at, actor_id, actor_username, acting_as, action, object_type, object_id, result, ip, user_agent, detail, prev_hash, hash) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		formatTime(e.At), e.ActorID, e.ActorUsername, e.ActingAs, e.Action, e.ObjectType, e.ObjectID, e.Result, e.IP, e.UserAgent, string(e.Detail), e.PrevHash, e.Hash,
	)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) ListAudit(ctx context.Context, action, actor string, limit, offset int) ([]AuditEntry, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if action != "" {
		where = append(where, "action LIKE ?")
		args = append(args, "%"+action+"%")
	}
	if actor != "" {
		where = append(where, "actor_username LIKE ?")
		args = append(args, "%"+actor+"%")
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor_id, actor_username, acting_as, action, object_type, object_id, result, ip, user_agent, detail, prev_hash, hash FROM audit_logs WHERE `+w+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		qArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var at, detail string
		var actingAs sql.NullString
		if err := rows.Scan(&e.ID, &at, &e.ActorID, &e.ActorUsername, &actingAs, &e.Action, &e.ObjectType, &e.ObjectID, &e.Result, &e.IP, &e.UserAgent, &detail, &e.PrevHash, &e.Hash); err != nil {
			return nil, 0, err
		}
		e.At = parseTime(at)
		if actingAs.Valid {
			e.ActingAs = &actingAs.String
		}
		e.Detail = json.RawMessage(detail)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *Store) VerifyAuditChain(ctx context.Context) (valid bool, checked int, brokenAt *int64, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, at, actor_id, actor_username, acting_as, action, object_type, object_id, result, ip, user_agent, detail, prev_hash, hash FROM audit_logs ORDER BY id ASC`,
	)
	if err != nil {
		return false, 0, nil, err
	}
	defer rows.Close()

	expectedPrev := strings.Repeat("0", 64)
	for rows.Next() {
		var e AuditEntry
		var at, detail string
		var actingAs sql.NullString
		if err := rows.Scan(&e.ID, &at, &e.ActorID, &e.ActorUsername, &actingAs, &e.Action, &e.ObjectType, &e.ObjectID, &e.Result, &e.IP, &e.UserAgent, &detail, &e.PrevHash, &e.Hash); err != nil {
			return false, checked, nil, err
		}
		e.At = parseTime(at)
		if actingAs.Valid {
			e.ActingAs = &actingAs.String
		}
		e.Detail = json.RawMessage(detail)
		checked++
		if e.PrevHash != expectedPrev || computeAuditHash(expectedPrev, &e) != e.Hash {
			id := e.ID
			return false, checked, &id, nil
		}
		expectedPrev = e.Hash
	}
	return true, checked, nil, rows.Err()
}

// --- Jobs ---

func (s *Store) CreateJob(ctx context.Context, j *Job) error {
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	if len(j.Payload) == 0 {
		j.Payload = json.RawMessage(`{}`)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO jobs (id, type, status, created_by, payload, log, created_at) VALUES (?,?,?,?,?,?,?)`,
		j.ID, j.Type, j.Status, j.CreatedBy, string(j.Payload), j.Log, formatTime(j.CreatedAt),
	)
	return err
}

func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	var j Job
	var payload, createdAt string
	var errStr, startedAt, finishedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, status, created_by, payload, log, error, created_at, started_at, finished_at FROM jobs WHERE id=?`,
		id,
	).Scan(&j.ID, &j.Type, &j.Status, &j.CreatedBy, &payload, &j.Log, &errStr, &createdAt, &startedAt, &finishedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	j.Payload = json.RawMessage(payload)
	if errStr.Valid {
		j.Error = &errStr.String
	}
	j.CreatedAt = parseTime(createdAt)
	j.StartedAt = parseNullTime(startedAt)
	j.FinishedAt = parseNullTime(finishedAt)
	return &j, nil
}

func (s *Store) ListJobs(ctx context.Context, createdBy *string, limit, offset int) ([]Job, int, error) {
	where := "1=1"
	var args []any
	if createdBy != nil {
		where = "created_by = ?"
		args = append(args, *createdBy)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, status, created_by, payload, log, error, created_at, started_at, finished_at FROM jobs WHERE `+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		qArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var j Job
		var payload, createdAt string
		var errStr, startedAt, finishedAt sql.NullString
		if err := rows.Scan(&j.ID, &j.Type, &j.Status, &j.CreatedBy, &payload, &j.Log, &errStr, &createdAt, &startedAt, &finishedAt); err != nil {
			return nil, 0, err
		}
		j.Payload = json.RawMessage(payload)
		if errStr.Valid {
			j.Error = &errStr.String
		}
		j.CreatedAt = parseTime(createdAt)
		j.StartedAt = parseNullTime(startedAt)
		j.FinishedAt = parseNullTime(finishedAt)
		out = append(out, j)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateJob(ctx context.Context, j *Job) error {
	var started, finished any
	if j.StartedAt != nil {
		started = formatTime(*j.StartedAt)
	}
	if j.FinishedAt != nil {
		finished = formatTime(*j.FinishedAt)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status=?, log=?, error=?, started_at=?, finished_at=? WHERE id=?`,
		j.Status, j.Log, j.Error, started, finished, j.ID,
	)
	return err
}

func (s *Store) Counts(ctx context.Context) (users, packages, jobsRunning int, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM packages`).Scan(&packages); err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE status IN ('queued','running')`).Scan(&jobsRunning)
	return
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
