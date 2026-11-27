// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type GitPreview struct {
	ID              string `json:"id"`
	GitDeployID     string `json:"git_deploy_id"`
	UserID          string `json:"user_id"`
	DomainID        string `json:"domain_id"`
	Subdomain       string `json:"subdomain"`
	Branch          string `json:"branch"`
	PRNumber        int    `json:"pr_number"`
	Image           string `json:"image"`
	ContainerName   string `json:"container_name"`
	HostPort        int    `json:"host_port"`
	ContainerPort   int    `json:"container_port"`
	PreviewDomainID string `json:"preview_domain_id"`
	Status          string `json:"status"`
	LastDeployedAt  string `json:"last_deployed_at"`
	CreatedAt       string `json:"created_at"`
}

func (s *Store) migrateGitPreviews() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS git_previews (
		id TEXT PRIMARY KEY,
		git_deploy_id TEXT NOT NULL,
		user_id TEXT NOT NULL,
		domain_id TEXT NOT NULL,
		subdomain TEXT NOT NULL UNIQUE,
		branch TEXT NOT NULL,
		pr_number INTEGER NOT NULL DEFAULT 0,
		image TEXT NOT NULL,
		container_name TEXT NOT NULL,
		host_port INTEGER NOT NULL,
		container_port INTEGER NOT NULL DEFAULT 3000,
		preview_domain_id TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'deploying',
		last_deployed_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_git_previews_deploy ON git_previews(git_deploy_id);
	CREATE INDEX IF NOT EXISTS idx_git_previews_user ON git_previews(user_id);
	`)
	return err
}

func (s *Store) AllocatePreviewPort(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT host_port FROM git_previews`)
	if err != nil {
		return 18001, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var p int
		if rows.Scan(&p) == nil {
			used[p] = true
		}
	}
	for p := 18001; p <= 19999; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, errors.New("no preview ports available")
}

func (s *Store) CreateGitPreview(ctx context.Context, p *GitPreview) error {
	if p.CreatedAt == "" {
		p.CreatedAt = formatTime(time.Now().UTC())
	}
	if p.Status == "" {
		p.Status = "deploying"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO git_previews (
			id, git_deploy_id, user_id, domain_id, subdomain, branch, pr_number,
			image, container_name, host_port, container_port, preview_domain_id,
			status, last_deployed_at, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.GitDeployID, p.UserID, p.DomainID, p.Subdomain, p.Branch, p.PRNumber,
		p.Image, p.ContainerName, p.HostPort, p.ContainerPort, p.PreviewDomainID,
		p.Status, p.LastDeployedAt, p.CreatedAt,
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetGitPreview(ctx context.Context, id string) (*GitPreview, error) {
	var p GitPreview
	err := s.db.QueryRowContext(ctx, `
		SELECT id, git_deploy_id, user_id, domain_id, subdomain, branch, pr_number,
			image, container_name, host_port, container_port, preview_domain_id,
			status, last_deployed_at, created_at
		FROM git_previews WHERE id=?`, id,
	).Scan(
		&p.ID, &p.GitDeployID, &p.UserID, &p.DomainID, &p.Subdomain, &p.Branch, &p.PRNumber,
		&p.Image, &p.ContainerName, &p.HostPort, &p.ContainerPort, &p.PreviewDomainID,
		&p.Status, &p.LastDeployedAt, &p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListGitPreviews(ctx context.Context, userID, resellerID *string, gitDeployID string) ([]GitPreview, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "gp.user_id=?")
		args = append(args, *userID)
	}
	if resellerID != nil {
		where = append(where, "u.reseller_id=?")
		args = append(args, *resellerID)
	}
	if gitDeployID != "" {
		where = append(where, "gp.git_deploy_id=?")
		args = append(args, gitDeployID)
	}
	q := `SELECT gp.id, gp.git_deploy_id, gp.user_id, gp.domain_id, gp.subdomain, gp.branch, gp.pr_number,
		gp.image, gp.container_name, gp.host_port, gp.container_port, gp.preview_domain_id,
		gp.status, gp.last_deployed_at, gp.created_at
		FROM git_previews gp JOIN users u ON gp.user_id = u.id
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY gp.created_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []GitPreview{}
	for rows.Next() {
		var p GitPreview
		if err := rows.Scan(
			&p.ID, &p.GitDeployID, &p.UserID, &p.DomainID, &p.Subdomain, &p.Branch, &p.PRNumber,
			&p.Image, &p.ContainerName, &p.HostPort, &p.ContainerPort, &p.PreviewDomainID,
			&p.Status, &p.LastDeployedAt, &p.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (s *Store) TouchGitPreview(ctx context.Context, id, status string) {
	_, _ = s.db.ExecContext(ctx,
		`UPDATE git_previews SET status=?, last_deployed_at=? WHERE id=?`,
		status, formatTime(time.Now().UTC()), id,
	)
}

func (s *Store) DeleteGitPreview(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_previews WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
