// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/StakoCP/stako/internal/catalog"
)

type PluginHook struct {
	Event  string `json:"event"`
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"`
}

type PluginPage struct {
	Slug  string   `json:"slug"`
	Title string   `json:"title"`
	URL   string   `json:"url"`
	Roles []string `json:"roles,omitempty"`
}

type Plugin struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Version     string                `json:"version"`
	Author      string                `json:"author"`
	Description string                `json:"description"`
	Homepage    string                `json:"homepage"`
	Enabled     bool                  `json:"enabled"`
	Hooks       []PluginHook          `json:"hooks"`
	Pages       []PluginPage          `json:"pages"`
	Apps        []catalog.AppManifest `json:"apps"`
	CreatedAt   string                `json:"created_at"`
	UpdatedAt   string                `json:"updated_at"`
}

func (s *Store) migratePlugins() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS plugins (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		version TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		homepage TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		manifest_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	`)
	return err
}

func (s *Store) UpsertPlugin(ctx context.Context, p *Plugin) error {
	now := formatTime(time.Now().UTC())
	if p.CreatedAt == "" {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.Hooks == nil {
		p.Hooks = []PluginHook{}
	}
	if p.Pages == nil {
		p.Pages = []PluginPage{}
	}
	if p.Apps == nil {
		p.Apps = []catalog.AppManifest{}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	en := 0
	if p.Enabled {
		en = 1
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO plugins (id, name, version, author, description, homepage, enabled, manifest_json, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			version=excluded.version,
			author=excluded.author,
			description=excluded.description,
			homepage=excluded.homepage,
			enabled=excluded.enabled,
			manifest_json=excluded.manifest_json,
			updated_at=excluded.updated_at`,
		p.ID, p.Name, p.Version, p.Author, p.Description, p.Homepage, en, string(b), p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (s *Store) GetPlugin(ctx context.Context, id string) (*Plugin, error) {
	var raw string
	var en int
	err := s.db.QueryRowContext(ctx, `SELECT manifest_json, enabled FROM plugins WHERE id=?`, id).Scan(&raw, &en)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var p Plugin
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	p.Enabled = en == 1
	return &p, nil
}

func (s *Store) ListPlugins(ctx context.Context) ([]Plugin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT manifest_json, enabled FROM plugins ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Plugin{}
	for rows.Next() {
		var raw string
		var en int
		if err := rows.Scan(&raw, &en); err != nil {
			return nil, err
		}
		var p Plugin
		if json.Unmarshal([]byte(raw), &p) == nil {
			p.Enabled = en == 1
			items = append(items, p)
		}
	}
	return items, rows.Err()
}

func (s *Store) SetPluginEnabled(ctx context.Context, id string, enabled bool) error {
	en := 0
	if enabled {
		en = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE plugins SET enabled=?, updated_at=? WHERE id=?`, en, formatTime(time.Now().UTC()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePlugin(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM plugins WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
