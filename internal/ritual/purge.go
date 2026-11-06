package ritual

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PurgeConversationTx atomically increments content_epoch, invalidates any active run leases,
// deletes canonical messages, unsent drafts, and SSE event logs, and nulls out turn context
// snapshots and run output texts while preserving non-text comparison/usage metadata
// (Spec Section 25.1-25.2, DATA01-DATA04).
func PurgeConversationTx(ctx context.Context, tx pgx.Tx, ownerID, convID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		UPDATE conversations
		SET content_epoch = content_epoch + 1,
		    purged_at = COALESCE(purged_at, now()),
		    updated_at = now()
		WHERE id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE ai_runs
		SET output_text = NULL,
		    lease_token = NULL,
		    lease_expires_at = NULL,
		    status = CASE
		        WHEN status IN ('queued', 'running', 'streaming') THEN 'cancelled'
		        ELSE status
		    END,
		    finished_at = CASE
		        WHEN status IN ('queued', 'running', 'streaming') THEN COALESCE(finished_at, now())
		        ELSE finished_at
		    END,
		    purged_at = now(),
		    updated_at = now()
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE conversation_turns
		SET context_snapshot = NULL,
		    resolution = COALESCE(resolution, 'cancelled_on_closure'),
		    resolved_at = COALESCE(resolved_at, now()),
		    purged_at = now(),
		    version = version + 1,
		    updated_at = now()
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM conversation_messages
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM conversation_drafts
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM ai_run_events
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID); err != nil {
		return err
	}

	return nil
}
