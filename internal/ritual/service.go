package ritual

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ActiveActionDTO struct {
	ID           uuid.UUID `json:"id"`
	Key          string    `json:"key"`
	Label        string    `json:"label"`
	Description  string    `json:"description"`
	StartedAt    time.Time `json:"started_at"`
	PlannedEndAt time.Time `json:"planned_end_at"`
	DurationMs   int64     `json:"duration_ms"`
}

type FlowerSummaryDTO struct {
	ID             uuid.UUID       `json:"id"`
	SpeciesID      string          `json:"species_id"`
	SpeciesName    string          `json:"species_name"`
	StyleSeed      string          `json:"style_seed"`
	VisualSnapshot json.RawMessage `json:"visual_snapshot"`
	Status         string          `json:"status"`
	KeptLine       string          `json:"kept_line"`
	PendingRow     *int16          `json:"pending_row,omitempty"`
	PendingCol     *int16          `json:"pending_col,omitempty"`
	PlantedRow     *int16          `json:"planted_row,omitempty"`
	PlantedCol     *int16          `json:"planted_col,omitempty"`
	PlantedAt      *time.Time      `json:"planted_at,omitempty"`
	Version        string          `json:"version"`
}

type RitualView struct {
	ID                uuid.UUID         `json:"id"`
	ConversationID    uuid.UUID         `json:"conversation_id"`
	Kind              string            `json:"kind"`
	DefinitionVersion string            `json:"definition_version"`
	Route             string            `json:"route"`
	RouteLabel        string            `json:"route_label"`
	Cursor            int               `json:"cursor"`
	TotalActions      int               `json:"total_actions"`
	VisualStage       int               `json:"visual_stage"`
	Status            string            `json:"status"`
	Version           string            `json:"version"`
	CurrentStepLabel  string            `json:"current_step_label"`
	CurrentDesc       string            `json:"current_description"`
	NextActionKey     string            `json:"next_action_key,omitempty"`
	NextActionLabel   string            `json:"next_action_label,omitempty"`
	ActiveAction      *ActiveActionDTO  `json:"active_action,omitempty"`
	AllowedCommands   []string          `json:"allowed_commands"`
	Flower            *FlowerSummaryDTO `json:"flower,omitempty"`
}

type ClosureInput struct {
	ExpectedVersion  string `json:"expected_version"`
	CancelPending    bool   `json:"cancel_pending"`
	DraftDisposition string `json:"draft_disposition"` // "require_empty" or "discard"
}

type ClosureResult struct {
	ConversationID      uuid.UUID         `json:"conversation_id"`
	ConversationStage   string            `json:"conversation_stage"`
	ConversationVersion string            `json:"conversation_version"`
	Ritual              *RitualView       `json:"ritual,omitempty"`
	Flower              *FlowerSummaryDTO `json:"flower,omitempty"`
}

type CompleteResult struct {
	ConversationID      uuid.UUID         `json:"conversation_id"`
	ConversationStage   string            `json:"conversation_stage"`
	ConversationVersion string            `json:"conversation_version"`
	CloseReason         string            `json:"close_reason"`
	Purged              bool              `json:"purged"`
	RitualID            uuid.UUID         `json:"ritual_id"`
	RitualStatus        string            `json:"ritual_status"`
	Flower              *FlowerSummaryDTO `json:"flower,omitempty"`
	GardenVersion       string            `json:"garden_version,omitempty"`
}

type Service struct {
	pool      *pgxpool.Pool
	rituals   *config.RitualCatalog
	flowers   *config.FlowerCatalog
	broker    *ai.SSEBroker
	retention time.Duration
}

func NewService(
	pool *pgxpool.Pool,
	rituals *config.RitualCatalog,
	flowers *config.FlowerCatalog,
	broker *ai.SSEBroker,
	retention time.Duration,
) *Service {
	return &Service{
		pool:      pool,
		rituals:   rituals,
		flowers:   flowers,
		broker:    broker,
		retention: retention,
	}
}

func (s *Service) BeginClosure(ctx context.Context, ownerID, convID uuid.UUID, idem character.IdemMeta, in ClosureInput) (ClosureResult, error) {
	expVer, err := parseVer(in.ExpectedVersion)
	if err != nil {
		return ClosureResult{}, err
	}
	disp := in.DraftDisposition
	if disp == "" {
		disp = "require_empty"
	}
	if disp != "require_empty" && disp != "discard" {
		return ClosureResult{}, apperr.Validation("draft_disposition은 require_empty 또는 discard여야 해요.", nil)
	}

	var cancelledRunIDs []uuid.UUID
	var cancelledTurnID *uuid.UUID

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (ClosureResult, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return ClosureResult{}, err
		}
		if rec != nil {
			var replay ClosureResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return ClosureResult{}, err
			}
			return replay, nil
		}

		// Lock conversation -> draft -> turn -> runs -> ritual/flower in canonical order (Spec Section 21.1)
		var kind, stage string
		var curVer int64
		var purgedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT kind, stage, version, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&kind, &stage, &curVer, &purgedAt)
		if err == pgx.ErrNoRows {
			return ClosureResult{}, apperr.NotFound("대화를 찾을 수 없어요.")
		}
		if err != nil {
			return ClosureResult{}, err
		}
		if purgedAt != nil || stage == "completed" {
			return ClosureResult{}, apperr.ContentPurged()
		}
		if curVer != expVer {
			return ClosureResult{}, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}
		if stage != "chat" {
			return ClosureResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 마무리 단계에 들어간 대화예요.", false, map[string]any{"stage": stage})
		}

		// Check unsent draft
		var draftContent string
		err = tx.QueryRow(ctx, `
			SELECT content FROM conversation_drafts
			WHERE conversation_id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&draftContent)
		if err != nil && err != pgx.ErrNoRows {
			return ClosureResult{}, err
		}
		if strings.TrimSpace(draftContent) != "" {
			if disp == "require_empty" {
				return ClosureResult{}, apperr.New(
					apperr.CodeUnsentDraft,
					http.StatusConflict,
					"아직 보내지 않은 작성 중인 글이 있어요. 보내고 마무리하거나 초안을 버리고 마무리할 수 있어요.",
					false,
					nil,
				)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE conversation_drafts
				SET content = '', draft_version = draft_version + 1, updated_at = now()
				WHERE conversation_id = $1 AND owner_id = $2
			`, convID, ownerID); err != nil {
				return ClosureResult{}, err
			}
		}

		// Check unresolved turn (Spec Section 21.4)
		var unresolvedTurnID uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT id FROM conversation_turns
			WHERE conversation_id = $1 AND owner_id = $2 AND resolved_at IS NULL
			FOR UPDATE
		`, convID, ownerID).Scan(&unresolvedTurnID)
		if err != nil && err != pgx.ErrNoRows {
			return ClosureResult{}, err
		}
		if err == nil {
			if !in.CancelPending {
				return ClosureResult{}, apperr.New(
					apperr.CodeTurnUnresolved,
					http.StatusConflict,
					"아직 답변이 진행 중이거나 선택 대기 중이에요. 기다리거나 멈추고 정리할 수 있어요.",
					false,
					map[string]any{"turn_id": unresolvedTurnID.String()},
				)
			}
			cancelledTurnID = &unresolvedTurnID
			rows, err := tx.Query(ctx, `
				SELECT id FROM ai_runs
				WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
				ORDER BY id ASC
				FOR UPDATE
			`, unresolvedTurnID, ownerID)
			if err != nil {
				return ClosureResult{}, err
			}
			for rows.Next() {
				var rid uuid.UUID
				if err := rows.Scan(&rid); err != nil {
					rows.Close()
					return ClosureResult{}, err
				}
				cancelledRunIDs = append(cancelledRunIDs, rid)
			}
			rows.Close()

			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = CASE WHEN status IN ('queued', 'running', 'streaming') THEN 'cancelled' ELSE status END,
				    lease_token = NULL,
				    lease_expires_at = NULL,
				    finished_at = COALESCE(finished_at, now()),
				    updated_at = now()
				WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
			`, unresolvedTurnID, ownerID); err != nil {
				return ClosureResult{}, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE conversation_turns
				SET resolution = 'cancelled_on_closure',
				    resolved_at = now(),
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
			`, unresolvedTurnID); err != nil {
				return ClosureResult{}, err
			}
		}

		now := time.Now().UTC()
		var out ClosureResult

		if kind == "release" {
			routeDef := s.rituals.Routes["common"]
			packDef := routeDef.Actions[0] // pack (0.8s) starts immediately upon entering release_ritual (Spec Section 16.1)
			ritualID := uuid.New()
			actionID := uuid.New()
			plannedEnd := now.Add(time.Duration(packDef.DurationMs) * time.Millisecond)

			if _, err := tx.Exec(ctx, `
				INSERT INTO rituals (
					id, owner_id, conversation_id, kind, definition_version, route,
					cursor, next_ordinal, active_action_id, status, version
				) VALUES ($1, $2, $3, 'release', $4, 'common', 0, 2, $5, 'active', 1)
			`, ritualID, ownerID, convID, s.rituals.DefinitionVersion, actionID); err != nil {
				return ClosureResult{}, err
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO ritual_actions (
					id, owner_id, ritual_id, ordinal, route, action_key, started_at, planned_end_at
				) VALUES ($1, $2, $3, 1, 'common', $4, $5, $6)
			`, actionID, ownerID, ritualID, packDef.Key, now, plannedEnd); err != nil {
				return ClosureResult{}, err
			}

			newVer := curVer + 1
			if _, err := tx.Exec(ctx, `
				UPDATE conversations
				SET stage = 'release_ritual',
				    content_epoch = content_epoch + 1,
				    version = $3,
				    updated_at = now()
				WHERE id = $1 AND owner_id = $2
			`, convID, ownerID, newVer); err != nil {
				return ClosureResult{}, err
			}

			rv := s.buildRitualView(ritualRow{
				ID:                ritualID,
				OwnerID:           ownerID,
				ConversationID:    convID,
				Kind:              "release",
				DefinitionVersion: s.rituals.DefinitionVersion,
				Route:             "common",
				Cursor:            0,
				ActiveActionID:    &actionID,
				Status:            "active",
				Version:           1,
			}, &actionRow{
				ID:           actionID,
				ActionKey:    packDef.Key,
				StartedAt:    now,
				PlannedEndAt: plannedEnd,
			}, nil)

			out = ClosureResult{
				ConversationID:      convID,
				ConversationStage:   "release_ritual",
				ConversationVersion: strconv.FormatInt(newVer, 10),
				Ritual:              &rv,
			}
		} else {
			// grow -> gift_ready: ensure at most 1 flower per conversation (FLOW01)
			flw, err := s.ensureFlowerGiftTx(ctx, tx, ownerID, convID)
			if err != nil {
				return ClosureResult{}, err
			}
			newVer := curVer + 1
			if _, err := tx.Exec(ctx, `
				UPDATE conversations
				SET stage = 'gift_ready',
				    content_epoch = content_epoch + 1,
				    version = $3,
				    updated_at = now()
				WHERE id = $1 AND owner_id = $2
			`, convID, ownerID, newVer); err != nil {
				return ClosureResult{}, err
			}
			out = ClosureResult{
				ConversationID:      convID,
				ConversationStage:   "gift_ready",
				ConversationVersion: strconv.FormatInt(newVer, 10),
				Flower:              &flw,
			}
		}

		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusOK, respBytes, map[string]string{"conversation_id": convID.String()}, s.retention,
		); err != nil {
			return ClosureResult{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ClosureResult{}, err
		}
		return ClosureResult{}, apperr.Internal(err)
	}

	for _, rid := range cancelledRunIDs {
		s.broker.CancelRun(rid)
	}
	if cancelledTurnID != nil {
		s.broker.Notify(*cancelledTurnID)
	}
	return res, nil
}

func (s *Service) GetRitual(ctx context.Context, ownerID, ritualID uuid.UUID) (RitualView, error) {
	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (RitualView, error) {
		rRow, actRow, err := s.loadAndSettleRitualTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return RitualView{}, err
		}
		var flowerPtr *FlowerSummaryDTO
		if rRow.Kind == "planting" {
			f, err := loadFlowerByConversationTx(ctx, tx, ownerID, rRow.ConversationID)
			if err == nil {
				flowerPtr = &f
			}
		}
		return s.buildRitualView(rRow, actRow, flowerPtr), nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return RitualView{}, err
		}
		return RitualView{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) ReturnToChat(ctx context.Context, ownerID, ritualID uuid.UUID, idem character.IdemMeta, expectedVersion string) (ClosureResult, error) {
	expVer, err := parseVer(expectedVersion)
	if err != nil {
		return ClosureResult{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (ClosureResult, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return ClosureResult{}, err
		}
		if rec != nil {
			var replay ClosureResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return ClosureResult{}, err
			}
			return replay, nil
		}

		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT conversation_id FROM rituals WHERE id = $1 AND owner_id = $2`, ritualID, ownerID).Scan(&convID); err != nil {
			if err == pgx.ErrNoRows {
				return ClosureResult{}, apperr.NotFound("연출 정보를 찾을 수 없어요.")
			}
			return ClosureResult{}, err
		}

		// Canonical lock: conversation -> ritual
		var convStage string
		var convVer int64
		if err := tx.QueryRow(ctx, `
			SELECT stage, version FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, convID, ownerID).Scan(&convStage, &convVer); err != nil {
			return ClosureResult{}, err
		}

		rRow, actRow, err := s.loadAndSettleRitualTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return ClosureResult{}, err
		}
		if rRow.Version != expVer {
			return ClosureResult{}, apperr.VersionConflict(strconv.FormatInt(rRow.Version, 10))
		}
		// Spec Appendix A: allowed ONLY when route == 'common' and no active action
		if rRow.Status != "active" || rRow.Kind != "release" || rRow.Route != "common" || actRow != nil {
			return ClosureResult{}, apperr.New(
				apperr.CodeInvalidTransition,
				http.StatusConflict,
				"장소를 선택한 뒤이거나 동작 중일 때는 대화로 돌아갈 수 없어요.",
				false,
				nil,
			)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET status = 'abandoned', version = version + 1, updated_at = now()
			WHERE id = $1
		`, ritualID); err != nil {
			return ClosureResult{}, err
		}

		newConvVer := convVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET stage = 'chat', version = $2, updated_at = now()
			WHERE id = $1
		`, convID, newConvVer); err != nil {
			return ClosureResult{}, err
		}

		out := ClosureResult{
			ConversationID:      convID,
			ConversationStage:   "chat",
			ConversationVersion: strconv.FormatInt(newConvVer, 10),
		}
		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return ClosureResult{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ClosureResult{}, err
		}
		return ClosureResult{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) SelectRoute(ctx context.Context, ownerID, ritualID uuid.UUID, idem character.IdemMeta, expectedVersion, route string) (RitualView, error) {
	expVer, err := parseVer(expectedVersion)
	if err != nil {
		return RitualView{}, err
	}
	if route != "sea" && route != "incinerate" && route != "bury" {
		return RitualView{}, apperr.Validation("유효한 목적지 경로(sea, incinerate, bury)를 선택해 주세요.", map[string]string{"route": "허용되지 않는 경로예요."})
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (RitualView, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return RitualView{}, err
		}
		if rec != nil {
			var replay RitualView
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return RitualView{}, err
			}
			return replay, nil
		}

		rRow, actRow, err := s.loadAndSettleRitualTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return RitualView{}, err
		}
		if rRow.Version != expVer {
			return RitualView{}, apperr.VersionConflict(strconv.FormatInt(rRow.Version, 10))
		}
		if actRow != nil {
			return RitualView{}, apperr.New(apperr.CodeActionInProgress, http.StatusConflict, "현재 동작이 끝난 뒤 선택해 주세요.", false, nil)
		}
		commonDef := s.rituals.Routes["common"]
		if rRow.Status != "active" || rRow.Kind != "release" || rRow.Route != "common" || rRow.Cursor != len(commonDef.Actions) {
			return RitualView{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "봉투를 어깨에 멘 뒤에 이동할 장소를 고를 수 있어요.", false, nil)
		}

		targetDef := s.rituals.Routes[route]
		arriveDef := targetDef.Actions[0]
		now := time.Now().UTC()
		actionID := uuid.New()
		plannedEnd := now.Add(time.Duration(arriveDef.DurationMs) * time.Millisecond)

		if _, err := tx.Exec(ctx, `
			INSERT INTO ritual_actions (
				id, owner_id, ritual_id, ordinal, route, action_key, started_at, planned_end_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, actionID, ownerID, ritualID, rRow.NextOrdinal, route, arriveDef.Key, now, plannedEnd); err != nil {
			return RitualView{}, err
		}

		newVer := rRow.Version + 1
		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET route = $2,
			    cursor = 0,
			    next_ordinal = next_ordinal + 1,
			    active_action_id = $3,
			    version = $4,
			    updated_at = now()
			WHERE id = $1
		`, ritualID, route, actionID, newVer); err != nil {
			return RitualView{}, err
		}

		rRow.Route = route
		rRow.Cursor = 0
		rRow.ActiveActionID = &actionID
		rRow.Version = newVer

		view := s.buildRitualView(rRow, &actionRow{
			ID:           actionID,
			ActionKey:    arriveDef.Key,
			StartedAt:    now,
			PlannedEndAt: plannedEnd,
		}, nil)

		respBytes, _ := json.Marshal(view)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return RitualView{}, err
		}
		return view, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return RitualView{}, err
		}
		return RitualView{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) StartAction(ctx context.Context, ownerID, ritualID uuid.UUID, idem character.IdemMeta, expectedVersion, actionKey string) (RitualView, error) {
	expVer, err := parseVer(expectedVersion)
	if err != nil {
		return RitualView{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (RitualView, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return RitualView{}, err
		}
		if rec != nil {
			var replay RitualView
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return RitualView{}, err
			}
			return replay, nil
		}

		rRow, actRow, err := s.loadAndSettleRitualTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return RitualView{}, err
		}
		if rRow.Version != expVer {
			return RitualView{}, apperr.VersionConflict(strconv.FormatInt(rRow.Version, 10))
		}
		if rRow.Status != "active" {
			return RitualView{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 완료된 과정이에요.", false, nil)
		}
		if actRow != nil {
			return RitualView{}, apperr.New(apperr.CodeActionInProgress, http.StatusConflict, "현재 동작이 진행 중이에요.", false, nil)
		}

		routeDef, ok := s.rituals.Routes[rRow.Route]
		if !ok || rRow.Cursor >= len(routeDef.Actions) {
			return RitualView{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "진행할 수 있는 다음 동작이 없어요.", false, nil)
		}
		nextDef := routeDef.Actions[rRow.Cursor]
		if nextDef.Key != actionKey {
			return RitualView{}, apperr.New(
				apperr.CodeInvalidTransition,
				http.StatusConflict,
				"순서에 맞지 않는 동작 요청이에요.",
				false,
				map[string]any{"expected_action_key": nextDef.Key},
			)
		}

		now := time.Now().UTC()
		actionID := uuid.New()
		plannedEnd := now.Add(time.Duration(nextDef.DurationMs) * time.Millisecond)

		if _, err := tx.Exec(ctx, `
			INSERT INTO ritual_actions (
				id, owner_id, ritual_id, ordinal, route, action_key, started_at, planned_end_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, actionID, ownerID, ritualID, rRow.NextOrdinal, rRow.Route, nextDef.Key, now, plannedEnd); err != nil {
			return RitualView{}, err
		}

		newVer := rRow.Version + 1
		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET next_ordinal = next_ordinal + 1,
			    active_action_id = $2,
			    version = $3,
			    updated_at = now()
			WHERE id = $1
		`, ritualID, actionID, newVer); err != nil {
			return RitualView{}, err
		}

		rRow.ActiveActionID = &actionID
		rRow.Version = newVer

		var flowerPtr *FlowerSummaryDTO
		if rRow.Kind == "planting" {
			if f, err := loadFlowerByConversationTx(ctx, tx, ownerID, rRow.ConversationID); err == nil {
				flowerPtr = &f
			}
		}

		view := s.buildRitualView(rRow, &actionRow{
			ID:           actionID,
			ActionKey:    nextDef.Key,
			StartedAt:    now,
			PlannedEndAt: plannedEnd,
		}, flowerPtr)

		respBytes, _ := json.Marshal(view)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return RitualView{}, err
		}
		return view, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return RitualView{}, err
		}
		return RitualView{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) SkipAction(ctx context.Context, ownerID, ritualID, actionID uuid.UUID, idem character.IdemMeta, expectedVersion string) (RitualView, error) {
	expVer, err := parseVer(expectedVersion)
	if err != nil {
		return RitualView{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (RitualView, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return RitualView{}, err
		}
		if rec != nil {
			var replay RitualView
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return RitualView{}, err
			}
			return replay, nil
		}

		rRow, err := lockRitualRowTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return RitualView{}, err
		}
		if rRow.Version != expVer {
			return RitualView{}, apperr.VersionConflict(strconv.FormatInt(rRow.Version, 10))
		}

		// Only complete the explicitly targeted actionID; never advance into next action (RIT06)
		if rRow.ActiveActionID == nil || *rRow.ActiveActionID != actionID {
			return RitualView{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "건너뛸 수 있는 현재 동작이 아니에요.", false, nil)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE ritual_actions
			SET completed_at = COALESCE(completed_at, now()),
			    skipped = true
			WHERE id = $1 AND ritual_id = $2
		`, actionID, ritualID); err != nil {
			return RitualView{}, err
		}

		newVer := rRow.Version + 1
		newCursor := rRow.Cursor + 1
		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET cursor = $2,
			    active_action_id = NULL,
			    version = $3,
			    updated_at = now()
			WHERE id = $1
		`, ritualID, newCursor, newVer); err != nil {
			return RitualView{}, err
		}

		rRow.Cursor = newCursor
		rRow.ActiveActionID = nil
		rRow.Version = newVer

		var flowerPtr *FlowerSummaryDTO
		if rRow.Kind == "planting" {
			if f, err := loadFlowerByConversationTx(ctx, tx, ownerID, rRow.ConversationID); err == nil {
				flowerPtr = &f
			}
		}

		view := s.buildRitualView(rRow, nil, flowerPtr)
		respBytes, _ := json.Marshal(view)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return RitualView{}, err
		}
		return view, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return RitualView{}, err
		}
		return RitualView{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) CompleteRitual(ctx context.Context, ownerID, ritualID uuid.UUID, idem character.IdemMeta, expectedVersion string) (CompleteResult, error) {
	expVer, err := parseVer(expectedVersion)
	if err != nil {
		return CompleteResult{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (CompleteResult, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return CompleteResult{}, err
		}
		if rec != nil {
			var replay CompleteResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return CompleteResult{}, err
			}
			return replay, nil
		}

		var convID uuid.UUID
		var kind string
		if err := tx.QueryRow(ctx, `
			SELECT conversation_id, kind FROM rituals WHERE id = $1 AND owner_id = $2
		`, ritualID, ownerID).Scan(&convID, &kind); err != nil {
			if err == pgx.ErrNoRows {
				return CompleteResult{}, apperr.NotFound("연출 정보를 찾을 수 없어요.")
			}
			return CompleteResult{}, err
		}

		// Canonical lock order: garden (if planting) -> conversation -> ritual -> flower -> occupancy
		var gardenID uuid.UUID
		var gardenVer int64
		if kind == "planting" {
			if err := tx.QueryRow(ctx, `
				SELECT id, version FROM gardens WHERE owner_id = $1 FOR UPDATE
			`, ownerID).Scan(&gardenID, &gardenVer); err != nil {
				return CompleteResult{}, err
			}
		}

		var convStage string
		var convVer int64
		if err := tx.QueryRow(ctx, `
			SELECT stage, version FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, convID, ownerID).Scan(&convStage, &convVer); err != nil {
			return CompleteResult{}, err
		}

		rRow, actRow, err := s.loadAndSettleRitualTx(ctx, tx, ownerID, ritualID)
		if err != nil {
			return CompleteResult{}, err
		}
		if rRow.Version != expVer {
			return CompleteResult{}, apperr.VersionConflict(strconv.FormatInt(rRow.Version, 10))
		}
		if actRow != nil {
			return CompleteResult{}, apperr.New(apperr.CodeActionInProgress, http.StatusConflict, "마지막 동작이 끝난 뒤 마무리할 수 있어요.", false, nil)
		}

		routeDef, ok := s.rituals.Routes[rRow.Route]
		if !ok || rRow.Route == "common" || rRow.Cursor != len(routeDef.Actions) || rRow.Status != "active" {
			return CompleteResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "모든 동작을 마친 뒤에 최종 마무리할 수 있어요.", false, nil)
		}

		var flowerOut *FlowerSummaryDTO
		var gardenVerStr string

		if kind == "planting" {
			var flowerID uuid.UUID
			var flowerStatus string
			if err := tx.QueryRow(ctx, `
				SELECT id, status FROM flower_gifts
				WHERE source_conversation_id = $1 AND owner_id = $2
				FOR UPDATE
			`, convID, ownerID).Scan(&flowerID, &flowerStatus); err != nil {
				return CompleteResult{}, err
			}
			if flowerStatus != "planting" {
				return CompleteResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "식재 진행 중인 꽃 상태가 아니에요.", false, nil)
			}

			var rowNo, colNo int16
			var occState string
			if err := tx.QueryRow(ctx, `
				SELECT row_no, col_no, state
				FROM garden_occupancies
				WHERE garden_id = $1 AND flower_id = $2 AND ritual_id = $3 AND owner_id = $4
				FOR UPDATE
			`, gardenID, flowerID, ritualID, ownerID).Scan(&rowNo, &colNo, &occState); err != nil {
				return CompleteResult{}, err
			}
			if occState != "reserved" {
				return CompleteResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "유효한 자리 예약 상태가 아니에요.", false, nil)
			}

			now := time.Now().UTC()
			if _, err := tx.Exec(ctx, `
				UPDATE garden_occupancies
				SET state = 'planted', planted_at = $3
				WHERE garden_id = $1 AND flower_id = $2
			`, gardenID, flowerID, now); err != nil {
				return CompleteResult{}, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE flower_gifts
				SET status = 'planted',
				    planted_row = $2,
				    planted_col = $3,
				    planted_at = $4,
				    pending_row = NULL,
				    pending_col = NULL,
				    version = version + 1,
				    updated_at = now()
				WHERE id = $1
			`, flowerID, rowNo, colNo, now); err != nil {
				return CompleteResult{}, err
			}

			newGardenVer := gardenVer + 1
			if _, err := tx.Exec(ctx, `
				UPDATE gardens
				SET version = $2, updated_at = now()
				WHERE id = $1
			`, gardenID, newGardenVer); err != nil {
				return CompleteResult{}, err
			}
			gardenVerStr = strconv.FormatInt(newGardenVer, 10)

			flw, err := loadFlowerByConversationTx(ctx, tx, ownerID, convID)
			if err != nil {
				return CompleteResult{}, err
			}
			flowerOut = &flw
		}

		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET status = 'completed',
			    completed_at = now(),
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1
		`, ritualID); err != nil {
			return CompleteResult{}, err
		}

		newConvVer := convVer + 1
		closeReason := routeDef.CloseReason
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET stage = 'completed',
			    closed_at = now(),
			    close_reason = $2,
			    version = $3,
			    updated_at = now()
			WHERE id = $1
		`, convID, closeReason, newConvVer); err != nil {
			return CompleteResult{}, err
		}

		// Atomically purge raw conversation content in the same transaction (Spec Section 21.5, GARD09)
		if err := PurgeConversationTx(ctx, tx, ownerID, convID); err != nil {
			return CompleteResult{}, err
		}

		out := CompleteResult{
			ConversationID:      convID,
			ConversationStage:   "completed",
			ConversationVersion: strconv.FormatInt(newConvVer, 10),
			CloseReason:         closeReason,
			Purged:              true,
			RitualID:            ritualID,
			RitualStatus:        "completed",
			Flower:              flowerOut,
			GardenVersion:       gardenVerStr,
		}

		resultIDs := map[string]string{
			"conversation_id": convID.String(),
			"ritual_id":       ritualID.String(),
		}
		if flowerOut != nil {
			resultIDs["flower_id"] = flowerOut.ID.String()
		}
		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, resultIDs, s.retention); err != nil {
			return CompleteResult{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return CompleteResult{}, err
		}
		return CompleteResult{}, apperr.Internal(err)
	}
	return res, nil
}

type ritualRow struct {
	ID                uuid.UUID
	OwnerID           uuid.UUID
	ConversationID    uuid.UUID
	Kind              string
	DefinitionVersion string
	Route             string
	Cursor            int
	NextOrdinal       int
	ActiveActionID    *uuid.UUID
	Status            string
	Version           int64
}

type actionRow struct {
	ID           uuid.UUID
	ActionKey    string
	StartedAt    time.Time
	PlannedEndAt time.Time
}

func lockRitualRowTx(ctx context.Context, tx pgx.Tx, ownerID, ritualID uuid.UUID) (ritualRow, error) {
	var r ritualRow
	err := tx.QueryRow(ctx, `
		SELECT id, owner_id, conversation_id, kind, definition_version, route,
		       cursor, next_ordinal, active_action_id, status, version
		FROM rituals
		WHERE id = $1 AND owner_id = $2
		FOR UPDATE
	`, ritualID, ownerID).Scan(
		&r.ID, &r.OwnerID, &r.ConversationID, &r.Kind, &r.DefinitionVersion,
		&r.Route, &r.Cursor, &r.NextOrdinal, &r.ActiveActionID, &r.Status, &r.Version,
	)
	if err == pgx.ErrNoRows {
		return ritualRow{}, apperr.NotFound("연출 정보를 찾을 수 없어요.")
	}
	return r, err
}

// loadAndSettleRitualTx settles ONLY the single currently running action if server time
// has passed planned_end_at; it NEVER auto-starts subsequent actions (Spec Section 18, RIT07).
func (s *Service) loadAndSettleRitualTx(ctx context.Context, tx pgx.Tx, ownerID, ritualID uuid.UUID) (ritualRow, *actionRow, error) {
	rRow, err := lockRitualRowTx(ctx, tx, ownerID, ritualID)
	if err != nil {
		return ritualRow{}, nil, err
	}
	if rRow.ActiveActionID == nil {
		return rRow, nil, nil
	}

	var aRow actionRow
	var completedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, action_key, started_at, planned_end_at, completed_at
		FROM ritual_actions
		WHERE id = $1 AND ritual_id = $2
		FOR UPDATE
	`, *rRow.ActiveActionID, ritualID).Scan(
		&aRow.ID, &aRow.ActionKey, &aRow.StartedAt, &aRow.PlannedEndAt, &completedAt,
	)
	if err != nil {
		return ritualRow{}, nil, err
	}

	now := time.Now().UTC()
	if completedAt != nil || !now.Before(aRow.PlannedEndAt) {
		if completedAt == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE ritual_actions SET completed_at = $2 WHERE id = $1
			`, aRow.ID, now); err != nil {
				return ritualRow{}, nil, err
			}
		}
		rRow.Cursor++
		rRow.ActiveActionID = nil
		rRow.Version++
		if _, err := tx.Exec(ctx, `
			UPDATE rituals
			SET cursor = $2, active_action_id = NULL, version = $3, updated_at = now()
			WHERE id = $1
		`, ritualID, rRow.Cursor, rRow.Version); err != nil {
			return ritualRow{}, nil, err
		}
		return rRow, nil, nil
	}

	return rRow, &aRow, nil
}

func (s *Service) buildRitualView(r ritualRow, act *actionRow, flower *FlowerSummaryDTO) RitualView {
	routeDef := s.rituals.Routes[r.Route]
	view := RitualView{
		ID:                r.ID,
		ConversationID:    r.ConversationID,
		Kind:              r.Kind,
		DefinitionVersion: r.DefinitionVersion,
		Route:             r.Route,
		RouteLabel:        routeDef.Label,
		Cursor:            r.Cursor,
		TotalActions:      len(routeDef.Actions),
		Status:            r.Status,
		Version:           strconv.FormatInt(r.Version, 10),
		AllowedCommands:   []string{},
		Flower:            flower,
	}

	if act != nil {
		var actDef config.RitualActionDef
		for _, d := range routeDef.Actions {
			if d.Key == act.ActionKey {
				actDef = d
				break
			}
		}
		view.VisualStage = actDef.VisualStage
		view.CurrentStepLabel = actDef.Label
		view.CurrentDesc = actDef.Description
		view.ActiveAction = &ActiveActionDTO{
			ID:           act.ID,
			Key:          act.ActionKey,
			Label:        actDef.Label,
			Description:  actDef.Description,
			StartedAt:    act.StartedAt,
			PlannedEndAt: act.PlannedEndAt,
			DurationMs:   actDef.DurationMs,
		}
		view.AllowedCommands = []string{"skip_current_action"}
		return view
	}

	// Settled / waiting for next user command
	if r.Cursor == 0 {
		view.VisualStage = 0
	} else if r.Cursor <= len(routeDef.Actions) {
		prev := routeDef.Actions[r.Cursor-1]
		view.VisualStage = prev.CompletedVisualStage
		view.CurrentStepLabel = prev.Label + " 완료"
		view.CurrentDesc = prev.Description
	}

	if r.Status != "active" {
		return view
	}

	if r.Route == "common" {
		if r.Cursor < len(routeDef.Actions) {
			next := routeDef.Actions[r.Cursor]
			view.NextActionKey = next.Key
			view.NextActionLabel = next.Label
			view.AllowedCommands = []string{"start_next_action", "return_to_chat"}
		} else {
			view.AllowedCommands = []string{"select_route:sea", "select_route:incinerate", "select_route:bury", "return_to_chat"}
		}
		return view
	}

	if r.Cursor < len(routeDef.Actions) {
		next := routeDef.Actions[r.Cursor]
		view.NextActionKey = next.Key
		view.NextActionLabel = next.Label
		view.AllowedCommands = []string{"start_next_action"}
	} else {
		view.AllowedCommands = []string{"complete_ritual"}
	}
	return view
}

func (s *Service) BuildViewForActiveConversation(ctx context.Context, ownerID, convID uuid.UUID) (*RitualView, error) {
	var ritualID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM rituals
		WHERE conversation_id = $1 AND owner_id = $2 AND status = 'active'
		LIMIT 1
	`, convID, ownerID).Scan(&ritualID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v, err := s.GetRitual(ctx, ownerID, ritualID)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Service) ensureFlowerGiftTx(ctx context.Context, tx pgx.Tx, ownerID, convID uuid.UUID) (FlowerSummaryDTO, error) {
	existing, err := loadFlowerByConversationTx(ctx, tx, ownerID, convID)
	if err == nil {
		return existing, nil
	}
	if err != pgx.ErrNoRows {
		return FlowerSummaryDTO{}, err
	}

	sp := pickWeightedSpecies(s.flowers.Species)
	seed := cryptoSeedInt63()
	visSnap, _ := json.Marshal(map[string]any{
		"catalog_version": s.flowers.CatalogVersion,
		"species_id":      sp.ID,
		"species_name":    sp.NameKo,
		"petal_hex":       sp.PetalHex,
		"center_hex":      sp.CenterHex,
		"stem_hex":        sp.StemHex,
		"style_seed":      strconv.FormatInt(seed, 10),
	})

	flowerID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO flower_gifts (
			id, owner_id, source_conversation_id, catalog_version,
			species_id, species_name, style_seed, visual_snapshot, status, kept_line, version
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'available', '', 1)
	`, flowerID, ownerID, convID, s.flowers.CatalogVersion, sp.ID, sp.NameKo, seed, visSnap); err != nil {
		return FlowerSummaryDTO{}, err
	}
	return loadFlowerByConversationTx(ctx, tx, ownerID, convID)
}

func LoadFlowerByConversation(ctx context.Context, pool *pgxpool.Pool, ownerID, convID uuid.UUID) (*FlowerSummaryDTO, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, species_id, species_name, style_seed, visual_snapshot,
		       status, kept_line, pending_row, pending_col, planted_row, planted_col, planted_at, version
		FROM flower_gifts
		WHERE source_conversation_id = $1 AND owner_id = $2
	`, convID, ownerID)
	f, err := scanFlowerRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func loadFlowerByConversationTx(ctx context.Context, tx pgx.Tx, ownerID, convID uuid.UUID) (FlowerSummaryDTO, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, species_id, species_name, style_seed, visual_snapshot,
		       status, kept_line, pending_row, pending_col, planted_row, planted_col, planted_at, version
		FROM flower_gifts
		WHERE source_conversation_id = $1 AND owner_id = $2
	`, convID, ownerID)
	return scanFlowerRow(row)
}

func scanFlowerRow(row pgx.Row) (FlowerSummaryDTO, error) {
	var f FlowerSummaryDTO
	var seed int64
	var ver int64
	var visRaw []byte
	err := row.Scan(
		&f.ID, &f.SpeciesID, &f.SpeciesName, &seed, &visRaw,
		&f.Status, &f.KeptLine, &f.PendingRow, &f.PendingCol,
		&f.PlantedRow, &f.PlantedCol, &f.PlantedAt, &ver,
	)
	if err != nil {
		return FlowerSummaryDTO{}, err
	}
	f.StyleSeed = strconv.FormatInt(seed, 10)
	f.Version = strconv.FormatInt(ver, 10)
	f.VisualSnapshot = json.RawMessage(visRaw)
	return f, nil
}

func pickWeightedSpecies(species []config.FlowerSpecies) config.FlowerSpecies {
	total := 0
	for _, s := range species {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		total += w
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	target := 0
	if err == nil {
		target = int(nBig.Int64())
	}
	acc := 0
	for _, s := range species {
		w := s.Weight
		if w <= 0 {
			w = 1
		}
		acc += w
		if target < acc {
			return s
		}
	}
	return species[0]
}

func cryptoSeedInt63() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	v := int64(binary.BigEndian.Uint64(b[:]) & 0x7fffffffffffffff)
	if v == 0 {
		return 104729
	}
	return v
}

func parseVer(v string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation("expected_version 형식이 올바르지 않아요.", map[string]string{"expected_version": "양의 정수 문자열이 필요해요."})
	}
	return n, nil
}
