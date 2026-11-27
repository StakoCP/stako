// SPDX-License-Identifier: MIT
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DomainCacheConfig struct {
	DomainID          string `json:"domain_id"`
	UserID            string `json:"user_id"`
	MicrocacheEnabled bool   `json:"microcache_enabled"`
	MicrocacheTTLSec  int    `json:"microcache_ttl_sec"`
	RedisObjectCache  bool   `json:"redis_object_cache"`
	RedisInstanceID   string `json:"redis_instance_id"`
	RedisPort         int    `json:"redis_port"`
	CFZoneID          string `json:"cf_zone_id"`
	CFAPITokenEnc     string `json:"-"`
	HasCFToken        bool   `json:"has_cf_token"`
	CFUnderAttack     bool   `json:"cf_under_attack"`
	CFDevMode         bool   `json:"cf_dev_mode"`
	UpdatedAt         string `json:"updated_at"`
}

func (s *Store) migrateDomainCache() error {
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS domain_cache_configs (
		domain_id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		microcache_enabled INTEGER NOT NULL DEFAULT 0,
		microcache_ttl_sec INTEGER NOT NULL DEFAULT 10,
		redis_object_cache INTEGER NOT NULL DEFAULT 0,
		redis_instance_id TEXT NOT NULL DEFAULT '',
		redis_port INTEGER NOT NULL DEFAULT 0,
		cf_zone_id TEXT NOT NULL DEFAULT '',
		cf_api_token_enc TEXT NOT NULL DEFAULT '',
		cf_under_attack INTEGER NOT NULL DEFAULT 0,
		cf_dev_mode INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL
	);
	`)
	return err
}

func (s *Store) GetDomainCacheConfig(ctx context.Context, domainID string) (*DomainCacheConfig, error) {
	var c DomainCacheConfig
	var mc, roc, ua, dm int
	err := s.db.QueryRowContext(ctx, `
		SELECT domain_id, user_id, microcache_enabled, microcache_ttl_sec,
			redis_object_cache, redis_instance_id, redis_port,
			cf_zone_id, cf_api_token_enc, cf_under_attack, cf_dev_mode, updated_at
		FROM domain_cache_configs WHERE domain_id=?`, domainID,
	).Scan(
		&c.DomainID, &c.UserID, &mc, &c.MicrocacheTTLSec,
		&roc, &c.RedisInstanceID, &c.RedisPort,
		&c.CFZoneID, &c.CFAPITokenEnc, &ua, &dm, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &DomainCacheConfig{
				DomainID:         domainID,
				MicrocacheTTLSec: 10,
			}, nil
		}
		return nil, err
	}
	c.MicrocacheEnabled = mc == 1
	c.RedisObjectCache = roc == 1
	c.HasCFToken = c.CFAPITokenEnc != ""
	c.CFUnderAttack = ua == 1
	c.CFDevMode = dm == 1
	return &c, nil
}

func (s *Store) UpsertDomainCacheConfig(ctx context.Context, c *DomainCacheConfig) error {
	c.UpdatedAt = formatTime(time.Now().UTC())
	if c.MicrocacheTTLSec <= 0 {
		c.MicrocacheTTLSec = 10
	}
	b2i := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO domain_cache_configs (
			domain_id, user_id, microcache_enabled, microcache_ttl_sec,
			redis_object_cache, redis_instance_id, redis_port,
			cf_zone_id, cf_api_token_enc, cf_under_attack, cf_dev_mode, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(domain_id) DO UPDATE SET
			microcache_enabled=excluded.microcache_enabled,
			microcache_ttl_sec=excluded.microcache_ttl_sec,
			redis_object_cache=excluded.redis_object_cache,
			redis_instance_id=excluded.redis_instance_id,
			redis_port=excluded.redis_port,
			cf_zone_id=excluded.cf_zone_id,
			cf_api_token_enc=excluded.cf_api_token_enc,
			cf_under_attack=excluded.cf_under_attack,
			cf_dev_mode=excluded.cf_dev_mode,
			updated_at=excluded.updated_at`,
		c.DomainID, c.UserID, b2i(c.MicrocacheEnabled), c.MicrocacheTTLSec,
		b2i(c.RedisObjectCache), c.RedisInstanceID, c.RedisPort,
		c.CFZoneID, c.CFAPITokenEnc, b2i(c.CFUnderAttack), b2i(c.CFDevMode), c.UpdatedAt,
	)
	return err
}
