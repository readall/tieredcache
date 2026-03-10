package multitiercache

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTier implements Tier for relational archive (pgx batch).
type PostgresTier struct {
	pool  *pgxpool.Pool
	table string
}

func NewPostgresTier(connString, table string) (Tier, error) {
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, err
	}
	return &PostgresTier{pool: pool, table: table}, nil
}

func (p *PostgresTier) Name() string { return "postgres" }

func (p *PostgresTier) PutBatch(ctx context.Context, items []TierItem) error {
	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(fmt.Sprintf("INSERT INTO %s (key, value, last_access, version) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING", p.table),
			item.Key, item.Value, time.Unix(int64(item.Meta.LastAccessUnix), 0), item.Meta.Version)
	}
	br := p.pool.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < len(items); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// Get retrieves a value from Postgres by key (promotion support).
func (p *PostgresTier) Get(ctx context.Context, key []byte) ([]byte, error) {
	var value []byte
	err := p.pool.QueryRow(ctx, fmt.Sprintf("SELECT value FROM %s WHERE key = $1", p.table), key).Scan(&value)
	if err != nil {
		// Return nil, nil for not found (promotion will skip this tier)
		return nil, nil
	}
	return value, nil
}

// Delete removes a key from Postgres.
func (p *PostgresTier) Delete(ctx context.Context, key []byte) error {
	_, err := p.pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE key = $1", p.table), key)
	if err != nil {
		return fmt.Errorf("postgres delete failed: %w", err)
	}
	return nil
}
