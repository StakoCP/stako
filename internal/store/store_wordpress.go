// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type WPSite struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	DomainID       string `json:"domain_id"`
	Domain         string `json:"domain"`
	Path           string `json:"path"`
	WPVersion      string `json:"wp_version"`
	PHPVersion     string `json:"php_version"`
	AdminURL       string `json:"admin_url"`
	IsStaging      bool   `json:"is_staging"`
	ParentID       string `json:"parent_id"`
	UpdatesCore    int    `json:"updates_core"`
	UpdatesPlugins int    `json:"updates_plugins"`
	UpdatesThemes  int    `json:"updates_themes"`
	SecurityScore  int    `json:"security_score"`
	SecurityIssues string `json:"security_issues"`
	LastScannedAt  string `json:"last_scanned_at"`
	CreatedAt      string `json:"created_at"`
}

func (s *Store) migrateWordPress() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS wp_sites (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		domain_id TEXT NOT NULL,
		domain TEXT NOT NULL,
		path TEXT NOT NULL UNIQUE,
		wp_version TEXT NOT NULL DEFAULT '',
		php_version TEXT NOT NULL DEFAULT '8.3',
		admin_url TEXT NOT NULL DEFAULT '',
		is_staging INTEGER NOT NULL DEFAULT 0,
		parent_id TEXT NOT NULL DEFAULT '',
		updates_core INTEGER NOT NULL DEFAULT 0,
		updates_plugins INTEGER NOT NULL DEFAULT 0,
		updates_themes INTEGER NOT NULL DEFAULT 0,
		security_score INTEGER NOT NULL DEFAULT 100,
		security_issues TEXT NOT NULL DEFAULT '[]',
		last_scanned_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_wp_sites_user ON wp_sites(user_id);
	`)
	return err
}

func (s *Store) UpsertWPSite(ctx context.Context, w *WPSite) error {
	if w.CreatedAt == "" {
		w.CreatedAt = formatTime(time.Now().UTC())
	}
	if w.LastScannedAt == "" {
		w.LastScannedAt = w.CreatedAt
	}
	if w.SecurityIssues == "" {
		w.SecurityIssues = "[]"
	}
	stg := 0
	if w.IsStaging {
		stg = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO wp_sites (
			id, user_id, domain_id, domain, path, wp_version, php_version, admin_url,
			is_staging, parent_id, updates_core, updates_plugins, updates_themes,
			security_score, security_issues, last_scanned_at, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET
			wp_version=excluded.wp_version,
			php_version=excluded.php_version,
			admin_url=excluded.admin_url,
			updates_core=excluded.updates_core,
			updates_plugins=excluded.updates_plugins,
			updates_themes=excluded.updates_themes,
			security_score=excluded.security_score,
			security_issues=excluded.security_issues,
			last_scanned_at=excluded.last_scanned_at`,
		w.ID, w.UserID, w.DomainID, w.Domain, w.Path, w.WPVersion, w.PHPVersion, w.AdminURL,
		stg, w.ParentID, w.UpdatesCore, w.UpdatesPlugins, w.UpdatesThemes,
		w.SecurityScore, w.SecurityIssues, w.LastScannedAt, w.CreatedAt,
	)
	return err
}

func (s *Store) GetWPSite(ctx context.Context, id string) (*WPSite, error) {
	var w WPSite
	var stg int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, domain_id, domain, path, wp_version, php_version, admin_url,
			is_staging, parent_id, updates_core, updates_plugins, updates_themes,
			security_score, security_issues, last_scanned_at, created_at
		FROM wp_sites WHERE id=?`, id,
	).Scan(
		&w.ID, &w.UserID, &w.DomainID, &w.Domain, &w.Path, &w.WPVersion, &w.PHPVersion, &w.AdminURL,
		&stg, &w.ParentID, &w.UpdatesCore, &w.UpdatesPlugins, &w.UpdatesThemes,
		&w.SecurityScore, &w.SecurityIssues, &w.LastScannedAt, &w.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.IsStaging = stg == 1
	return &w, nil
}

func (s *Store) ListWPSites(ctx context.Context, userID, resellerID *string) ([]WPSite, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "wp.user_id=?")
		args = append(args, *userID)
	}
	if resellerID != nil {
		where = append(where, "u.reseller_id=?")
		args = append(args, *resellerID)
	}
	q := `SELECT wp.id, wp.user_id, wp.domain_id, wp.domain, wp.path, wp.wp_version, wp.php_version, wp.admin_url,
		wp.is_staging, wp.parent_id, wp.updates_core, wp.updates_plugins, wp.updates_themes,
		wp.security_score, wp.security_issues, wp.last_scanned_at, wp.created_at
		FROM wp_sites wp JOIN users u ON wp.user_id = u.id
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY wp.created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WPSite{}
	for rows.Next() {
		var w WPSite
		var stg int
		if err := rows.Scan(
			&w.ID, &w.UserID, &w.DomainID, &w.Domain, &w.Path, &w.WPVersion, &w.PHPVersion, &w.AdminURL,
			&stg, &w.ParentID, &w.UpdatesCore, &w.UpdatesPlugins, &w.UpdatesThemes,
			&w.SecurityScore, &w.SecurityIssues, &w.LastScannedAt, &w.CreatedAt,
		); err != nil {
			return nil, err
		}
		w.IsStaging = stg == 1
		items = append(items, w)
	}
	return items, rows.Err()
}

func (s *Store) UpdateWPSiteUpdates(ctx context.Context, id string, core, plugins, themes int) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE wp_sites SET updates_core=?, updates_plugins=?, updates_themes=?, last_scanned_at=? WHERE id=?`,
		core, plugins, themes, formatTime(time.Now().UTC()), id,
	)
}

func (s *Store) UpdateWPSiteSecurity(ctx context.Context, id string, score int, issuesJSON string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE wp_sites SET security_score=?, security_issues=?, last_scanned_at=? WHERE id=?`,
		score, issuesJSON, formatTime(time.Now().UTC()), id,
	)
}

func (s *Store) DeleteWPSite(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM wp_sites WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
