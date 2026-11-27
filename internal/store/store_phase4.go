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

func (s *Store) migratePhase4() error {
	schema := `
	CREATE TABLE IF NOT EXISTS mail_domains (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain TEXT NOT NULL UNIQUE,
		dkim_selector TEXT NOT NULL DEFAULT 'default',
		dkim_public_key TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS mailboxes (
		id TEXT PRIMARY KEY,
		mail_domain_id TEXT NOT NULL REFERENCES mail_domains(id) ON DELETE CASCADE,
		email TEXT NOT NULL UNIQUE,
		quota_mb INTEGER NOT NULL DEFAULT 1024,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS backups (
		id TEXT PRIMARY KEY,
		user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
		kind TEXT NOT NULL,
		target TEXT NOT NULL,
		size_bytes INTEGER NOT NULL DEFAULT 0,
		checksum TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'completed',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS webhooks (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		url TEXT NOT NULL,
		events TEXT NOT NULL DEFAULT '[]',
		secret_enc TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS system_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE mail_domains ADD COLUMN relay_provider TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_domains ADD COLUMN relay_host TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_domains ADD COLUMN relay_port INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE mail_domains ADD COLUMN relay_user TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE mail_domains ADD COLUMN relay_pass_enc TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = s.db.Exec(stmt)
	}
	return nil
}

// --- Settings / Lockdown ---

func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM system_settings WHERE key=?`, key).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return val, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) DeleteNonAdminSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE role != 'admin')`)
	return err
}

// --- Mail ---

func (s *Store) CreateMailDomain(ctx context.Context, m *MailDomain) error {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mail_domains (id, user_id, domain, dkim_selector, dkim_public_key, created_at) VALUES (?,?,?,?,?,?)`,
		m.ID, m.UserID, m.Domain, m.DKIMSelector, m.DKIMPublicKey, formatTime(m.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetMailDomain(ctx context.Context, id string) (*MailDomain, error) {
	var m MailDomain
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, domain, dkim_selector, dkim_public_key, relay_provider, relay_host, relay_port, relay_user, relay_pass_enc, created_at FROM mail_domains WHERE id=?`, id,
	).Scan(&m.ID, &m.UserID, &m.Domain, &m.DKIMSelector, &m.DKIMPublicKey, &m.RelayProvider, &m.RelayHost, &m.RelayPort, &m.RelayUser, &m.RelayPassEnc, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.CreatedAt = parseTime(createdAt)
	return &m, nil
}

func (s *Store) ListMailDomains(ctx context.Context, userID, resellerID *string) ([]MailDomain, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "md.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(md.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM mail_domains md JOIN users u ON md.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT md.id, md.user_id, md.domain, md.dkim_selector, md.dkim_public_key, md.relay_provider, md.relay_host, md.relay_port, md.relay_user, md.relay_pass_enc, md.created_at `+join+` ORDER BY md.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MailDomain{}
	for rows.Next() {
		var m MailDomain
		var createdAt string
		if err := rows.Scan(&m.ID, &m.UserID, &m.Domain, &m.DKIMSelector, &m.DKIMPublicKey, &m.RelayProvider, &m.RelayHost, &m.RelayPort, &m.RelayUser, &m.RelayPassEnc, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMailDomainRelay(ctx context.Context, id, provider, host string, port int, user, passEnc string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mail_domains SET relay_provider=?, relay_host=?, relay_port=?, relay_user=?, relay_pass_enc=? WHERE id=?`,
		provider, host, port, user, passEnc, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteMailDomain(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mail_domains WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateMailbox(ctx context.Context, mb *Mailbox) error {
	if mb.CreatedAt.IsZero() {
		mb.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mailboxes (id, mail_domain_id, email, quota_mb, created_at) VALUES (?,?,?,?,?)`,
		mb.ID, mb.MailDomainID, mb.Email, mb.QuotaMB, formatTime(mb.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetMailbox(ctx context.Context, id string) (*Mailbox, error) {
	var mb Mailbox
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, mail_domain_id, email, quota_mb, created_at FROM mailboxes WHERE id=?`, id).
		Scan(&mb.ID, &mb.MailDomainID, &mb.Email, &mb.QuotaMB, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	mb.CreatedAt = parseTime(createdAt)
	return &mb, nil
}

func (s *Store) ListMailboxes(ctx context.Context, mailDomainID string) ([]Mailbox, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, mail_domain_id, email, quota_mb, created_at FROM mailboxes WHERE mail_domain_id=? ORDER BY email ASC`, mailDomainID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		var mb Mailbox
		var createdAt string
		if err := rows.Scan(&mb.ID, &mb.MailDomainID, &mb.Email, &mb.QuotaMB, &createdAt); err != nil {
			return nil, err
		}
		mb.CreatedAt = parseTime(createdAt)
		out = append(out, mb)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMailbox(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mailboxes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Backups ---

func (s *Store) CreateBackup(ctx context.Context, b *Backup) error {
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO backups (id, user_id, kind, target, size_bytes, checksum, status, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		b.ID, b.UserID, b.Kind, b.Target, b.SizeBytes, b.Checksum, b.Status, formatTime(b.CreatedAt),
	)
	return err
}

func (s *Store) GetBackup(ctx context.Context, id string) (*Backup, error) {
	var b Backup
	var userID sql.NullString
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, kind, target, size_bytes, checksum, status, created_at FROM backups WHERE id=?`, id,
	).Scan(&b.ID, &userID, &b.Kind, &b.Target, &b.SizeBytes, &b.Checksum, &b.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if userID.Valid {
		b.UserID = &userID.String
	}
	b.CreatedAt = parseTime(createdAt)
	return &b, nil
}

func (s *Store) ListBackups(ctx context.Context, userID *string) ([]Backup, error) {
	q := `SELECT id, user_id, kind, target, size_bytes, checksum, status, created_at FROM backups`
	var args []any
	if userID != nil {
		q += ` WHERE user_id = ?`
		args = append(args, *userID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Backup{}
	for rows.Next() {
		var b Backup
		var uid sql.NullString
		var createdAt string
		if err := rows.Scan(&b.ID, &uid, &b.Kind, &b.Target, &b.SizeBytes, &b.Checksum, &b.Status, &createdAt); err != nil {
			return nil, err
		}
		if uid.Valid {
			b.UserID = &uid.String
		}
		b.CreatedAt = parseTime(createdAt)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBackup(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM backups WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Webhooks ---

func (s *Store) CreateWebhook(ctx context.Context, w *Webhook) error {
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now().UTC()
	}
	evJSON, _ := json.Marshal(w.Events)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhooks (id, user_id, url, events, secret_enc, created_at) VALUES (?,?,?,?,?,?)`,
		w.ID, w.UserID, w.URL, string(evJSON), w.SecretEnc, formatTime(w.CreatedAt),
	)
	return err
}

func (s *Store) ListWebhooks(ctx context.Context, userID *string) ([]Webhook, error) {
	q := `SELECT id, user_id, url, events, created_at FROM webhooks`
	var args []any
	if userID != nil {
		q += ` WHERE user_id = ?`
		args = append(args, *userID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var w Webhook
		var evJSON, createdAt string
		if err := rows.Scan(&w.ID, &w.UserID, &w.URL, &evJSON, &createdAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evJSON), &w.Events)
		w.CreatedAt = parseTime(createdAt)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) DeleteWebhook(ctx context.Context, id, userID string, isAdmin bool) error {
	q := `DELETE FROM webhooks WHERE id=?`
	args := []any{id}
	if !isAdmin {
		q += ` AND user_id=?`
		args = append(args, userID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
