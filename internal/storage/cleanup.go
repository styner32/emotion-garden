package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Cleaner struct {
	pool           *pgxpool.Pool
	usageRetention time.Duration
	logger         *slog.Logger
}

func NewCleaner(pool *pgxpool.Pool, usageRetention time.Duration, logger *slog.Logger) *Cleaner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Cleaner{
		pool:           pool,
		usageRetention: usageRetention,
		logger:         logger,
	}
}

func (c *Cleaner) RunOnce(ctx context.Context) {
	// 1. Expired idempotency records (7d default)
	_, err := c.pool.Exec(ctx, `DELETE FROM idempotency_records WHERE expires_at < now()`)
	if err != nil {
		c.logger.Warn("idempotency cleanup failed", "err_code", "CLEANUP_IDEMPOTENCY")
	}

	// 2. Resolved turn SSE events older than 24h on active conversations (Spec Section 25.2)
	_, err = c.pool.Exec(ctx, `
		DELETE FROM ai_run_events e
		USING conversation_turns t
		WHERE e.turn_id = t.id
		  AND t.resolved_at IS NOT NULL
		  AND e.created_at < now() - interval '24 hours'
	`)
	if err != nil {
		c.logger.Warn("turn event cleanup failed", "err_code", "CLEANUP_EVENTS")
	}

	// 3. Closed conversation non-text usage metadata older than retention (90d default, Spec Section 19.5)
	cutoff := time.Now().UTC().Add(-c.usageRetention)
	_, err = c.pool.Exec(ctx, `
		DELETE FROM ai_runs r
		USING conversations cv
		WHERE r.conversation_id = cv.id
		  AND cv.closed_at IS NOT NULL
		  AND r.created_at < $1
	`, cutoff)
	if err != nil {
		c.logger.Warn("ai_runs retention cleanup failed", "err_code", "CLEANUP_USAGE")
	}
}

func (c *Cleaner) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.RunOnce(ctx)
			}
		}
	}()
}
