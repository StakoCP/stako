// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type UptimeMonitor struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	Domain          string `json:"domain"`
	URL             string `json:"url"`
	IntervalSec     int    `json:"interval_sec"`
	ExpectedStatus  int    `json:"expected_status"`
	Status          string `json:"status"`
	LastHTTPStatus  int    `json:"last_http_status"`
	LastLatencyMs   int64  `json:"last_latency_ms"`
	LastError       string `json:"last_error"`
	SSLExpiresAt    string `json:"ssl_expires_at"`
	SSLDaysLeft     int    `json:"ssl_days_left"`
	SSLIssuer       string `json:"ssl_issuer"`
	DomainExpiresAt string `json:"domain_expires_at"`
	DomainDaysLeft  int    `json:"domain_days_left"`
	TotalChecks     int    `json:"total_checks"`
	UpChecks        int    `json:"up_checks"`
	PublicStatus    bool   `json:"public_status"`
	LastCheckedAt   string `json:"last_checked_at"`
	CreatedAt       string `json:"created_at"`
}

func (s *Store) migrateUptime() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS uptime_monitors (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		domain TEXT NOT NULL,
		url TEXT NOT NULL,
		interval_sec INTEGER NOT NULL DEFAULT 300,
		expected_status INTEGER NOT NULL DEFAULT 200,
		status TEXT NOT NULL DEFAULT 'unknown',
		last_http_status INTEGER NOT NULL DEFAULT 0,
		last_latency_ms INTEGER NOT NULL DEFAULT 0,
		last_error TEXT NOT NULL DEFAULT '',
		ssl_expires_at TEXT NOT NULL DEFAULT '',
		ssl_days_left INTEGER NOT NULL DEFAULT -1,
		ssl_issuer TEXT NOT NULL DEFAULT '',
		domain_expires_at TEXT NOT NULL DEFAULT '',
		domain_days_left INTEGER NOT NULL DEFAULT -1,
		total_checks INTEGER NOT NULL DEFAULT 0,
		up_checks INTEGER NOT NULL DEFAULT 0,
		public_status INTEGER NOT NULL DEFAULT 1,
		last_checked_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_uptime_user ON uptime_monitors(user_id);
	`)
	return err
}

func (s *Store) CreateUptimeMonitor(ctx context.Context, m *UptimeMonitor) error {
	if m.CreatedAt == "" {
		m.CreatedAt = formatTime(time.Now().UTC())
	}
	if m.Status == "" {
		m.Status = "unknown"
	}
	if m.IntervalSec <= 0 {
		m.IntervalSec = 300
	}
	if m.ExpectedStatus <= 0 {
		m.ExpectedStatus = 200
	}
	pub := 0
	if m.PublicStatus {
		pub = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO uptime_monitors (
			id, user_id, domain, url, interval_sec, expected_status, status,
			last_http_status, last_latency_ms, last_error,
			ssl_expires_at, ssl_days_left, ssl_issuer,
			domain_expires_at, domain_days_left,
			total_checks, up_checks, public_status, last_checked_at, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, m.UserID, m.Domain, m.URL, m.IntervalSec, m.ExpectedStatus, m.Status,
		m.LastHTTPStatus, m.LastLatencyMs, m.LastError,
		m.SSLExpiresAt, m.SSLDaysLeft, m.SSLIssuer,
		m.DomainExpiresAt, m.DomainDaysLeft,
		m.TotalChecks, m.UpChecks, pub, m.LastCheckedAt, m.CreatedAt,
	)
	return err
}

func (s *Store) GetUptimeMonitor(ctx context.Context, id string) (*UptimeMonitor, error) {
	var m UptimeMonitor
	var pub int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, domain, url, interval_sec, expected_status, status,
			last_http_status, last_latency_ms, last_error,
			ssl_expires_at, ssl_days_left, ssl_issuer,
			domain_expires_at, domain_days_left,
			total_checks, up_checks, public_status, last_checked_at, created_at
		FROM uptime_monitors WHERE id=?`, id,
	).Scan(
		&m.ID, &m.UserID, &m.Domain, &m.URL, &m.IntervalSec, &m.ExpectedStatus, &m.Status,
		&m.LastHTTPStatus, &m.LastLatencyMs, &m.LastError,
		&m.SSLExpiresAt, &m.SSLDaysLeft, &m.SSLIssuer,
		&m.DomainExpiresAt, &m.DomainDaysLeft,
		&m.TotalChecks, &m.UpChecks, &pub, &m.LastCheckedAt, &m.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.PublicStatus = pub == 1
	return &m, nil
}

func (s *Store) ListUptimeMonitors(ctx context.Context, userID *string, publicOnly bool) ([]UptimeMonitor, error) {
	query := `
		SELECT id, user_id, domain, url, interval_sec, expected_status, status,
			last_http_status, last_latency_ms, last_error,
			ssl_expires_at, ssl_days_left, ssl_issuer,
			domain_expires_at, domain_days_left,
			total_checks, up_checks, public_status, last_checked_at, created_at
		FROM uptime_monitors WHERE 1=1`
	var args []any
	if userID != nil {
		query += " AND user_id=?"
		args = append(args, *userID)
	}
	if publicOnly {
		query += " AND public_status=1"
	}
	query += " ORDER BY created_at DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []UptimeMonitor
	for rows.Next() {
		var m UptimeMonitor
		var pub int
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Domain, &m.URL, &m.IntervalSec, &m.ExpectedStatus, &m.Status,
			&m.LastHTTPStatus, &m.LastLatencyMs, &m.LastError,
			&m.SSLExpiresAt, &m.SSLDaysLeft, &m.SSLIssuer,
			&m.DomainExpiresAt, &m.DomainDaysLeft,
			&m.TotalChecks, &m.UpChecks, &pub, &m.LastCheckedAt, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		m.PublicStatus = pub == 1
		items = append(items, m)
	}
	if items == nil {
		items = []UptimeMonitor{}
	}
	return items, rows.Err()
}

func (s *Store) UpdateUptimeMonitorResult(ctx context.Context, m *UptimeMonitor) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE uptime_monitors SET
			status=?, last_http_status=?, last_latency_ms=?, last_error=?,
			ssl_expires_at=?, ssl_days_left=?, ssl_issuer=?,
			domain_expires_at=?, domain_days_left=?,
			total_checks=?, up_checks=?, last_checked_at=?
		WHERE id=?`,
		m.Status, m.LastHTTPStatus, m.LastLatencyMs, m.LastError,
		m.SSLExpiresAt, m.SSLDaysLeft, m.SSLIssuer,
		m.DomainExpiresAt, m.DomainDaysLeft,
		m.TotalChecks, m.UpChecks, m.LastCheckedAt, m.ID,
	)
	return err
}

func (s *Store) DeleteUptimeMonitor(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM uptime_monitors WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
