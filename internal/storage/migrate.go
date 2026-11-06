package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationAdvisoryLockKey int64 = 8421094512

func MigrateUp(ctx context.Context, pool *pgxpool.Pool, migrationsDir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return err
	}
	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	for _, fname := range upFiles {
		version := strings.TrimSuffix(fname, ".up.sql")
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, fname))
		if err != nil {
			return err
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", fname, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}

// EnsureBootstrap creates or returns the single bootstrap owner, user_settings,
// and default garden idempotently under concurrency (AUTH03).
func EnsureBootstrap(ctx context.Context, pool *pgxpool.Pool, timezone string, defaultCols, defaultRows int16, defaultReleaseMode, defaultGrowMode string) (uuid.UUID, error) {
	return WithTx(ctx, pool, func(tx pgx.Tx) (uuid.UUID, error) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationAdvisoryLockKey+1); err != nil {
			return uuid.Nil, err
		}

		var userID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM app_users WHERE account_kind = 'bootstrap' LIMIT 1`).Scan(&userID)
		if err != nil {
			if err != pgx.ErrNoRows {
				return uuid.Nil, err
			}
			userID = uuid.New()
			if _, err := tx.Exec(ctx, `
				INSERT INTO app_users (id, account_kind, timezone)
				VALUES ($1, 'bootstrap', $2)
			`, userID, timezone); err != nil {
				return uuid.Nil, err
			}
		}

		aliasesJSON, _ := json.Marshal(map[string]string{
			"openai": "openai_chat",
			"gemini": "gemini_chat",
		})
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_settings (owner_id, release_mode, grow_mode, model_aliases)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (owner_id) DO NOTHING
		`, userID, defaultReleaseMode, defaultGrowMode, aliasesJSON); err != nil {
			return uuid.Nil, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO gardens (id, owner_id, cols, rows)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (owner_id) DO NOTHING
		`, uuid.New(), userID, defaultCols, defaultRows); err != nil {
			return uuid.Nil, err
		}

		return userID, nil
	})
}

// VerifySchemaAndOrphans validates required indexes and checks logical relational
// integrity since physical FKs are intentionally omitted per Spec Section 19.1.
func VerifySchemaAndOrphans(ctx context.Context, pool *pgxpool.Pool) error {
	requiredIndexes := []string{
		"uq_open_conversation",
		"uq_unresolved_turn",
		"uq_client_turn",
		"uq_message_seq",
		"uq_opening",
		"uq_user_message",
		"uq_selected_answer",
		"uq_current_run",
		"uq_run_attempt",
		"uq_flower_source",
		"uq_garden_owner",
		"uq_active_ritual",
		"uq_active_action",
		"uq_turn_event",
	}
	for _, idx := range requiredIndexes {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE indexname = $1)`, idx).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("missing required index: %s", idx)
		}
	}

	orphanChecks := []struct {
		name  string
		query string
	}{
		{
			name:  "turns_without_conversation",
			query: `SELECT COUNT(*) FROM conversation_turns t LEFT JOIN conversations c ON c.id = t.conversation_id AND c.owner_id = t.owner_id WHERE c.id IS NULL`,
		},
		{
			name:  "messages_without_conversation",
			query: `SELECT COUNT(*) FROM conversation_messages m LEFT JOIN conversations c ON c.id = m.conversation_id AND c.owner_id = m.owner_id WHERE c.id IS NULL`,
		},
		{
			name:  "runs_without_turn",
			query: `SELECT COUNT(*) FROM ai_runs r LEFT JOIN conversation_turns t ON t.id = r.turn_id AND t.owner_id = r.owner_id WHERE t.id IS NULL`,
		},
		{
			name:  "occupancies_without_garden_or_flower",
			query: `SELECT COUNT(*) FROM garden_occupancies o LEFT JOIN gardens g ON g.id = o.garden_id AND g.owner_id = o.owner_id LEFT JOIN flower_gifts f ON f.id = o.flower_id AND f.owner_id = o.owner_id WHERE g.id IS NULL OR f.id IS NULL`,
		},
		{
			name:  "purged_conversations_with_remaining_raw_messages",
			query: `SELECT COUNT(*) FROM conversations c JOIN conversation_messages m ON m.conversation_id = c.id WHERE c.purged_at IS NOT NULL`,
		},
	}

	for _, chk := range orphanChecks {
		var cnt int64
		if err := pool.QueryRow(ctx, chk.query).Scan(&cnt); err != nil {
			return fmt.Errorf("orphan check %s failed: %w", chk.name, err)
		}
		if cnt > 0 {
			return fmt.Errorf("integrity violation %s: %d rows", chk.name, cnt)
		}
	}
	return nil
}
