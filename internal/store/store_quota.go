// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type UserDiskQuota struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	ProjectID   int    `json:"project_id"`
	DiskLimitMB int    `json:"disk_limit_mb"`
	InodeLimit  int    `json:"inode_limit"`
	DiskUsedMB  int    `json:"disk_used_mb"`
	InodeUsed   int    `json:"inode_used"`
	UpdatedAt   string `json:"updated_at"`
}

func (s *Store) migrateUserQuotas() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS user_disk_quotas (
		user_id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		project_id INTEGER NOT NULL,
		disk_limit_mb INTEGER NOT NULL DEFAULT 0,
		inode_limit INTEGER NOT NULL DEFAULT 0,
		disk_used_mb INTEGER NOT NULL DEFAULT 0,
		inode_used INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);
	`)
	return err
}

func (s *Store) GetUserDiskQuota(ctx context.Context, userID string) (*UserDiskQuota, error) {
	var q UserDiskQuota
	err := s.db.QueryRowContext(ctx, `
		SELECT user_id, username, project_id, disk_limit_mb, inode_limit, disk_used_mb, inode_used, updated_at
		FROM user_disk_quotas WHERE user_id=?`, userID,
	).Scan(&q.UserID, &q.Username, &q.ProjectID, &q.DiskLimitMB, &q.InodeLimit, &q.DiskUsedMB, &q.InodeUsed, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &q, nil
}

func (s *Store) UpsertUserDiskQuota(ctx context.Context, q *UserDiskQuota) error {
	q.UpdatedAt = formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_disk_quotas (user_id, username, project_id, disk_limit_mb, inode_limit, disk_used_mb, inode_used, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
			username=excluded.username,
			project_id=excluded.project_id,
			disk_limit_mb=excluded.disk_limit_mb,
			inode_limit=excluded.inode_limit,
			disk_used_mb=excluded.disk_used_mb,
			inode_used=excluded.inode_used,
			updated_at=excluded.updated_at`,
		q.UserID, q.Username, q.ProjectID, q.DiskLimitMB, q.InodeLimit, q.DiskUsedMB, q.InodeUsed, q.UpdatedAt,
	)
	return err
}

func (s *Store) ListUserDiskQuotas(ctx context.Context) ([]UserDiskQuota, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, username, project_id, disk_limit_mb, inode_limit, disk_used_mb, inode_used, updated_at
		FROM user_disk_quotas ORDER BY username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UserDiskQuota{}
	for rows.Next() {
		var q UserDiskQuota
		if err := rows.Scan(&q.UserID, &q.Username, &q.ProjectID, &q.DiskLimitMB, &q.InodeLimit, &q.DiskUsedMB, &q.InodeUsed, &q.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, q)
	}
	return items, rows.Err()
}
