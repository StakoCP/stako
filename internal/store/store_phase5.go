// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) migratePhase5() error {
	schema := `
	CREATE TABLE IF NOT EXISTS dns_zones (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain TEXT NOT NULL UNIQUE,
		provider TEXT NOT NULL DEFAULT 'powerdns',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS dns_records (
		id TEXT PRIMARY KEY,
		zone_id TEXT NOT NULL REFERENCES dns_zones(id) ON DELETE CASCADE,
		type TEXT NOT NULL,
		name TEXT NOT NULL,
		content TEXT NOT NULL,
		ttl INTEGER NOT NULL DEFAULT 3600,
		priority INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS cron_jobs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		schedule TEXT NOT NULL,
		command TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS alert_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		channel TEXT NOT NULL,
		target TEXT NOT NULL,
		threshold TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

// --- DNS ---

func (s *Store) CreateDNSZone(ctx context.Context, z *DNSZone) error {
	if z.CreatedAt.IsZero() {
		z.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dns_zones (id, user_id, domain, provider, created_at) VALUES (?,?,?,?,?)`,
		z.ID, z.UserID, z.Domain, z.Provider, formatTime(z.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetDNSZone(ctx context.Context, id string) (*DNSZone, error) {
	var z DNSZone
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, domain, provider, created_at FROM dns_zones WHERE id=?`, id).
		Scan(&z.ID, &z.UserID, &z.Domain, &z.Provider, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	z.CreatedAt = parseTime(createdAt)
	return &z, nil
}

func (s *Store) ListDNSZones(ctx context.Context, userID, resellerID *string) ([]DNSZone, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "dz.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(dz.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM dns_zones dz JOIN users u ON dz.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT dz.id, dz.user_id, dz.domain, dz.provider, dz.created_at `+join+` ORDER BY dz.domain ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DNSZone{}
	for rows.Next() {
		var z DNSZone
		var createdAt string
		if err := rows.Scan(&z.ID, &z.UserID, &z.Domain, &z.Provider, &createdAt); err != nil {
			return nil, err
		}
		z.CreatedAt = parseTime(createdAt)
		out = append(out, z)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDNSZone(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dns_zones WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateDNSRecord(ctx context.Context, r *DNSRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO dns_records (id, zone_id, type, name, content, ttl, priority) VALUES (?,?,?,?,?,?,?)`,
		r.ID, r.ZoneID, r.Type, r.Name, r.Content, r.TTL, r.Priority,
	)
	return err
}

func (s *Store) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, zone_id, type, name, content, ttl, priority FROM dns_records WHERE zone_id=? ORDER BY type ASC, name ASC`, zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DNSRecord{}
	for rows.Next() {
		var r DNSRecord
		if err := rows.Scan(&r.ID, &r.ZoneID, &r.Type, &r.Name, &r.Content, &r.TTL, &r.Priority); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteDNSRecord(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dns_records WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Cron ---

func (s *Store) CreateCronJob(ctx context.Context, c *CronJob) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, user_id, schedule, command, created_at) VALUES (?,?,?,?,?)`,
		c.ID, c.UserID, c.Schedule, c.Command, formatTime(c.CreatedAt),
	)
	return err
}

func (s *Store) GetCronJob(ctx context.Context, id string) (*CronJob, error) {
	var c CronJob
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, schedule, command, created_at FROM cron_jobs WHERE id=?`, id).
		Scan(&c.ID, &c.UserID, &c.Schedule, &c.Command, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.CreatedAt = parseTime(createdAt)
	return &c, nil
}

func (s *Store) ListCronJobs(ctx context.Context, userID, resellerID *string) ([]CronJob, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "cj.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(cj.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM cron_jobs cj JOIN users u ON cj.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT cj.id, cj.user_id, cj.schedule, cj.command, cj.created_at `+join+` ORDER BY cj.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		var c CronJob
		var createdAt string
		if err := rows.Scan(&c.ID, &c.UserID, &c.Schedule, &c.Command, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt = parseTime(createdAt)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteCronJob(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Alerts ---

func (s *Store) CreateAlertRule(ctx context.Context, a *AlertRule) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO alert_rules (id, name, channel, target, threshold, created_at) VALUES (?,?,?,?,?,?)`,
		a.ID, a.Name, a.Channel, a.Target, a.Threshold, formatTime(a.CreatedAt),
	)
	return err
}

func (s *Store) ListAlertRules(ctx context.Context) ([]AlertRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, channel, target, threshold, created_at FROM alert_rules ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var a AlertRule
		var createdAt string
		if err := rows.Scan(&a.ID, &a.Name, &a.Channel, &a.Target, &a.Threshold, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt = parseTime(createdAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAlertRule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM alert_rules WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
