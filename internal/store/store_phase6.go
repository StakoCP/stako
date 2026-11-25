// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) migratePhase6() error {
	schema := `
	CREATE TABLE IF NOT EXISTS git_deploys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain_id TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
		repo_url TEXT NOT NULL,
		branch TEXT NOT NULL DEFAULT 'main',
		build_cmd TEXT NOT NULL DEFAULT '',
		webhook_secret_enc TEXT NOT NULL DEFAULT '',
		last_deployed_at TEXT,
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS server_nodes (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		host TEXT NOT NULL,
		port INTEGER NOT NULL DEFAULT 8084,
		role TEXT NOT NULL DEFAULT 'worker',
		token_hash TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'online',
		created_at TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *Store) CreateGitDeploy(ctx context.Context, g *GitDeploy, secretEnc string) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO git_deploys (id, user_id, domain_id, repo_url, branch, build_cmd, webhook_secret_enc, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		g.ID, g.UserID, g.DomainID, g.RepoURL, g.Branch, g.BuildCmd, secretEnc, formatTime(g.CreatedAt),
	)
	return err
}

func (s *Store) GetGitDeploy(ctx context.Context, id string) (*GitDeploy, error) {
	var g GitDeploy
	var lastDep sql.NullString
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, domain_id, repo_url, branch, build_cmd, last_deployed_at, created_at FROM git_deploys WHERE id=?`, id,
	).Scan(&g.ID, &g.UserID, &g.DomainID, &g.RepoURL, &g.Branch, &g.BuildCmd, &lastDep, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	g.LastDeployedAt = parseNullTime(lastDep)
	g.CreatedAt = parseTime(createdAt)
	return &g, nil
}

func (s *Store) ListGitDeploys(ctx context.Context, userID, resellerID *string) ([]GitDeploy, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "gd.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(gd.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM git_deploys gd JOIN users u ON gd.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT gd.id, gd.user_id, gd.domain_id, gd.repo_url, gd.branch, gd.build_cmd, gd.last_deployed_at, gd.created_at `+join+` ORDER BY gd.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GitDeploy{}
	for rows.Next() {
		var g GitDeploy
		var lastDep sql.NullString
		var createdAt string
		if err := rows.Scan(&g.ID, &g.UserID, &g.DomainID, &g.RepoURL, &g.Branch, &g.BuildCmd, &lastDep, &createdAt); err != nil {
			return nil, err
		}
		g.LastDeployedAt = parseNullTime(lastDep)
		g.CreatedAt = parseTime(createdAt)
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) TouchGitDeploy(ctx context.Context, id string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE git_deploys SET last_deployed_at=? WHERE id=?`, formatTime(time.Now().UTC()), id)
}

func (s *Store) DeleteGitDeploy(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM git_deploys WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateServerNode(ctx context.Context, n *ServerNode, tokenHash string) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO server_nodes (id, name, host, port, role, token_hash, status, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		n.ID, n.Name, n.Host, n.Port, n.Role, tokenHash, n.Status, formatTime(n.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) ListServerNodes(ctx context.Context) ([]ServerNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, host, port, role, status, created_at FROM server_nodes ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerNode{}
	for rows.Next() {
		var n ServerNode
		var createdAt string
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.Role, &n.Status, &createdAt); err != nil {
			return nil, err
		}
		n.CreatedAt = parseTime(createdAt)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) DeleteServerNode(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM server_nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
