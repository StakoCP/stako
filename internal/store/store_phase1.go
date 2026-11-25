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

func (s *Store) migratePhase1() error {
	// Add impersonation columns to sessions if missing
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN acting_as_user_id TEXT`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN impersonation_id TEXT`)

	schema := `
	CREATE TABLE IF NOT EXISTS impersonation_sessions (
		id TEXT PRIMARY KEY,
		actor_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		target_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		reason TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL,
		created_at TEXT NOT NULL,
		ended_at TEXT
	);
	CREATE TABLE IF NOT EXISTS ip_addresses (
		id TEXT PRIMARY KEY,
		ip TEXT NOT NULL UNIQUE,
		family TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'shared',
		visibility TEXT NOT NULL DEFAULT 'public',
		interface_name TEXT NOT NULL DEFAULT 'eth0',
		owner_id TEXT REFERENCES users(id),
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS domains (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain TEXT NOT NULL UNIQUE,
		aliases TEXT NOT NULL DEFAULT '[]',
		stack TEXT NOT NULL DEFAULT 'nginx',
		template TEXT NOT NULL DEFAULT 'php',
		docroot TEXT NOT NULL,
		php_version TEXT NOT NULL DEFAULT '8.3',
		ssl_enabled INTEGER NOT NULL DEFAULT 0,
		ssl_mode TEXT NOT NULL DEFAULT 'none',
		force_https INTEGER NOT NULL DEFAULT 1,
		hsts INTEGER NOT NULL DEFAULT 0,
		ipv4_id TEXT REFERENCES ip_addresses(id),
		ipv6_id TEXT REFERENCES ip_addresses(id),
		proxy_target TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS proxy_rules (
		id TEXT PRIMARY KEY,
		domain_id TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
		path TEXT NOT NULL,
		upstream TEXT NOT NULL,
		websocket INTEGER NOT NULL DEFAULT 1,
		sse INTEGER NOT NULL DEFAULT 0,
		grpc INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		UNIQUE(domain_id, path)
	);
	CREATE TABLE IF NOT EXISTS apps (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain_id TEXT REFERENCES domains(id) ON DELETE SET NULL,
		name TEXT NOT NULL,
		runtime TEXT NOT NULL,
		runtime_version TEXT NOT NULL DEFAULT '',
		work_dir TEXT NOT NULL,
		command TEXT NOT NULL,
		port INTEGER NOT NULL UNIQUE,
		env_keys TEXT NOT NULL DEFAULT '[]',
		env_enc TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'stopped',
		unit_name TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(user_id, name)
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// --- Impersonation ---

func (s *Store) StartImpersonation(ctx context.Context, sessID string, imp *ImpersonationSession) error {
	if imp.CreatedAt.IsZero() {
		imp.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO impersonation_sessions (id, actor_id, target_id, reason, ip, created_at) VALUES (?,?,?,?,?,?)`,
		imp.ID, imp.ActorID, imp.TargetID, imp.Reason, imp.IP, formatTime(imp.CreatedAt),
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET acting_as_user_id=?, impersonation_id=? WHERE id=?`,
		imp.TargetID, imp.ID, sessID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EndImpersonation(ctx context.Context, sessID string, impID *string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET acting_as_user_id=NULL, impersonation_id=NULL WHERE id=?`, sessID); err != nil {
		return err
	}
	if impID != nil {
		_, _ = tx.ExecContext(ctx, `UPDATE impersonation_sessions SET ended_at=? WHERE id=?`, formatTime(time.Now().UTC()), *impID)
	}
	return tx.Commit()
}

// --- IP Addresses ---

func (s *Store) CreateIP(ctx context.Context, ip *IPAddress) error {
	if ip.CreatedAt.IsZero() {
		ip.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO ip_addresses (id, ip, family, scope, visibility, interface_name, owner_id, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		ip.ID, ip.IP, ip.Family, ip.Scope, ip.Visibility, ip.InterfaceName, ip.OwnerID, formatTime(ip.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetIP(ctx context.Context, id string) (*IPAddress, error) {
	var ip IPAddress
	var ownerID sql.NullString
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, ip, family, scope, visibility, interface_name, owner_id, created_at FROM ip_addresses WHERE id=?`, id).
		Scan(&ip.ID, &ip.IP, &ip.Family, &ip.Scope, &ip.Visibility, &ip.InterfaceName, &ownerID, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ownerID.Valid {
		ip.OwnerID = &ownerID.String
	}
	ip.CreatedAt = parseTime(createdAt)
	return &ip, nil
}

func (s *Store) ListIPs(ctx context.Context, role, userID string) ([]IPAddress, error) {
	q := `SELECT id, ip, family, scope, visibility, interface_name, owner_id, created_at FROM ip_addresses`
	var args []any
	if role == "reseller" {
		q += ` WHERE scope='shared' OR owner_id=?`
		args = append(args, userID)
	} else if role != "admin" {
		q += ` WHERE scope='shared'`
	}
	q += ` ORDER BY family ASC, ip ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IPAddress{}
	for rows.Next() {
		var ip IPAddress
		var ownerID sql.NullString
		var createdAt string
		if err := rows.Scan(&ip.ID, &ip.IP, &ip.Family, &ip.Scope, &ip.Visibility, &ip.InterfaceName, &ownerID, &createdAt); err != nil {
			return nil, err
		}
		if ownerID.Valid {
			ip.OwnerID = &ownerID.String
		}
		ip.CreatedAt = parseTime(createdAt)
		out = append(out, ip)
	}
	return out, rows.Err()
}

func (s *Store) DeleteIP(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM ip_addresses WHERE id=?`, id)
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

// --- Domains ---

const domainCols = `id, user_id, domain, aliases, stack, template, docroot, php_version, ssl_enabled, ssl_mode, force_https, hsts, ipv4_id, ipv6_id, proxy_target, status, created_at, updated_at`

func scanDomain(row interface{ Scan(...any) error }) (*Domain, error) {
	var d Domain
	var aliasesJSON, createdAt, updatedAt string
	var sslInt, forceInt, hstsInt int
	var ipv4, ipv6 sql.NullString
	err := row.Scan(
		&d.ID, &d.UserID, &d.Domain, &aliasesJSON, &d.Stack, &d.Template, &d.Docroot,
		&d.PHPVersion, &sslInt, &d.SSLMode, &forceInt, &hstsInt, &ipv4, &ipv6,
		&d.ProxyTarget, &d.Status, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(aliasesJSON), &d.Aliases)
	if d.Aliases == nil {
		d.Aliases = []string{}
	}
	d.SSLEnabled = sslInt != 0
	d.ForceHTTPS = forceInt != 0
	d.HSTS = hstsInt != 0
	if ipv4.Valid {
		d.IPv4ID = &ipv4.String
	}
	if ipv6.Valid {
		d.IPv6ID = &ipv6.String
	}
	d.CreatedAt = parseTime(createdAt)
	d.UpdatedAt = parseTime(updatedAt)
	return &d, nil
}

func (s *Store) CreateDomain(ctx context.Context, d *Domain) error {
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = d.CreatedAt
	aliasesJSON, _ := json.Marshal(d.Aliases)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO domains (`+domainCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.UserID, d.Domain, string(aliasesJSON), d.Stack, d.Template, d.Docroot,
		d.PHPVersion, boolInt(d.SSLEnabled), d.SSLMode, boolInt(d.ForceHTTPS), boolInt(d.HSTS),
		d.IPv4ID, d.IPv6ID, d.ProxyTarget, d.Status, formatTime(d.CreatedAt), formatTime(d.UpdatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetDomain(ctx context.Context, id string) (*Domain, error) {
	return scanDomain(s.db.QueryRowContext(ctx, `SELECT `+domainCols+` FROM domains WHERE id=?`, id))
}

func (s *Store) ListDomains(ctx context.Context, userID *string, resellerID *string, search string, limit, offset int) ([]Domain, int, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "d.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(d.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	if search != "" {
		where = append(where, "d.domain LIKE ?")
		args = append(args, "%"+search+"%")
	}
	w := strings.Join(where, " AND ")
	join := `FROM domains d JOIN users u ON d.user_id = u.id WHERE ` + w
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+join, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, limit, offset)
	cols := `d.id, d.user_id, d.domain, d.aliases, d.stack, d.template, d.docroot, d.php_version, d.ssl_enabled, d.ssl_mode, d.force_https, d.hsts, d.ipv4_id, d.ipv6_id, d.proxy_target, d.status, d.created_at, d.updated_at`
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` `+join+` ORDER BY d.created_at DESC LIMIT ? OFFSET ?`, qArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *d)
	}
	return out, total, rows.Err()
}

func (s *Store) CountDomainsByUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM domains WHERE user_id=?`, userID).Scan(&n)
	return n, err
}

func (s *Store) UpdateDomain(ctx context.Context, d *Domain) error {
	d.UpdatedAt = time.Now().UTC()
	aliasesJSON, _ := json.Marshal(d.Aliases)
	res, err := s.db.ExecContext(ctx,
		`UPDATE domains SET aliases=?, stack=?, template=?, docroot=?, php_version=?, ssl_enabled=?, ssl_mode=?, force_https=?, hsts=?, ipv4_id=?, ipv6_id=?, proxy_target=?, status=?, updated_at=? WHERE id=?`,
		string(aliasesJSON), d.Stack, d.Template, d.Docroot, d.PHPVersion, boolInt(d.SSLEnabled), d.SSLMode, boolInt(d.ForceHTTPS), boolInt(d.HSTS),
		d.IPv4ID, d.IPv6ID, d.ProxyTarget, d.Status, formatTime(d.UpdatedAt), d.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDomain(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM domains WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Proxy Rules ---

func (s *Store) ListProxyRules(ctx context.Context, domainID string) ([]ProxyRule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, domain_id, path, upstream, websocket, sse, grpc, created_at FROM proxy_rules WHERE domain_id=? ORDER BY path ASC`,
		domainID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProxyRule{}
	for rows.Next() {
		var r ProxyRule
		var ws, sse, grpc int
		var createdAt string
		if err := rows.Scan(&r.ID, &r.DomainID, &r.Path, &r.Upstream, &ws, &sse, &grpc, &createdAt); err != nil {
			return nil, err
		}
		r.WebSocket = ws != 0
		r.SSE = sse != 0
		r.GRPC = grpc != 0
		r.CreatedAt = parseTime(createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateProxyRule(ctx context.Context, r *ProxyRule) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO proxy_rules (id, domain_id, path, upstream, websocket, sse, grpc, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		r.ID, r.DomainID, r.Path, r.Upstream, boolInt(r.WebSocket), boolInt(r.SSE), boolInt(r.GRPC), formatTime(r.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) DeleteProxyRule(ctx context.Context, domainID, ruleID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM proxy_rules WHERE id=? AND domain_id=?`, ruleID, domainID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Apps ---

const appCols = `id, user_id, domain_id, name, runtime, runtime_version, work_dir, command, port, env_keys, env_enc, status, unit_name, created_at, updated_at`

func scanApp(row interface{ Scan(...any) error }) (*App, error) {
	var a App
	var domainID sql.NullString
	var envKeysJSON, createdAt, updatedAt string
	err := row.Scan(
		&a.ID, &a.UserID, &domainID, &a.Name, &a.Runtime, &a.RuntimeVersion,
		&a.WorkDir, &a.Command, &a.Port, &envKeysJSON, &a.EnvEnc,
		&a.Status, &a.UnitName, &createdAt, &updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if domainID.Valid {
		a.DomainID = &domainID.String
	}
	_ = json.Unmarshal([]byte(envKeysJSON), &a.EnvKeys)
	if a.EnvKeys == nil {
		a.EnvKeys = []string{}
	}
	a.CreatedAt = parseTime(createdAt)
	a.UpdatedAt = parseTime(updatedAt)
	return &a, nil
}

func (s *Store) NextFreeAppPort(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT port FROM apps`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err == nil {
			used[p] = true
		}
	}
	for port := 3000; port < 4000; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, errors.New("no free app port available in 3000-3999")
}

func (s *Store) CreateApp(ctx context.Context, a *App) error {
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = a.CreatedAt
	keysJSON, _ := json.Marshal(a.EnvKeys)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO apps (`+appCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.UserID, a.DomainID, a.Name, a.Runtime, a.RuntimeVersion,
		a.WorkDir, a.Command, a.Port, string(keysJSON), a.EnvEnc,
		a.Status, a.UnitName, formatTime(a.CreatedAt), formatTime(a.UpdatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetApp(ctx context.Context, id string) (*App, error) {
	return scanApp(s.db.QueryRowContext(ctx, `SELECT `+appCols+` FROM apps WHERE id=?`, id))
}

func (s *Store) ListApps(ctx context.Context, userID, resellerID *string, limit, offset int) ([]App, int, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "a.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(a.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	w := strings.Join(where, " AND ")
	join := `FROM apps a JOIN users u ON a.user_id = u.id WHERE ` + w
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+join, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	qArgs := append(args, limit, offset)
	cols := `a.id, a.user_id, a.domain_id, a.name, a.runtime, a.runtime_version, a.work_dir, a.command, a.port, a.env_keys, a.env_enc, a.status, a.unit_name, a.created_at, a.updated_at`
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` `+join+` ORDER BY a.created_at DESC LIMIT ? OFFSET ?`, qArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *a)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateApp(ctx context.Context, a *App) error {
	a.UpdatedAt = time.Now().UTC()
	keysJSON, _ := json.Marshal(a.EnvKeys)
	res, err := s.db.ExecContext(ctx,
		`UPDATE apps SET domain_id=?, runtime_version=?, command=?, port=?, env_keys=?, env_enc=?, status=?, updated_at=? WHERE id=?`,
		a.DomainID, a.RuntimeVersion, a.Command, a.Port, string(keysJSON), a.EnvEnc, a.Status, formatTime(a.UpdatedAt), a.ID,
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

func (s *Store) DeleteApp(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM apps WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
