// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"time"
)

type ServiceConfigRevision struct {
	ID        string    `json:"id"`
	Service   string    `json:"service"`
	Content   string    `json:"content"`
	Comment   string    `json:"comment"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) migrateSysConfig() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS service_config_revisions (
		id TEXT PRIMARY KEY,
		service TEXT NOT NULL,
		content TEXT NOT NULL,
		comment TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`)
	return err
}

func (s *Store) SaveServiceConfigRevision(ctx context.Context, rev *ServiceConfigRevision) error {
	if rev.CreatedAt.IsZero() {
		rev.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO service_config_revisions (id, service, content, comment, created_by, created_at) VALUES (?,?,?,?,?,?)`,
		rev.ID, rev.Service, rev.Content, rev.Comment, rev.CreatedBy, formatTime(rev.CreatedAt),
	)
	return err
}

func (s *Store) ListServiceConfigRevisions(ctx context.Context, service string) ([]ServiceConfigRevision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, service, content, comment, created_by, created_at FROM service_config_revisions WHERE service=? ORDER BY created_at DESC LIMIT 20`,
		service,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServiceConfigRevision{}
	for rows.Next() {
		var r ServiceConfigRevision
		var createdAt string
		if err := rows.Scan(&r.ID, &r.Service, &r.Content, &r.Comment, &r.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}
