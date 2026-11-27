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

func (s *Store) migratePhase3() error {
	schema := `
	CREATE TABLE IF NOT EXISTS containers (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain_id TEXT REFERENCES domains(id) ON DELETE SET NULL,
		name TEXT NOT NULL,
		image TEXT NOT NULL,
		ports TEXT NOT NULL DEFAULT '[]',
		volumes TEXT NOT NULL DEFAULT '[]',
		env_enc TEXT NOT NULL DEFAULT '',
		cpu_limit TEXT NOT NULL DEFAULT '1.0',
		memory_mb INTEGER NOT NULL DEFAULT 512,
		status TEXT NOT NULL DEFAULT 'running',
		created_at TEXT NOT NULL,
		UNIQUE(user_id, name)
	);
	CREATE TABLE IF NOT EXISTS compose_stacks (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		compose_yaml TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		created_at TEXT NOT NULL,
		UNIQUE(user_id, name)
	);
	CREATE TABLE IF NOT EXISTS app_installs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		domain_id TEXT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
		app_slug TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'installed',
		created_at TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS redis_instances (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		port INTEGER NOT NULL UNIQUE,
		memory_mb INTEGER NOT NULL DEFAULT 256,
		persistent INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'running',
		created_at TEXT NOT NULL,
		UNIQUE(user_id, name)
	);
	CREATE TABLE IF NOT EXISTS rabbit_vhosts (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		vhost TEXT NOT NULL UNIQUE,
		username TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	for _, stmt := range []string{
		`ALTER TABLE containers ADD COLUMN repo_url TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE containers ADD COLUMN repo_branch TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE containers ADD COLUMN dockerfile TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = s.db.Exec(stmt)
	}
	return nil
}

// --- Containers ---

func (s *Store) CreateContainer(ctx context.Context, c *Container) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	pJSON, _ := json.Marshal(c.Ports)
	vJSON, _ := json.Marshal(c.Volumes)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO containers (id, user_id, domain_id, name, image, repo_url, repo_branch, dockerfile, ports, volumes, env_enc, cpu_limit, memory_mb, status, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.DomainID, c.Name, c.Image, c.RepoURL, c.RepoBranch, c.Dockerfile, string(pJSON), string(vJSON), c.EnvEnc, c.CPULimit, c.MemoryMB, c.Status, formatTime(c.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetContainer(ctx context.Context, id string) (*Container, error) {
	var c Container
	var domainID sql.NullString
	var pJSON, vJSON, createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, domain_id, name, image, repo_url, repo_branch, dockerfile, ports, volumes, env_enc, cpu_limit, memory_mb, status, created_at FROM containers WHERE id=?`, id,
	).Scan(&c.ID, &c.UserID, &domainID, &c.Name, &c.Image, &c.RepoURL, &c.RepoBranch, &c.Dockerfile, &pJSON, &vJSON, &c.EnvEnc, &c.CPULimit, &c.MemoryMB, &c.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if domainID.Valid {
		c.DomainID = &domainID.String
	}
	_ = json.Unmarshal([]byte(pJSON), &c.Ports)
	_ = json.Unmarshal([]byte(vJSON), &c.Volumes)
	c.CreatedAt = parseTime(createdAt)
	return &c, nil
}

func (s *Store) ListContainers(ctx context.Context, userID, resellerID *string, limit, offset int) ([]Container, int, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "c.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(c.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM containers c JOIN users u ON c.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) `+join, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.user_id, c.domain_id, c.name, c.image, c.repo_url, c.repo_branch, c.dockerfile, c.ports, c.volumes, c.cpu_limit, c.memory_mb, c.status, c.created_at `+join+` ORDER BY c.created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Container{}
	for rows.Next() {
		var c Container
		var domainID sql.NullString
		var pJSON, vJSON, createdAt string
		if err := rows.Scan(&c.ID, &c.UserID, &domainID, &c.Name, &c.Image, &c.RepoURL, &c.RepoBranch, &c.Dockerfile, &pJSON, &vJSON, &c.CPULimit, &c.MemoryMB, &c.Status, &createdAt); err != nil {
			return nil, 0, err
		}
		if domainID.Valid {
			c.DomainID = &domainID.String
		}
		_ = json.Unmarshal([]byte(pJSON), &c.Ports)
		_ = json.Unmarshal([]byte(vJSON), &c.Volumes)
		c.CreatedAt = parseTime(createdAt)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (s *Store) UpdateContainerStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE containers SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) DeleteContainer(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM containers WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Compose Stacks ---

func (s *Store) CreateComposeStack(ctx context.Context, st *ComposeStack) error {
	if st.CreatedAt.IsZero() {
		st.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO compose_stacks (id, user_id, name, compose_yaml, status, created_at) VALUES (?,?,?,?,?,?)`,
		st.ID, st.UserID, st.Name, st.ComposeYAML, st.Status, formatTime(st.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetComposeStack(ctx context.Context, id string) (*ComposeStack, error) {
	var st ComposeStack
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, compose_yaml, status, created_at FROM compose_stacks WHERE id=?`, id,
	).Scan(&st.ID, &st.UserID, &st.Name, &st.ComposeYAML, &st.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	st.CreatedAt = parseTime(createdAt)
	return &st, nil
}

func (s *Store) ListComposeStacks(ctx context.Context, userID, resellerID *string) ([]ComposeStack, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "cs.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(cs.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM compose_stacks cs JOIN users u ON cs.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT cs.id, cs.user_id, cs.name, cs.compose_yaml, cs.status, cs.created_at `+join+` ORDER BY cs.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ComposeStack{}
	for rows.Next() {
		var st ComposeStack
		var createdAt string
		if err := rows.Scan(&st.ID, &st.UserID, &st.Name, &st.ComposeYAML, &st.Status, &createdAt); err != nil {
			return nil, err
		}
		st.CreatedAt = parseTime(createdAt)
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) DeleteComposeStack(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM compose_stacks WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- App Installs ---

func (s *Store) CreateAppInstall(ctx context.Context, ai *AppInstall) error {
	if ai.CreatedAt.IsZero() {
		ai.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO app_installs (id, user_id, domain_id, app_slug, status, created_at) VALUES (?,?,?,?,?,?)`,
		ai.ID, ai.UserID, ai.DomainID, ai.AppSlug, ai.Status, formatTime(ai.CreatedAt),
	)
	return err
}

func (s *Store) ListAppInstalls(ctx context.Context, userID, resellerID *string) ([]AppInstall, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "ai.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(ai.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM app_installs ai JOIN users u ON ai.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT ai.id, ai.user_id, ai.domain_id, ai.app_slug, ai.status, ai.created_at `+join+` ORDER BY ai.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppInstall{}
	for rows.Next() {
		var ai AppInstall
		var createdAt string
		if err := rows.Scan(&ai.ID, &ai.UserID, &ai.DomainID, &ai.AppSlug, &ai.Status, &createdAt); err != nil {
			return nil, err
		}
		ai.CreatedAt = parseTime(createdAt)
		out = append(out, ai)
	}
	return out, rows.Err()
}

func (s *Store) GetAppInstall(ctx context.Context, id string) (*AppInstall, error) {
	var ai AppInstall
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, domain_id, app_slug, status, created_at FROM app_installs WHERE id=?`, id,
	).Scan(&ai.ID, &ai.UserID, &ai.DomainID, &ai.AppSlug, &ai.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	ai.CreatedAt = parseTime(createdAt)
	return &ai, nil
}

func (s *Store) UpdateAppInstall(ctx context.Context, id, domainID, status string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE app_installs SET domain_id=?, status=? WHERE id=?`, domainID, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAppInstall(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM app_installs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Redis & RabbitMQ ---

func (s *Store) NextFreeRedisPort(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT port FROM redis_instances`)
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
	for port := 6380; port < 6500; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, errors.New("no free redis port")
}

func (s *Store) CreateRedis(ctx context.Context, r *RedisInstance) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO redis_instances (id, user_id, name, port, memory_mb, persistent, status, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		r.ID, r.UserID, r.Name, r.Port, r.MemoryMB, boolInt(r.Persistent), r.Status, formatTime(r.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetRedis(ctx context.Context, id string) (*RedisInstance, error) {
	var r RedisInstance
	var persInt int
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, port, memory_mb, persistent, status, created_at FROM redis_instances WHERE id=?`, id,
	).Scan(&r.ID, &r.UserID, &r.Name, &r.Port, &r.MemoryMB, &persInt, &r.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	r.Persistent = persInt != 0
	r.CreatedAt = parseTime(createdAt)
	return &r, nil
}

func (s *Store) ListRedis(ctx context.Context, userID, resellerID *string) ([]RedisInstance, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "r.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(r.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM redis_instances r JOIN users u ON r.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.user_id, r.name, r.port, r.memory_mb, r.persistent, r.status, r.created_at `+join+` ORDER BY r.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RedisInstance{}
	for rows.Next() {
		var r RedisInstance
		var persInt int
		var createdAt string
		if err := rows.Scan(&r.ID, &r.UserID, &r.Name, &r.Port, &r.MemoryMB, &persInt, &r.Status, &createdAt); err != nil {
			return nil, err
		}
		r.Persistent = persInt != 0
		r.CreatedAt = parseTime(createdAt)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRedis(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM redis_instances WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreateRabbitVhost(ctx context.Context, v *RabbitVhost) error {
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO rabbit_vhosts (id, user_id, vhost, username, status, created_at) VALUES (?,?,?,?,?,?)`,
		v.ID, v.UserID, v.Vhost, v.Username, v.Status, formatTime(v.CreatedAt),
	)
	if isUniqueErr(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) GetRabbitVhost(ctx context.Context, id string) (*RabbitVhost, error) {
	var v RabbitVhost
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, vhost, username, status, created_at FROM rabbit_vhosts WHERE id=?`, id,
	).Scan(&v.ID, &v.UserID, &v.Vhost, &v.Username, &v.Status, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	v.CreatedAt = parseTime(createdAt)
	return &v, nil
}

func (s *Store) ListRabbitVhosts(ctx context.Context, userID, resellerID *string) ([]RabbitVhost, error) {
	where := []string{"1=1"}
	var args []any
	if userID != nil {
		where = append(where, "rv.user_id = ?")
		args = append(args, *userID)
	} else if resellerID != nil {
		where = append(where, "(rv.user_id = ? OR u.owner_id = ?)")
		args = append(args, *resellerID, *resellerID)
	}
	join := `FROM rabbit_vhosts rv JOIN users u ON rv.user_id = u.id WHERE ` + strings.Join(where, " AND ")
	rows, err := s.db.QueryContext(ctx, `SELECT rv.id, rv.user_id, rv.vhost, rv.username, rv.status, rv.created_at `+join+` ORDER BY rv.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RabbitVhost{}
	for rows.Next() {
		var v RabbitVhost
		var createdAt string
		if err := rows.Scan(&v.ID, &v.UserID, &v.Vhost, &v.Username, &v.Status, &createdAt); err != nil {
			return nil, err
		}
		v.CreatedAt = parseTime(createdAt)
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRabbitVhost(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM rabbit_vhosts WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
