package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// PgxDriver wraps a pgxpool.Pool to satisfy our internal DBQuerier interface.
type PgxDriver struct {
	pool *pgxpool.Pool
}

func NewPostgresDriver(url string, maxConns int) (*PgxDriver, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	config.MaxConns = int32(maxConns) //nolint:gosec // MaxConns from int is fine here

	// Register pgvector type after connection is established
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, err
	}

	// Health check
	if err := pool.Ping(context.Background()); err != nil {
		return nil, err
	}

	return &PgxDriver{pool: pool}, nil
}

func (d *PgxDriver) QueryRow(ctx context.Context, sql string, args ...any) DBRow {
	return d.pool.QueryRow(ctx, sql, args...)
}

func (d *PgxDriver) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := d.pool.Exec(ctx, sql, args...)
	return translatePgError(err)
}

func (d *PgxDriver) Query(ctx context.Context, sql string, args ...any) (DBRows, error) {
	rows, err := d.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (d *PgxDriver) BeginTx(ctx context.Context) (DBTx, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgxTxAdapter{tx: tx}, nil
}

// Transaction adapter
type pgxTxAdapter struct {
	tx pgx.Tx
}

func (a *pgxTxAdapter) QueryRow(ctx context.Context, sql string, args ...any) DBRow {
	return a.tx.QueryRow(ctx, sql, args...)
}
func (a *pgxTxAdapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := a.tx.Exec(ctx, sql, args...)
	return translatePgError(err)
}
func (a *pgxTxAdapter) Query(ctx context.Context, sql string, args ...any) (DBRows, error) {
	rows, err := a.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}
func (a *pgxTxAdapter) BeginTx(ctx context.Context) (DBTx, error) {
	return nil, nil // Nested transactions not used in current storage spec
}
func (a *pgxTxAdapter) Commit(ctx context.Context) error {
	return a.tx.Commit(ctx)
}
func (a *pgxTxAdapter) Rollback(ctx context.Context) error {
	return a.tx.Rollback(ctx)
}

func translatePgError(err error) error {
	if err != nil && err.Error() == "no rows in result set" {
		return errNoRows
	}
	return err
}
