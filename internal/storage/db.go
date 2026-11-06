package storage

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PGUniqueViolation      = "23505"
	PGCheckViolation       = "23514"
	PGSerializationFailure = "40001"
	PGDeadlockDetected     = "40P01"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 25
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	return pgxpool.NewWithConfig(ctx, cfg)
}

// WithTx executes fn inside a Read Committed database transaction with bounded
// retry (up to 3 attempts with jitter) on deadlock or serialization failure.
// External AI API calls MUST NEVER be placed inside fn (Spec Section 21.7).
func WithTx[T any](ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) (T, error)) (T, error) {
	var zero T
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			return zero, err
		}

		res, fnErr := fn(tx)
		if fnErr != nil {
			_ = tx.Rollback(ctx)
			if attempt < maxAttempts && isRetryablePGError(fnErr) {
				backoff := time.Duration(15*attempt+rand.IntN(25)) * time.Millisecond
				select {
				case <-ctx.Done():
					return zero, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			return zero, fnErr
		}

		if commitErr := tx.Commit(ctx); commitErr != nil {
			if attempt < maxAttempts && isRetryablePGError(commitErr) {
				backoff := time.Duration(15*attempt+rand.IntN(25)) * time.Millisecond
				select {
				case <-ctx.Done():
					return zero, ctx.Err()
				case <-time.After(backoff):
					continue
				}
			}
			return zero, commitErr
		}

		return res, nil
	}

	return zero, errors.New("transaction retry limit exceeded")
}

func isRetryablePGError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == PGSerializationFailure || pgErr.Code == PGDeadlockDetected
	}
	return false
}

func IsUniqueConstraint(err error, constraintName string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == PGUniqueViolation {
		if constraintName == "" {
			return true
		}
		return pgErr.ConstraintName == constraintName
	}
	return false
}
