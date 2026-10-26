package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 211420260901

var migrationNamePattern = regexp.MustCompile(`^(\d{4,18})_[a-z0-9_]+\.sql$`)

type migration struct {
	Version  int64
	Name     string
	SQL      string
	Checksum string
}

func Migrate(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	migrations, err := loadMigrations(directory)
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) }()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			name text NOT NULL UNIQUE,
			checksum char(64) NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, item := range migrations {
		var checksum string
		err := conn.QueryRow(ctx,
			`SELECT checksum FROM schema_migrations WHERE version = $1`, item.Version,
		).Scan(&checksum)
		switch {
		case err == nil:
			if strings.TrimSpace(checksum) != item.Checksum {
				return fmt.Errorf("migration %s was changed after being applied", item.Name)
			}
			continue
		case err != pgx.ErrNoRows:
			return fmt.Errorf("read migration %s state: %w", item.Name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", item.Name, err)
		}
		if _, err = tx.Exec(ctx, item.SQL); err == nil {
			_, err = tx.Exec(ctx,
				`INSERT INTO schema_migrations(version,name,checksum) VALUES($1,$2,$3)`,
				item.Version, item.Name, item.Checksum,
			)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", item.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.Name, err)
		}
	}
	return nil
}

func loadMigrations(directory string) ([]migration, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}
	items := make([]migration, 0, len(entries))
	seen := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() || !migrationNamePattern.MatchString(entry.Name()) {
			continue
		}
		match := migrationNamePattern.FindStringSubmatch(entry.Name())
		version, _ := strconv.ParseInt(match[1], 10, 64)
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %04d: %s and %s", version, previous, entry.Name())
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(content)
		items = append(items, migration{
			Version:  version,
			Name:     entry.Name(),
			SQL:      string(content),
			Checksum: hex.EncodeToString(sum[:]),
		})
		seen[version] = entry.Name()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Version < items[j].Version })
	if len(items) == 0 {
		return nil, fmt.Errorf("no migrations found in %s", directory)
	}
	return items, nil
}
