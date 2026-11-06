package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"emotion-garden/internal/config"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultLeaseDuration     = 30 * time.Second
	defaultHeartbeatInterval = 5 * time.Second
	defaultPollInterval      = 500 * time.Millisecond
	deltaBatchWindow         = 250 * time.Millisecond
	deltaBatchRunes          = 512
)

type WorkerConfig struct {
	Concurrency      int
	RunTimeout       time.Duration
	FirstTextTimeout time.Duration
	IdleTimeout      time.Duration
	LeaseDuration    time.Duration
	HeartbeatEvery   time.Duration
	PollInterval     time.Duration
}

type Worker struct {
	pool      *pgxpool.Pool
	models    *config.ModelRegistry
	prompts   *config.PromptBundle
	providers map[string]ChatProvider
	broker    *SSEBroker
	cfg       WorkerConfig
	triggerCh chan struct{}
	logger    *slog.Logger
}

func NewWorker(
	pool *pgxpool.Pool,
	models *config.ModelRegistry,
	prompts *config.PromptBundle,
	providers map[string]ChatProvider,
	broker *SSEBroker,
	cfg WorkerConfig,
	logger *slog.Logger,
) *Worker {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 60 * time.Second
	}
	if cfg.FirstTextTimeout <= 0 {
		cfg.FirstTextTimeout = 25 * time.Second
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 20 * time.Second
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = defaultLeaseDuration
	}
	if cfg.HeartbeatEvery <= 0 {
		cfg.HeartbeatEvery = defaultHeartbeatInterval
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		pool:      pool,
		models:    models,
		prompts:   prompts,
		providers: providers,
		broker:    broker,
		cfg:       cfg,
		triggerCh: make(chan struct{}, 32),
		logger:    logger,
	}
}

func (w *Worker) Trigger() {
	select {
	case w.triggerCh <- struct{}{}:
	default:
	}
}

func (w *Worker) Start(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup

	// Recover expired leases on startup before claiming new runs (AI09)
	if n, err := w.ReapExpiredLeases(ctx); err != nil {
		w.logger.Warn("startup lease reaper error", "err_code", "LEASE_REAP_FAILED")
	} else if n > 0 {
		w.logger.Info("recovered expired runs as interrupted on startup", "count", n)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(w.cfg.HeartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = w.ReapExpiredLeases(ctx)
			}
		}
	}()

	for i := 0; i < w.cfg.Concurrency; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			w.runLoop(ctx, workerIdx)
		}(i)
	}

	return &wg
}

func (w *Worker) runLoop(ctx context.Context, _ int) {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		claimed, err := w.claimNextRun(ctx)
		if err != nil {
			w.logger.Warn("worker claim error", "err_code", "CLAIM_ERROR")
		}
		if claimed != nil {
			w.executeClaimedRun(ctx, *claimed)
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-w.triggerCh:
		case <-ticker.C:
		}
	}
}

type claimedRun struct {
	RunID            uuid.UUID
	OwnerID          uuid.UUID
	ConversationID   uuid.UUID
	TurnID           uuid.UUID
	ConversationKind string
	TurnMode         string
	Slot             string
	Provider         string
	ModelAlias       string
	RequestedModel   string
	SettingsJSON     []byte
	ContextJSON      []byte
	ContextHash      string
	CapturedEpoch    int64
	LeaseToken       string
}

func (w *Worker) claimNextRun(ctx context.Context) (*claimedRun, error) {
	return storage.WithTx(ctx, w.pool, func(tx pgx.Tx) (*claimedRun, error) {
		var runID, ownerID, convID, turnID uuid.UUID
		var slot, provider, modelAlias, reqModel, ctxHash string
		var settingsRaw []byte
		var capturedEpoch int64

		err := tx.QueryRow(ctx, `
			SELECT id, owner_id, conversation_id, turn_id, slot, provider, model_alias,
			       requested_model, settings_snapshot, context_hash, captured_epoch
			FROM ai_runs
			WHERE status = 'queued' AND superseded_at IS NULL
			ORDER BY queued_at ASC, id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		`).Scan(
			&runID, &ownerID, &convID, &turnID, &slot, &provider,
			&modelAlias, &reqModel, &settingsRaw, &ctxHash, &capturedEpoch,
		)
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}

		// Lock conversation -> turn -> run in canonical order to verify epoch and unresolved state
		var convKind string
		var convEpoch int64
		var convPurgedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT kind, content_epoch, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&convKind, &convEpoch, &convPurgedAt)
		if err != nil {
			return nil, err
		}

		var turnMode string
		var ctxRaw []byte
		var resolvedAt *time.Time
		var nextSeq int64
		err = tx.QueryRow(ctx, `
			SELECT mode, context_snapshot, resolved_at, next_event_seq
			FROM conversation_turns
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, turnID, ownerID).Scan(&turnMode, &ctxRaw, &resolvedAt, &nextSeq)
		if err != nil {
			return nil, err
		}

		if convPurgedAt != nil || convEpoch != capturedEpoch || resolvedAt != nil || len(ctxRaw) == 0 {
			_, _ = tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = 'cancelled', finished_at = now(), updated_at = now()
				WHERE id = $1
			`, runID)
			return nil, nil
		}

		leaseToken := uuid.NewString()
		leaseExp := time.Now().UTC().Add(w.cfg.LeaseDuration)
		if _, err := tx.Exec(ctx, `
			UPDATE ai_runs
			SET status = 'running',
			    started_at = now(),
			    lease_token = $2,
			    lease_expires_at = $3,
			    updated_at = now()
			WHERE id = $1
		`, runID, leaseToken, leaseExp); err != nil {
			return nil, err
		}

		evPayload, _ := json.Marshal(map[string]any{
			"run_id": runID.String(),
			"slot":   slot,
			"status": StatusRunning,
		})
		if err := appendTurnEventTx(ctx, tx, turnID, ownerID, convID, &runID, nextSeq, "run.started", evPayload); err != nil {
			return nil, err
		}

		return &claimedRun{
			RunID:            runID,
			OwnerID:          ownerID,
			ConversationID:   convID,
			TurnID:           turnID,
			ConversationKind: convKind,
			TurnMode:         turnMode,
			Slot:             slot,
			Provider:         provider,
			ModelAlias:       modelAlias,
			RequestedModel:   reqModel,
			SettingsJSON:     settingsRaw,
			ContextJSON:      ctxRaw,
			ContextHash:      ctxHash,
			CapturedEpoch:    capturedEpoch,
			LeaseToken:       leaseToken,
		}, nil
	})
}

func (w *Worker) executeClaimedRun(appCtx context.Context, cr claimedRun) {
	w.broker.Notify(cr.TurnID)

	providerAdapter, ok := w.providers[cr.Provider]
	if !ok {
		w.finalizeFailedRun(context.Background(), cr, StatusFailed, "MODEL_UNAVAILABLE", "", "", nil)
		return
	}

	var opts GenerationOptions
	_ = json.Unmarshal(cr.SettingsJSON, &opts)

	sysPrompt := w.prompts.GrowPrompt
	if cr.ConversationKind == "release" {
		sysPrompt = w.prompts.ReleasePrompt
	}

	// Hard total timeout (never extended by events — Spec Section 26.2)
	runCtx, cancelRun := context.WithTimeout(appCtx, w.cfg.RunTimeout)
	defer cancelRun()

	w.broker.RegisterRunCancel(cr.RunID, cancelRun)
	defer w.broker.UnregisterRunCancel(cr.RunID)

	var timeoutReasonMu sync.Mutex
	var customTimeoutCode string
	setTimeoutReason := func(code string) {
		timeoutReasonMu.Lock()
		if customTimeoutCode == "" {
			customTimeoutCode = code
		}
		timeoutReasonMu.Unlock()
	}

	// First-text & post-first-text idle watchdog (Spec Section 26.2)
	progressCh := make(chan string, 16)
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		firstTimer := time.NewTimer(w.cfg.FirstTextTimeout)
		defer firstTimer.Stop()

		sawFirstText := false
		var idleTimer *time.Timer
		var idleC <-chan time.Time

		for {
			select {
			case <-runCtx.Done():
				if idleTimer != nil {
					idleTimer.Stop()
				}
				return
			case kind := <-progressCh:
				if !sawFirstText && kind == EventTextDelta {
					sawFirstText = true
					if !firstTimer.Stop() {
						select {
						case <-firstTimer.C:
						default:
						}
					}
					idleTimer = time.NewTimer(w.cfg.IdleTimeout)
					idleC = idleTimer.C
				} else if sawFirstText && idleTimer != nil {
					if !idleTimer.Stop() {
						select {
						case <-idleTimer.C:
						default:
						}
					}
					idleTimer.Reset(w.cfg.IdleTimeout)
				}
			case <-firstTimer.C:
				if !sawFirstText {
					setTimeoutReason("FIRST_TEXT_TIMEOUT")
					cancelRun()
					return
				}
			case <-idleC:
				setTimeoutReason("IDLE_TIMEOUT")
				cancelRun()
				return
			}
		}
	}()

	// Lease renewal & DB cancellation/epoch fence checker (Spec Section 14.4)
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		ticker := time.NewTicker(w.cfg.HeartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				alive := w.renewLease(context.Background(), cr)
				if !alive {
					cancelRun()
					return
				}
			}
		}
	}()

	// Delta batching state: immediate flush on first public text, then 250ms / 512 runes
	var bufMu sync.Mutex
	var pendingBuf strings.Builder
	var fullStreamed strings.Builder
	firstFlushed := false

	flushDelta := func() error {
		bufMu.Lock()
		if pendingBuf.Len() == 0 {
			bufMu.Unlock()
			return nil
		}
		chunk := pendingBuf.String()
		pendingBuf.Reset()
		isFirst := !firstFlushed
		firstFlushed = true
		bufMu.Unlock()

		return w.persistDeltaChunk(context.Background(), cr, chunk, isFirst)
	}

	batchDone := make(chan struct{})
	go func() {
		defer close(batchDone)
		ticker := time.NewTicker(deltaBatchWindow)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := flushDelta(); err != nil {
					cancelRun()
					return
				}
			}
		}
	}()

	req := GenerationRequest{
		RunID:            cr.RunID,
		ConversationKind: cr.ConversationKind,
		Provider:         cr.Provider,
		ModelAlias:       cr.ModelAlias,
		ModelID:          cr.RequestedModel,
		SystemPrompt:     sysPrompt,
		ContextJSON:      cr.ContextJSON,
		ContextHash:      cr.ContextHash,
		Options:          opts,
	}

	res, genErr := providerAdapter.Generate(runCtx, req, func(ev ProviderEvent) error {
		select {
		case progressCh <- ev.Kind:
		default:
		}
		if ev.Kind != EventTextDelta || ev.TextDelta == "" {
			return nil
		}

		bufMu.Lock()
		fullStreamed.WriteString(ev.TextDelta)
		pendingBuf.WriteString(ev.TextDelta)
		shouldFlushNow := !firstFlushed || utf8.RuneCountInString(pendingBuf.String()) >= deltaBatchRunes
		bufMu.Unlock()

		if shouldFlushNow {
			if err := flushDelta(); err != nil {
				return err
			}
		}
		return nil
	})

	// Stop background goroutines for this run
	cancelRun()
	<-watchdogDone
	<-hbDone
	<-batchDone

	if appCtx.Err() != nil {
		// Server is shutting down: mark interrupted if still active, never mark partial text succeeded
		w.finalizeFailedRun(context.Background(), cr, StatusInterrupted, "SERVER_SHUTDOWN", res.RequestID, "interrupted", res.Usage)
		return
	}

	timeoutReasonMu.Lock()
	tCode := customTimeoutCode
	timeoutReasonMu.Unlock()

	if tCode != "" {
		w.finalizeFailedRun(context.Background(), cr, StatusFailed, tCode, res.RequestID, "timeout", res.Usage)
		return
	}

	if genErr != nil && (res.Status == StatusCancelled || errors.Is(genErr, context.Canceled)) {
		// Check if DB already cancelled/fenced; if timed out by 60s parent context, record timeout
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			w.finalizeFailedRun(context.Background(), cr, StatusFailed, "PROVIDER_TIMEOUT", res.RequestID, "timeout", res.Usage)
			return
		}
		w.finalizeCancelledIfStillOwned(context.Background(), cr, res.Usage)
		return
	}

	if res.Status == StatusSucceeded {
		_ = flushDelta()
		w.finalizeSucceededRun(context.Background(), cr, res)
		return
	}

	status := res.Status
	if status == "" {
		status = StatusFailed
	}
	errCode := res.ErrorCode
	if errCode == "" {
		errCode = "PROVIDER_FAILED"
	}
	w.finalizeFailedRun(context.Background(), cr, status, errCode, res.RequestID, res.FinishReason, res.Usage)
}

func (w *Worker) renewLease(ctx context.Context, cr claimedRun) bool {
	tag, err := w.pool.Exec(ctx, `
		UPDATE ai_runs r
		SET lease_expires_at = $3,
		    updated_at = now()
		FROM conversations c, conversation_turns t
		WHERE r.id = $1
		  AND r.lease_token = $2
		  AND r.status IN ('running', 'streaming')
		  AND r.superseded_at IS NULL
		  AND c.id = r.conversation_id
		  AND c.content_epoch = r.captured_epoch
		  AND c.purged_at IS NULL
		  AND t.id = r.turn_id
		  AND t.resolved_at IS NULL
	`, cr.RunID, cr.LeaseToken, time.Now().UTC().Add(w.cfg.LeaseDuration))
	if err != nil {
		return false
	}
	return tag.RowsAffected() == 1
}

var errRunFenced = errors.New("run fenced, cancelled, or purged")

func (w *Worker) persistDeltaChunk(ctx context.Context, cr claimedRun, delta string, isFirst bool) error {
	committed, err := storage.WithTx(ctx, w.pool, func(tx pgx.Tx) (bool, error) {
		var convEpoch int64
		var purgedAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT content_epoch, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.ConversationID, cr.OwnerID).Scan(&convEpoch, &purgedAt); err != nil {
			return false, err
		}

		var resolvedAt *time.Time
		var nextSeq int64
		if err := tx.QueryRow(ctx, `
			SELECT resolved_at, next_event_seq
			FROM conversation_turns
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.TurnID, cr.OwnerID).Scan(&resolvedAt, &nextSeq); err != nil {
			return false, err
		}

		var curStatus string
		var curLease *string
		var supersededAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT status, lease_token, superseded_at
			FROM ai_runs
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.RunID, cr.OwnerID).Scan(&curStatus, &curLease, &supersededAt); err != nil {
			return false, err
		}

		if purgedAt != nil || convEpoch != cr.CapturedEpoch || resolvedAt != nil ||
			supersededAt != nil || curLease == nil || *curLease != cr.LeaseToken ||
			(curStatus != StatusRunning && curStatus != StatusStreaming) {
			return false, nil
		}

		if isFirst {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = 'streaming',
				    first_text_at = COALESCE(first_text_at, now()),
				    output_text = COALESCE(output_text, '') || $2,
				    updated_at = now()
				WHERE id = $1
			`, cr.RunID, delta); err != nil {
				return false, err
			}
		} else {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET output_text = COALESCE(output_text, '') || $2,
				    updated_at = now()
				WHERE id = $1
			`, cr.RunID, delta); err != nil {
				return false, err
			}
		}

		evPayload, _ := json.Marshal(map[string]any{
			"run_id": cr.RunID.String(),
			"slot":   cr.Slot,
			"text":   delta,
		})
		if err := appendTurnEventTx(ctx, tx, cr.TurnID, cr.OwnerID, cr.ConversationID, &cr.RunID, nextSeq, "run.delta", evPayload); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if !committed {
		return errRunFenced
	}
	w.broker.Notify(cr.TurnID)
	return nil
}

func (w *Worker) finalizeSucceededRun(ctx context.Context, cr claimedRun, res GenerationResult) {
	modelEntry, _ := w.models.GetByAlias(cr.ModelAlias)
	cost, usageStatus := EstimateCost(w.models, modelEntry, res.Usage)
	var usageRaw []byte
	if res.Usage != nil {
		usageRaw, _ = json.Marshal(res.Usage)
	}
	var priceVer *string
	if cost != nil && modelEntry.PriceVersion != "" {
		pv := modelEntry.PriceVersion
		priceVer = &pv
	}

	notified, err := storage.WithTx(ctx, w.pool, func(tx pgx.Tx) (bool, error) {
		var convEpoch int64
		var purgedAt *time.Time
		var nextMsgSeq int
		if err := tx.QueryRow(ctx, `
			SELECT content_epoch, purged_at, next_message_seq
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.ConversationID, cr.OwnerID).Scan(&convEpoch, &purgedAt, &nextMsgSeq); err != nil {
			return false, err
		}

		var resolvedAt *time.Time
		var nextEventSeq int64
		var turnMode string
		if err := tx.QueryRow(ctx, `
			SELECT mode, resolved_at, next_event_seq
			FROM conversation_turns
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.TurnID, cr.OwnerID).Scan(&turnMode, &resolvedAt, &nextEventSeq); err != nil {
			return false, err
		}

		var curStatus string
		var curLease *string
		var supersededAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT status, lease_token, superseded_at
			FROM ai_runs
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.RunID, cr.OwnerID).Scan(&curStatus, &curLease, &supersededAt); err != nil {
			return false, err
		}

		// Fence check: never resurrect text on purged/cancelled/resolved turn (AI10, DATA03)
		if purgedAt != nil || convEpoch != cr.CapturedEpoch || resolvedAt != nil ||
			supersededAt != nil || curLease == nil || *curLease != cr.LeaseToken ||
			(curStatus != StatusRunning && curStatus != StatusStreaming) {
			return false, nil
		}

		if _, err := tx.Exec(ctx, `
			UPDATE ai_runs
			SET status = 'succeeded',
			    output_text = $2,
			    reported_model = NULLIF($3, ''),
			    provider_request_id = NULLIF($4, ''),
			    finish_reason = NULLIF($5, ''),
			    first_text_at = COALESCE(first_text_at, now()),
			    finished_at = now(),
			    lease_token = NULL,
			    lease_expires_at = NULL,
			    usage_json = $6,
			    usage_status = $7,
			    price_version = $8,
			    estimated_cost = $9,
			    updated_at = now()
			WHERE id = $1
		`, cr.RunID, res.OutputText, res.ReportedModel, res.RequestID, res.FinishReason,
			usageRaw, usageStatus, priceVer, cost); err != nil {
			return false, err
		}

		compPayload, _ := json.Marshal(map[string]any{
			"run_id": cr.RunID.String(),
			"slot":   cr.Slot,
			"status": StatusSucceeded,
		})
		if err := appendTurnEventTx(ctx, tx, cr.TurnID, cr.OwnerID, cr.ConversationID, &cr.RunID, nextEventSeq, "run.completed", compPayload); err != nil {
			return false, err
		}
		nextEventSeq++

		if turnMode != "compare" {
			// Single mode: promote directly to canonical assistant message & resolve turn atomically
			msgID := uuid.New()
			if _, err := tx.Exec(ctx, `
				INSERT INTO conversation_messages (
					id, owner_id, conversation_id, turn_id, seq, role, origin, content, selected_run_id
				) VALUES ($1, $2, $3, $4, $5, 'assistant', 'ai_selected', $6, $7)
			`, msgID, cr.OwnerID, cr.ConversationID, cr.TurnID, nextMsgSeq, res.OutputText, cr.RunID); err != nil {
				return false, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE conversation_turns
				SET selected_run_id = $2,
				    resolution = 'auto_accepted',
				    resolved_at = now(),
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
			`, cr.TurnID, cr.RunID); err != nil {
				return false, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE conversations
				SET next_message_seq = next_message_seq + 1,
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
			`, cr.ConversationID); err != nil {
				return false, err
			}

			resPayload, _ := json.Marshal(map[string]any{
				"turn_id":         cr.TurnID.String(),
				"resolution":      "auto_accepted",
				"selected_run_id": cr.RunID.String(),
			})
			if err := appendTurnEventTx(ctx, tx, cr.TurnID, cr.OwnerID, cr.ConversationID, &cr.RunID, nextEventSeq, "turn.resolved", resPayload); err != nil {
				return false, err
			}
		} else {
			selPayload, _ := json.Marshal(map[string]any{
				"turn_id": cr.TurnID.String(),
			})
			if err := appendTurnEventTx(ctx, tx, cr.TurnID, cr.OwnerID, cr.ConversationID, &cr.RunID, nextEventSeq, "turn.selection_required", selPayload); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	if err != nil {
		w.logger.Error("finalize succeeded run error", "run_id", cr.RunID.String(), "err_code", "FINALIZE_ERROR")
		return
	}
	if notified {
		w.broker.Notify(cr.TurnID)
	}
}

func (w *Worker) finalizeFailedRun(ctx context.Context, cr claimedRun, status, errCode, reqID, finishReason string, usage *Usage) {
	modelEntry, _ := w.models.GetByAlias(cr.ModelAlias)
	cost, usageStatus := EstimateCost(w.models, modelEntry, usage)
	var usageRaw []byte
	if usage != nil {
		usageRaw, _ = json.Marshal(usage)
	}
	var priceVer *string
	if cost != nil && modelEntry.PriceVersion != "" {
		pv := modelEntry.PriceVersion
		priceVer = &pv
	}

	notified, _ := storage.WithTx(ctx, w.pool, func(tx pgx.Tx) (bool, error) {
		var convEpoch int64
		var purgedAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT content_epoch, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.ConversationID, cr.OwnerID).Scan(&convEpoch, &purgedAt); err != nil {
			return false, err
		}

		var nextEventSeq int64
		var resolvedAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT resolved_at, next_event_seq
			FROM conversation_turns
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.TurnID, cr.OwnerID).Scan(&resolvedAt, &nextEventSeq); err != nil {
			return false, err
		}

		var curStatus string
		var curLease *string
		if err := tx.QueryRow(ctx, `
			SELECT status, lease_token
			FROM ai_runs
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, cr.RunID, cr.OwnerID).Scan(&curStatus, &curLease); err != nil {
			return false, err
		}

		if curLease == nil || *curLease != cr.LeaseToken || (curStatus != StatusRunning && curStatus != StatusStreaming) {
			return false, nil
		}

		// Clear partial output_text if conversation was already purged
		clearText := purgedAt != nil || convEpoch != cr.CapturedEpoch
		if clearText {
			_, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = $2,
				    output_text = NULL,
				    error_code = $3,
				    provider_request_id = NULLIF($4, ''),
				    finish_reason = NULLIF($5, ''),
				    finished_at = now(),
				    lease_token = NULL,
				    lease_expires_at = NULL,
				    usage_json = $6,
				    usage_status = $7,
				    price_version = $8,
				    estimated_cost = $9,
				    updated_at = now()
				WHERE id = $1
			`, cr.RunID, status, errCode, reqID, finishReason, usageRaw, usageStatus, priceVer, cost)
			return false, err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE ai_runs
			SET status = $2,
			    error_code = $3,
			    provider_request_id = NULLIF($4, ''),
			    finish_reason = NULLIF($5, ''),
			    finished_at = now(),
			    lease_token = NULL,
			    lease_expires_at = NULL,
			    usage_json = $6,
			    usage_status = $7,
			    price_version = $8,
			    estimated_cost = $9,
			    updated_at = now()
			WHERE id = $1
		`, cr.RunID, status, errCode, reqID, finishReason, usageRaw, usageStatus, priceVer, cost); err != nil {
			return false, err
		}

		if resolvedAt == nil {
			evName := "run.failed"
			if status == StatusBlocked {
				evName = "run.blocked"
			}
			failPayload, _ := json.Marshal(map[string]any{
				"run_id":     cr.RunID.String(),
				"slot":       cr.Slot,
				"status":     status,
				"error_code": errCode,
			})
			if err := appendTurnEventTx(ctx, tx, cr.TurnID, cr.OwnerID, cr.ConversationID, &cr.RunID, nextEventSeq, evName, failPayload); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	if notified {
		w.broker.Notify(cr.TurnID)
	}
}

func (w *Worker) finalizeCancelledIfStillOwned(ctx context.Context, cr claimedRun, usage *Usage) {
	modelEntry, _ := w.models.GetByAlias(cr.ModelAlias)
	cost, usageStatus := EstimateCost(w.models, modelEntry, usage)
	var usageRaw []byte
	if usage != nil {
		usageRaw, _ = json.Marshal(usage)
	}
	var priceVer *string
	if cost != nil && modelEntry.PriceVersion != "" {
		pv := modelEntry.PriceVersion
		priceVer = &pv
	}

	_, _ = w.pool.Exec(ctx, `
		UPDATE ai_runs
		SET status = 'cancelled',
		    finished_at = COALESCE(finished_at, now()),
		    lease_token = NULL,
		    lease_expires_at = NULL,
		    usage_json = COALESCE($3, usage_json),
		    usage_status = CASE WHEN $3 IS NOT NULL THEN $4 ELSE usage_status END,
		    price_version = COALESCE($5, price_version),
		    estimated_cost = COALESCE($6, estimated_cost),
		    updated_at = now()
		WHERE id = $1 AND lease_token = $2
	`, cr.RunID, cr.LeaseToken, usageRaw, usageStatus, priceVer, cost)
	w.broker.Notify(cr.TurnID)
}

// ReapExpiredLeases marks running or streaming runs whose lease has expired as
// interrupted without re-calling any external provider (Spec Section 14.4, AI09).
func (w *Worker) ReapExpiredLeases(ctx context.Context) (int, error) {
	return storage.WithTx(ctx, w.pool, func(tx pgx.Tx) (int, error) {
		rows, err := tx.Query(ctx, `
			SELECT id, owner_id, conversation_id, turn_id, slot
			FROM ai_runs
			WHERE status IN ('running', 'streaming')
			  AND lease_expires_at IS NOT NULL
			  AND lease_expires_at < now()
			ORDER BY id ASC
			FOR UPDATE SKIP LOCKED
		`)
		if err != nil {
			return 0, err
		}
		type expiredItem struct {
			runID  uuid.UUID
			owner  uuid.UUID
			convID uuid.UUID
			turnID uuid.UUID
			slot   string
		}
		var items []expiredItem
		for rows.Next() {
			var it expiredItem
			if err := rows.Scan(&it.runID, &it.owner, &it.convID, &it.turnID, &it.slot); err != nil {
				rows.Close()
				return 0, err
			}
			items = append(items, it)
		}
		rows.Close()
		if len(items) == 0 {
			return 0, nil
		}

		for _, it := range items {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = 'interrupted',
				    error_code = 'LEASE_EXPIRED_INTERRUPTED',
				    finished_at = now(),
				    lease_token = NULL,
				    lease_expires_at = NULL,
				    updated_at = now()
				WHERE id = $1
			`, it.runID); err != nil {
				return 0, err
			}

			var nextSeq int64
			var resolvedAt *time.Time
			var purgedAt *time.Time
			err := tx.QueryRow(ctx, `
				SELECT t.next_event_seq, t.resolved_at, c.purged_at
				FROM conversation_turns t
				JOIN conversations c ON c.id = t.conversation_id
				WHERE t.id = $1 AND t.owner_id = $2
				FOR UPDATE OF t
			`, it.turnID, it.owner).Scan(&nextSeq, &resolvedAt, &purgedAt)
			if err == nil && resolvedAt == nil && purgedAt == nil {
				payload, _ := json.Marshal(map[string]any{
					"run_id":     it.runID.String(),
					"slot":       it.slot,
					"status":     StatusInterrupted,
					"error_code": "LEASE_EXPIRED_INTERRUPTED",
				})
				_ = appendTurnEventTx(ctx, tx, it.turnID, it.owner, it.convID, &it.runID, nextSeq, "run.failed", payload)
			}
			w.broker.Notify(it.turnID)
		}
		return len(items), nil
	})
}

func appendTurnEventTx(
	ctx context.Context,
	tx pgx.Tx,
	turnID, ownerID, convID uuid.UUID,
	runID *uuid.UUID,
	seq int64,
	eventType string,
	payload []byte,
) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO ai_run_events (turn_id, seq, owner_id, conversation_id, run_id, event_type, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, turnID, seq, ownerID, convID, runID, eventType, payload); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE conversation_turns
		SET next_event_seq = $2
		WHERE id = $1
	`, turnID, seq+1)
	return err
}
