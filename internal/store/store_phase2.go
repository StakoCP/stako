// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Store) migratePhase2() error {
	schema := `
	CREATE TABLE IF NOT EXISTS databases (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		engine TEXT NOT NULL,
		db_user TEXT NOT NULL,
		remote_hosts TEXT NOT NULL DEFAULT '["127.0.0.1"]',
		size_mb INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		UNIQUE(engine, name)
	);
	CREATE TABLE IF NOT EXISTS ftp_accounts (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		username TEXT NOT NULL UNIQUE,
		home_dir TEXT NOT NULL,
		quota_mb INTEGER NOT NULL DEFAULT 1024,
		protocol TEXT NOT NULL DEFAULT 'sftp',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS firewall_rules (
		id TEXT PRIMARY KEY,
		action TEXT NOT NULL,
		protocol TEXT NOT NULL,
		port TEXT NOT NULL,
		source_cidr TEXT NOT NULL DEFAULT '0.0.0.0/0',
		comment TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS security_findings (
		id TEXT PRIMARY KEY,
		category TEXT NOT NULL,
		severity TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT NOT NULL,
		remediation TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'open',
		component TEXT NOT NULL,
		detected_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	return s.seedDefaultFirewall()
}

func (s *Store) seedDefaultFirewall() error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM firewall_rules`).Scan(&count); err != nil || count > 0 {
		return err
	}
	now := time.Now().UTC()
	rules := []FirewallRule{
		{ID: "fw_ssh", Action: "allow", Protocol: "tcp", Port: "22", SourceCIDR: "0.0.0.0/0", Comment: "SSH", CreatedAt: now},
		{ID: "fw_http", Action: "allow", Protocol: "tcp", Port: "80", SourceCIDR: "0.0.0.0/0", Comment: "HTTP", CreatedAt: now},
		{ID: "fw_https", Action: "allow", Protocol: "tcp", Port: "443", SourceCIDR: "0.0.0.0/0", Comment: "HTTPS", CreatedAt: now},
		{ID: "fw_panel", Action: "allow", Protocol: "tcp", Port: "8083", SourceCIDR: "0.0.0.0/0", Comment: "Stako Panel", CreatedAt: now},
	}
	for _, r := range rules {
		if err := s.CreateFirewallRule(context.Background(), &r); err != nil {
			return err
		}
	}
	return nil
}

// --- Databases ---

func (s *Store) CreateDatabase(ctx context.Context, d *Database) error {
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC()
	}
	hostsJSON, _ := json.Marshal(d.RemoteHosts)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO databases (id, user_id, name, engine, db_user, remote_hosts, size_mb, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, d.Name, d.Engine, d.DBUser, string(hostsJSON), d.SizeMB, formatTime(d.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetDatabase(ctx context.Context, id string) (*Database, error) {
	var d Database
	var hostsJSON, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, engine, db_user, remote_hosts, size_mb, created_at FROM databases WHERE id=?`, id,
	).Scan(&d.ID, &d.UserID, &d.Name, &d.Engine, &d.DBUser, &hostsJSON, &d.SizeMB, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(hostsJSON), &d.RemoteHosts)
	d.CreatedAt = parseTime(createdAt)
	return &d, nil
}

func (s *Store) ListDatabases(ctx context.Context, userID, resellerID *string, limit, offset int) ([]Database, int, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "d.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(d.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM databases d JOIN users u ON d.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+join, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT d.id, d.user_id, d.name, d.engine, d.db_user, d.remote_hosts, d.size_mb, d.created_at `+join+` ORDER BY d.created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		var d Database
		var hostsJSON, createdAt string
		if err := rows.Scan(&d.ID, &d.UserID, &d.Name, &d.Engine, &d.DBUser, &hostsJSON, &d.SizeMB, &createdAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(hostsJSON), &d.RemoteHosts)
		d.CreatedAt = parseTime(createdAt)
		out = append(out, d)
	}
	return out, total, rows.Err()
}

func (s *Store) DeleteDatabase(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM databases WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- FTP Accounts ---

func (s *Store) CreateFtpAccount(ctx context.Context, f *FtpAccount) error {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ftp_accounts (id, user_id, username, home_dir, quota_mb, protocol, created_at) VALUES (?,?,?,?,?,?,?)`,
		f.ID, f.UserID, f.Username, f.HomeDir, f.QuotaMB, f.Protocol, formatTime(f.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetFtpAccount(ctx context.Context, id string) (*FtpAccount, error) {
	var f FtpAccount
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, username, home_dir, quota_mb, protocol, created_at FROM ftp_accounts WHERE id=?`, id,
	).Scan(&f.ID, &f.UserID, &f.Username, &f.HomeDir, &f.QuotaMB, &f.Protocol, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	f.CreatedAt = parseTime(createdAt)
	return &f, nil
}

func (s *Store) ListFtpAccounts(ctx context.Context, userID, resellerID *string) ([]FtpAccount, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "f.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(f.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM ftp_accounts f JOIN users u ON f.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.user_id, f.username, f.home_dir, f.quota_mb, f.protocol, f.created_at `+join+` ORDER BY f.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FtpAccount{}
	for rows.Next() {
		var f FtpAccount
		var createdAt string
		if err := rows.Scan(&f.ID, &f.UserID, &f.Username, &f.HomeDir, &f.QuotaMB, &f.Protocol, &createdAt); err != nil {
			return nil, err
		}
		f.CreatedAt = parseTime(createdAt)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFtpAccount(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ftp_accounts WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Firewall Rules ---

func (s *Store) CreateFirewallRule(ctx context.Context, r *FirewallRule) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO firewall_rules (id, action, protocol, port, source_cidr, comment, created_at) VALUES (?,?,?,?,?,?,?)`,
		r.ID, r.Action, r.Protocol, r.Port, r.SourceCIDR, r.Comment, formatTime(r.CreatedAt),
	)
	return err
}

func (s *Store) ListFirewallRules(ctx context.Context) ([]FirewallRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, action, protocol, port, source_cidr, comment, created_at FROM firewall_rules ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FirewallRule{}
	for rows.Next() {
		var r FirewallRule
		var createdAt string
		if err := rows.Scan(&r.ID, &r.Action, &r.Protocol, &r.Port, &r.SourceCIDR, &r.Comment, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteFirewallRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM firewall_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Security Findings ---

func (s *Store) UpsertSecurityFinding(ctx context.Context, f *SecurityFinding) error {
	now := time.Now().UTC()
	if f.DetectedAt.IsZero() {
		f.DetectedAt = now
	}
	f.UpdatedAt = now
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO security_findings (id, category, severity, title, description, remediation, status, component, detected_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET severity=excluded.severity, title=excluded.title, description=excluded.description, remediation=excluded.remediation, updated_at=excluded.updated_at`,
		f.ID, f.Category, f.Severity, f.Title, f.Description, f.Remediation, f.Status, f.Component, formatTime(f.DetectedAt), formatTime(f.UpdatedAt),
	)
	return err
}

func (s *Store) ListSecurityFindings(ctx context.Context) ([]SecurityFinding, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, category, severity, title, description, remediation, status, component, detected_at, updated_at FROM security_findings ORDER BY detected_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SecurityFinding{}
	for rows.Next() {
		var f SecurityFinding
		var det, upd string
		if err := rows.Scan(&f.ID, &f.Category, &f.Severity, &f.Title, &f.Description, &f.Remediation, &f.Status, &f.Component, &det, &upd); err != nil {
			return nil, err
		}
		f.DetectedAt = parseTime(det)
		f.UpdatedAt = parseTime(upd)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSecurityFindingStatus(ctx context.Context, id, status string) (*SecurityFinding, error) {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE security_findings SET status=?, updated_at=? WHERE id=?`, status, formatTime(now), id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	var f SecurityFinding
	var det, upd string
	err = s.db.QueryRowContext(ctx,
		`SELECT id, category, severity, title, description, remediation, status, component, detected_at, updated_at FROM security_findings WHERE id=?`, id,
	).Scan(&f.ID, &f.Category, &f.Severity, &f.Title, &f.Description, &f.Remediation, &f.Status, &f.Component, &det, &upd)
	if err != nil {
		return nil, err
	}
	f.DetectedAt = parseTime(det)
	f.UpdatedAt = parseTime(upd)
	return &f, nil
}
