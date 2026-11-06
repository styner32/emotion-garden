package conversation

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/ritual"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type SlotSummary struct {
	ConversationID      uuid.UUID          `json:"conversation_id"`
	Kind                string             `json:"kind"`
	Stage               string             `json:"stage"`
	UIStatus            string             `json:"ui_status"`
	UIStatusLabel       string             `json:"ui_status_label"`
	Character           character.Snapshot `json:"character"`
	ReplyPolicy         string             `json:"reply_policy"`
	GenerationMode      string             `json:"generation_mode"`
	ActiveTurnID        *uuid.UUID         `json:"active_turn_id,omitempty"`
	ActiveRitualID      *uuid.UUID         `json:"active_ritual_id,omitempty"`
	FlowerID            *uuid.UUID         `json:"flower_id,omitempty"`
	Version             string             `json:"version"`
	UpdatedAt           time.Time          `json:"updated_at"`
}

type ActiveSlots struct {
	Release *SlotSummary `json:"release"`
	Grow    *SlotSummary `json:"grow"`
}

type MessageDTO struct {
	ID            uuid.UUID  `json:"id"`
	TurnID        *uuid.UUID `json:"turn_id,omitempty"`
	Seq           int        `json:"seq"`
	Role          string     `json:"role"`
	Origin        string     `json:"origin"`
	Content       string     `json:"content"`
	SelectedRunID *uuid.UUID `json:"selected_run_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type DraftDTO struct {
	Text         string    `json:"text"`
	DraftVersion string    `json:"draft_version"`
	ContentEpoch string    `json:"content_epoch"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RunDTO struct {
	ID                uuid.UUID       `json:"id"`
	TurnID            uuid.UUID       `json:"turn_id"`
	Slot              string          `json:"slot"`
	DisplayLabel      string          `json:"display_label"`
	Provider          string          `json:"provider"`
	ModelAlias        string          `json:"model_alias"`
	RequestedModel    string          `json:"requested_model"`
	ReportedModel     string          `json:"reported_model,omitempty"`
	IsMock            bool            `json:"is_mock"`
	BlindHidden       bool            `json:"blind_hidden"`
	AttemptNo         int             `json:"attempt_no"`
	Status            string          `json:"status"`
	CanAccept         bool            `json:"can_accept"`
	CanRetry          bool            `json:"can_retry"`
	CanCancel         bool            `json:"can_cancel"`
	OutputText        string          `json:"output_text,omitempty"`
	ErrorCode         string          `json:"error_code,omitempty"`
	FinishReason      string          `json:"finish_reason,omitempty"`
	QueueMs           *int64          `json:"queue_ms,omitempty"`
	FirstTextMs       *int64          `json:"first_text_ms,omitempty"`
	TotalMs           *int64          `json:"total_ms,omitempty"`
	Usage             json.RawMessage `json:"usage,omitempty"`
	UsageStatus       string          `json:"usage_status"`
	PriceVersion      string          `json:"price_version,omitempty"`
	EstimatedCostUSD  *string         `json:"estimated_cost_usd,omitempty"`
}

type TurnSnapshotDTO struct {
	ID            uuid.UUID  `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
	TurnNo        int        `json:"turn_no"`
	ClientTurnID  string     `json:"client_turn_id"`
	Mode          string     `json:"mode"`
	BlindMode     bool       `json:"blind_mode"`
	State         string     `json:"state"` // generating, awaiting_selection, failed, resolved
	SelectedRunID *uuid.UUID `json:"selected_run_id,omitempty"`
	Resolution    string     `json:"resolution,omitempty"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	Version       string     `json:"version"`
	LastEventSeq  string     `json:"last_event_seq"`
	EventsURL     string     `json:"events_url"`
	Runs          []RunDTO   `json:"runs"`
	HasFeedback   bool       `json:"has_feedback"`
}

type ConversationDetail struct {
	ID                uuid.UUID                `json:"id"`
	Kind              string                   `json:"kind"`
	Stage             string                   `json:"stage"`
	Character         character.Snapshot       `json:"character"`
	ExpressionState   string                   `json:"expression_state"`
	GreetingKey       string                   `json:"greeting_key"`
	ReplyPolicy       string                   `json:"reply_policy"`
	GenerationMode    string                   `json:"generation_mode"`
	ModelAliases      []string                 `json:"model_aliases"`
	Version           string                   `json:"version"`
	ContentEpoch      string                   `json:"content_epoch"`
	Messages          []MessageDTO             `json:"messages"`
	Draft             DraftDTO                 `json:"draft"`
	LatestTurn        *TurnSnapshotDTO         `json:"latest_turn,omitempty"`
	ActiveRitual      *ritual.RitualView       `json:"active_ritual,omitempty"`
	FlowerGift        *ritual.FlowerSummaryDTO `json:"flower_gift,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

type CreateConversationInput struct {
	Kind           string   `json:"kind"`
	CharacterID    string   `json:"character_id"`
	GreetingKey    string   `json:"greeting_key,omitempty"`
	ReplyPolicy    string   `json:"reply_policy,omitempty"`
	GenerationMode string   `json:"generation_mode,omitempty"`
	ModelAliases   []string `json:"model_aliases,omitempty"`
}

type UpdatePreferencesInput struct {
	ExpectedVersion string   `json:"expected_version"`
	ReplyPolicy     *string  `json:"reply_policy,omitempty"`
	GenerationMode  *string  `json:"generation_mode,omitempty"`
	ModelAliases    []string `json:"model_aliases,omitempty"`
}

type SaveDraftInput struct {
	DraftVersion  string `json:"draft_version"`
	CapturedEpoch string `json:"captured_epoch"`
	Text          string `json:"text"`
}

type SubmitTurnInput struct {
	ClientTurnID    string   `json:"client_turn_id"`
	ExpectedVersion string   `json:"expected_version"`
	Text            string   `json:"text"`
	GenerationMode  string   `json:"generation_mode,omitempty"`
	ModelAliases    []string `json:"model_aliases,omitempty"`
}

type SubmitTurnResult struct {
	TurnID              uuid.UUID      `json:"turn_id"`
	ConversationVersion string         `json:"conversation_version"`
	DraftVersion        string         `json:"draft_version"`
	TurnVersion         string         `json:"turn_version"`
	State               string         `json:"state"`
	Runs                []SubmittedRun `json:"runs"`
	EventsURL           string         `json:"events_url,omitempty"`
}

type SubmittedRun struct {
	ID     uuid.UUID `json:"id"`
	Slot   string    `json:"slot"`
	IsMock bool      `json:"is_mock"`
}

type SelectCandidateInput struct {
	ExpectedVersion string `json:"expected_version"`
	RunID           string `json:"run_id"`
}

type SkipTurnInput struct {
	ExpectedVersion string `json:"expected_version"`
}

type RunActionInput struct {
	ExpectedTurnVersion string `json:"expected_turn_version"`
}

type TurnFeedbackInput struct {
	Scores map[string]map[string]int `json:"scores"`
}

type AISettingsView struct {
	ReleaseMode         string              `json:"release_mode"`
	GrowMode            string              `json:"grow_mode"`
	ModelAliases        map[string]string   `json:"model_aliases"`
	BlindCompare        bool                `json:"blind_compare"`
	DetailedEvalEnabled bool                `json:"detailed_eval_enabled"`
	CompareNoticeAck    bool                `json:"compare_notice_ack"`
	ReducedMotion       bool                `json:"reduced_motion"`
	DailyBudgetUSD      *string             `json:"daily_budget_usd,omitempty"`
	Version             string              `json:"version"`
	OpenAIKeyConfigured bool                `json:"openai_key_configured"`
	GeminiKeyConfigured bool                `json:"gemini_key_configured"`
	MockModeEnabled     bool                `json:"mock_mode_enabled"`
	Models              []config.ModelEntry `json:"models"`
}

type UpdateAISettingsInput struct {
	ExpectedVersion     string            `json:"expected_version"`
	ReleaseMode         *string           `json:"release_mode,omitempty"`
	GrowMode            *string           `json:"grow_mode,omitempty"`
	ModelAliases        map[string]string `json:"model_aliases,omitempty"`
	BlindCompare        *bool             `json:"blind_compare,omitempty"`
	DetailedEvalEnabled *bool             `json:"detailed_eval_enabled,omitempty"`
	CompareNoticeAck    *bool             `json:"compare_notice_ack,omitempty"`
	ReducedMotion       *bool             `json:"reduced_motion,omitempty"`
	DailyBudgetUSD      *string           `json:"daily_budget_usd,omitempty"`
}

type TurnEventRecord struct {
	Seq       int64
	EventType string
	Payload   []byte
}

type ComparisonItemDTO struct {
	TurnID         uuid.UUID      `json:"turn_id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	Kind           string         `json:"kind"`
	PromptVersion  string         `json:"prompt_version"`
	ContextHash    string         `json:"context_hash"`
	Resolution     string         `json:"resolution,omitempty"`
	Category       string         `json:"category"` // complete_comparison, incomplete_early_selection, both_skipped, technical_failure, pending
	SelectedSlot   string         `json:"selected_slot,omitempty"`
	SelectedProvider string       `json:"selected_provider,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	Runs           []RunDTO       `json:"runs"`
	FeedbackScores map[string]any `json:"feedback_scores,omitempty"`
}

type ComparisonGroupSummary struct {
	GroupKey                   string         `json:"group_key"`
	PromptVersion              string         `json:"prompt_version"`
	OpenAIModel                string         `json:"openai_model"`
	GeminiModel                string         `json:"gemini_model"`
	CompleteComparisons        int            `json:"complete_comparisons"`
	IncompleteEarlySelections  int            `json:"incomplete_early_selections"`
	BothSkipped                int            `json:"both_skipped"`
	TechnicalFailures          int            `json:"technical_failures"`
	WinsByProvider             map[string]int `json:"wins_by_provider"`
}

type ComparisonsResponse struct {
	Groups []ComparisonGroupSummary `json:"groups"`
	Items  []ComparisonItemDTO      `json:"items"`
}

type Service struct {
	pool      *pgxpool.Pool
	bundle    *config.Bundle
	charSvc   *character.Service
	ritualSvc *ritual.Service
	worker    *ai.Worker
	broker    *ai.SSEBroker
	providers map[string]ai.ChatProvider
}

func NewService(
	pool *pgxpool.Pool,
	bundle *config.Bundle,
	charSvc *character.Service,
	ritualSvc *ritual.Service,
	worker *ai.Worker,
	broker *ai.SSEBroker,
	providers map[string]ai.ChatProvider,
) *Service {
	return &Service{
		pool:      pool,
		bundle:    bundle,
		charSvc:   charSvc,
		ritualSvc: ritualSvc,
		worker:    worker,
		broker:    broker,
		providers: providers,
	}
}

func (s *Service) GetActiveSlots(ctx context.Context, ownerID uuid.UUID) (ActiveSlots, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, stage, character_snapshot, reply_policy, generation_mode, version, updated_at
		FROM conversations
		WHERE owner_id = $1 AND closed_at IS NULL
		ORDER BY created_at DESC
	`, ownerID)
	if err != nil {
		return ActiveSlots{}, apperr.Internal(err)
	}
	defer rows.Close()

	var slots ActiveSlots
	var items []SlotSummary
	for rows.Next() {
		var it SlotSummary
		var snapRaw []byte
		var ver int64
		if err := rows.Scan(
			&it.ConversationID, &it.Kind, &it.Stage, &snapRaw,
			&it.ReplyPolicy, &it.GenerationMode, &ver, &it.UpdatedAt,
		); err != nil {
			return ActiveSlots{}, apperr.Internal(err)
		}
		it.Version = strconv.FormatInt(ver, 10)
		_ = json.Unmarshal(snapRaw, &it.Character)
		items = append(items, it)
	}
	rows.Close()

	for i := range items {
		it := &items[i]
		s.enrichSlotStatus(ctx, ownerID, it)
		cp := *it
		if cp.Kind == "release" {
			slots.Release = &cp
		} else if cp.Kind == "grow" {
			slots.Grow = &cp
		}
	}
	return slots, nil
}

func (s *Service) enrichSlotStatus(ctx context.Context, ownerID uuid.UUID, it *SlotSummary) {
	switch it.Stage {
	case "chat":
		var turnID uuid.UUID
		var mode string
		err := s.pool.QueryRow(ctx, `
			SELECT id, mode FROM conversation_turns
			WHERE conversation_id = $1 AND owner_id = $2 AND resolved_at IS NULL
			LIMIT 1
		`, it.ConversationID, ownerID).Scan(&turnID, &mode)
		if err == nil {
			it.ActiveTurnID = &turnID
			var succCount int
			_ = s.pool.QueryRow(ctx, `
				SELECT COUNT(*) FROM ai_runs
				WHERE turn_id = $1 AND superseded_at IS NULL AND status = 'succeeded'
			`, turnID).Scan(&succCount)
			if mode == "compare" && succCount > 0 {
				it.UIStatus = "awaiting_selection"
				it.UIStatusLabel = "답변 선택"
				return
			}
			it.UIStatus = "generating"
			it.UIStatusLabel = "답변 중"
			return
		}
		it.UIStatus = "chatting"
		it.UIStatusLabel = "대화 중"

	case "release_ritual":
		rv, err := s.ritualSvc.BuildViewForActiveConversation(ctx, ownerID, it.ConversationID)
		if err == nil && rv != nil {
			it.ActiveRitualID = &rv.ID
			if containsStr(rv.AllowedCommands, "complete_ritual") {
				it.UIStatus = "awaiting_complete"
				it.UIStatusLabel = "마무리 대기"
				return
			}
		}
		it.UIStatus = "release_ritual"
		it.UIStatusLabel = "봉투 처리 중"

	case "gift_ready":
		if flw, err := ritual.LoadFlowerByConversation(ctx, s.pool, ownerID, it.ConversationID); err == nil && flw != nil {
			it.FlowerID = &flw.ID
		}
		it.UIStatus = "gift_ready"
		it.UIStatusLabel = "꽃 남기기"

	case "planting":
		if flw, err := ritual.LoadFlowerByConversation(ctx, s.pool, ownerID, it.ConversationID); err == nil && flw != nil {
			it.FlowerID = &flw.ID
		}
		rv, err := s.ritualSvc.BuildViewForActiveConversation(ctx, ownerID, it.ConversationID)
		if err == nil && rv != nil {
			it.ActiveRitualID = &rv.ID
			if containsStr(rv.AllowedCommands, "complete_ritual") {
				it.UIStatus = "awaiting_complete"
				it.UIStatusLabel = "마무리 대기"
				return
			}
		}
		it.UIStatus = "planting"
		it.UIStatusLabel = "꽃 심기"
	}
}

func (s *Service) CreateConversation(ctx context.Context, ownerID uuid.UUID, idem character.IdemMeta, in CreateConversationInput) (ConversationDetail, bool, error) {
	if in.Kind != "release" && in.Kind != "grow" {
		return ConversationDetail{}, false, apperr.Validation("대화 유형은 release 또는 grow여야 해요.", map[string]string{"kind": "release 또는 grow를 선택해 주세요."})
	}
	charID, err := uuid.Parse(strings.TrimSpace(in.CharacterID))
	if err != nil {
		return ConversationDetail{}, false, apperr.Validation("유효한 캐릭터 ID가 필요해요.", map[string]string{"character_id": "올바른 캐릭터를 선택해 주세요."})
	}

	replyPolicy := strings.TrimSpace(in.ReplyPolicy)
	if in.Kind == "release" {
		if replyPolicy == "" {
			replyPolicy = "apology"
		}
		if replyPolicy != "apology" && replyPolicy != "quiet" {
			return ConversationDetail{}, false, apperr.Validation("털어놓기 답장 방식은 apology 또는 quiet여야 해요.", map[string]string{"reply_policy": "허용되지 않는 답장 방식이에요."})
		}
	} else {
		if replyPolicy == "" {
			replyPolicy = "supportive"
		}
		if replyPolicy != "supportive" {
			return ConversationDetail{}, false, apperr.Validation("고민 대화 답장 방식은 supportive여야 해요.", map[string]string{"reply_policy": "허용되지 않는 답장 방식이에요."})
		}
	}

	greetKey, greetText, ok := s.bundle.Avatars.ResolveGreeting(in.Kind, strings.TrimSpace(in.GreetingKey))
	if !ok {
		return ConversationDetail{}, false, apperr.Validation("지원하지 않는 첫 인사 키예요.", map[string]string{"greeting_key": "유효하지 않은 인사 키예요."})
	}

	settings, err := s.GetAISettings(ctx, ownerID)
	if err != nil {
		return ConversationDetail{}, false, err
	}

	genMode := strings.TrimSpace(in.GenerationMode)
	if genMode == "" {
		if in.Kind == "release" {
			genMode = settings.ReleaseMode
		} else {
			genMode = settings.GrowMode
		}
	}
	if genMode != "openai" && genMode != "gemini" && genMode != "compare" {
		return ConversationDetail{}, false, apperr.Validation("AI 모드는 openai, gemini, compare 중 하나여야 해요.", map[string]string{"generation_mode": "허용되지 않는 모드예요."})
	}

	aliases, err := s.resolveAliasesForMode(genMode, in.ModelAliases, settings.ModelAliases)
	if err != nil {
		return ConversationDetail{}, false, err
	}

	type txRes struct {
		convID   uuid.UUID
		replayed bool
	}

	txOut, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (txRes, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return txRes{}, err
		}
		if rec != nil {
			cidStr := rec.ResultIDs["conversation_id"]
			cid, parseErr := uuid.Parse(cidStr)
			if parseErr == nil {
				return txRes{convID: cid, replayed: true}, nil
			}
		}

		// Check if active conversation of same kind already exists (CHAT02)
		var existingID uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT id FROM conversations
			WHERE owner_id = $1 AND kind = $2 AND closed_at IS NULL
			FOR UPDATE
		`, ownerID, in.Kind).Scan(&existingID)
		if err == nil {
			return txRes{}, apperr.New(
				apperr.CodeActiveConversationExists,
				http.StatusConflict,
				"이미 진행 중인 같은 유형의 이야기가 있어요.",
				false,
				map[string]any{"active_conversation_id": existingID.String()},
			)
		}
		if err != pgx.ErrNoRows {
			return txRes{}, err
		}

		// Verify character ownership, role, and non-archived status
		var chKind, chName, chGender, chAppVer string
		var chAge int16
		var chGenderCustom *string
		var chAppRaw []byte
		var chArchived *time.Time
		err = tx.QueryRow(ctx, `
			SELECT kind, name, age, gender, gender_custom, appearance_json, appearance_version, archived_at
			FROM characters
			WHERE id = $1 AND owner_id = $2
		`, charID, ownerID).Scan(&chKind, &chName, &chAge, &chGender, &chGenderCustom, &chAppRaw, &chAppVer, &chArchived)
		if err == pgx.ErrNoRows {
			return txRes{}, apperr.NotFound("선택한 캐릭터를 찾을 수 없어요.")
		}
		if err != nil {
			return txRes{}, err
		}
		if chArchived != nil {
			return txRes{}, apperr.Validation("보관된 캐릭터로는 새 대화를 시작할 수 없어요.", nil)
		}
		if chKind != in.Kind {
			return txRes{}, apperr.Validation("캐릭터의 역할과 대화 유형이 일치하지 않아요.", nil)
		}

		var app character.Appearance
		_ = json.Unmarshal(chAppRaw, &app)
		snap := character.Snapshot{
			ID:                charID,
			Kind:              chKind,
			Name:              chName,
			Age:               chAge,
			Gender:            chGender,
			GenderCustom:      chGenderCustom,
			Appearance:        app,
			AppearanceVersion: chAppVer,
		}
		snapJSON, _ := json.Marshal(snap)
		aliasesJSON, _ := json.Marshal(aliases)

		convID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversations (
				id, owner_id, kind, character_id, character_snapshot, greeting_key,
				stage, reply_policy, generation_mode, model_aliases,
				next_message_seq, next_turn_no, version, content_epoch
			) VALUES ($1, $2, $3, $4, $5, $6, 'chat', $7, $8, $9, 2, 1, 1, 1)
		`, convID, ownerID, in.Kind, charID, snapJSON, greetKey, replyPolicy, genMode, aliasesJSON); err != nil {
			if storage.IsUniqueConstraint(err, "uq_open_conversation") {
				var activeID uuid.UUID
				_ = tx.QueryRow(ctx, `SELECT id FROM conversations WHERE owner_id = $1 AND kind = $2 AND closed_at IS NULL`, ownerID, in.Kind).Scan(&activeID)
				return txRes{}, apperr.New(
					apperr.CodeActiveConversationExists,
					http.StatusConflict,
					"이미 진행 중인 같은 유형의 이야기가 있어요.",
					false,
					map[string]any{"active_conversation_id": activeID.String()},
				)
			}
			return txRes{}, err
		}

		// Fixed opening message (CHAT03: stored once, no AI call)
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_messages (
				id, owner_id, conversation_id, turn_id, seq, role, origin, content
			) VALUES ($1, $2, $3, NULL, 1, 'assistant', 'opening', $4)
		`, uuid.New(), ownerID, convID, greetText); err != nil {
			return txRes{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_drafts (conversation_id, owner_id, content, draft_version, content_epoch)
			VALUES ($1, $2, '', 1, 1)
		`, convID, ownerID); err != nil {
			return txRes{}, err
		}

		// Remember recent mode per kind (Spec Section 11.1)
		if in.Kind == "release" {
			_, _ = tx.Exec(ctx, `UPDATE user_settings SET release_mode = $2, updated_at = now() WHERE owner_id = $1`, ownerID, genMode)
		} else {
			_, _ = tx.Exec(ctx, `UPDATE user_settings SET grow_mode = $2, updated_at = now() WHERE owner_id = $1`, ownerID, genMode)
		}

		placeholderResp, _ := json.Marshal(map[string]string{"conversation_id": convID.String()})
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusCreated, placeholderResp, map[string]string{"conversation_id": convID.String()}, s.bundle.Env.IdempotencyRetention,
		); err != nil {
			return txRes{}, err
		}
		return txRes{convID: convID}, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ConversationDetail{}, false, err
		}
		return ConversationDetail{}, false, apperr.Internal(err)
	}

	detail, err := s.GetConversation(ctx, ownerID, txOut.convID)
	return detail, txOut.replayed, err
}

func (s *Service) GetConversation(ctx context.Context, ownerID, convID uuid.UUID) (ConversationDetail, error) {
	var d ConversationDetail
	var snapRaw, aliasesRaw []byte
	var ver, epoch int64
	var closedAt, purgedAt *time.Time
	var closeReason *string

	err := s.pool.QueryRow(ctx, `
		SELECT id, kind, stage, character_snapshot, greeting_key, reply_policy,
		       generation_mode, model_aliases, version, content_epoch,
		       closed_at, close_reason, purged_at, created_at, updated_at
		FROM conversations
		WHERE id = $1 AND owner_id = $2
	`, convID, ownerID).Scan(
		&d.ID, &d.Kind, &d.Stage, &snapRaw, &d.GreetingKey, &d.ReplyPolicy,
		&d.GenerationMode, &aliasesRaw, &ver, &epoch,
		&closedAt, &closeReason, &purgedAt, &d.CreatedAt, &d.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return ConversationDetail{}, apperr.NotFound("대화를 찾을 수 없어요.")
	}
	if err != nil {
		return ConversationDetail{}, apperr.Internal(err)
	}

	// DATA04 & Section 5: Completed/purged conversation returns 410 CONTENT_PURGED with safe summary details
	if purgedAt != nil || d.Stage == "completed" {
		details := map[string]any{
			"conversation_id": d.ID.String(),
			"kind":            d.Kind,
			"stage":           "completed",
		}
		if closeReason != nil {
			details["close_reason"] = *closeReason
		}
		if closedAt != nil {
			details["closed_at"] = closedAt.Format(time.RFC3339)
		}
		return ConversationDetail{}, apperr.New(
			apperr.CodeContentPurged,
			http.StatusGone,
			"이미 비우거나 심기를 마쳐 원문이 정리된 이야기예요.",
			false,
			details,
		)
	}

	_ = json.Unmarshal(snapRaw, &d.Character)
	_ = json.Unmarshal(aliasesRaw, &d.ModelAliases)
	d.Version = strconv.FormatInt(ver, 10)
	d.ContentEpoch = strconv.FormatInt(epoch, 10)

	mRows, err := s.pool.Query(ctx, `
		SELECT id, turn_id, seq, role, origin, content, selected_run_id, created_at
		FROM conversation_messages
		WHERE conversation_id = $1 AND owner_id = $2
		ORDER BY seq ASC
	`, convID, ownerID)
	if err != nil {
		return ConversationDetail{}, apperr.Internal(err)
	}
	d.Messages = []MessageDTO{}
	for mRows.Next() {
		var m MessageDTO
		if err := mRows.Scan(&m.ID, &m.TurnID, &m.Seq, &m.Role, &m.Origin, &m.Content, &m.SelectedRunID, &m.CreatedAt); err != nil {
			mRows.Close()
			return ConversationDetail{}, apperr.Internal(err)
		}
		d.Messages = append(d.Messages, m)
	}
	mRows.Close()

	// Compute character expression state (Spec Section 6.3: challenging, apologetic, listening)
	if d.Kind == "grow" {
		d.ExpressionState = "listening"
	} else {
		hasUserOrReply := len(d.Messages) > 1
		if !hasUserOrReply {
			d.ExpressionState = "challenging"
		} else if d.ReplyPolicy == "quiet" {
			d.ExpressionState = "listening"
		} else {
			d.ExpressionState = "apologetic"
		}
	}

	var drVer, drEpoch int64
	err = s.pool.QueryRow(ctx, `
		SELECT content, draft_version, content_epoch, updated_at
		FROM conversation_drafts
		WHERE conversation_id = $1 AND owner_id = $2
	`, convID, ownerID).Scan(&d.Draft.Text, &drVer, &drEpoch, &d.Draft.UpdatedAt)
	if err == nil {
		d.Draft.DraftVersion = strconv.FormatInt(drVer, 10)
		d.Draft.ContentEpoch = strconv.FormatInt(drEpoch, 10)
	} else {
		d.Draft = DraftDTO{DraftVersion: "1", ContentEpoch: d.ContentEpoch, UpdatedAt: d.UpdatedAt}
	}

	var latestTurnID uuid.UUID
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM conversation_turns
		WHERE conversation_id = $1 AND owner_id = $2
		ORDER BY turn_no DESC
		LIMIT 1
	`, convID, ownerID).Scan(&latestTurnID)
	if err == nil {
		if ts, tErr := s.GetTurnSnapshot(ctx, ownerID, latestTurnID); tErr == nil {
			d.LatestTurn = &ts
		}
	}

	if d.Stage == "release_ritual" || d.Stage == "planting" {
		if rv, rErr := s.ritualSvc.BuildViewForActiveConversation(ctx, ownerID, convID); rErr == nil {
			d.ActiveRitual = rv
		}
	}
	if d.Stage == "gift_ready" || d.Stage == "planting" {
		if flw, fErr := ritual.LoadFlowerByConversation(ctx, s.pool, ownerID, convID); fErr == nil {
			d.FlowerGift = flw
		}
	}
	return d, nil
}

func (s *Service) UpdatePreferences(ctx context.Context, ownerID, convID uuid.UUID, idem character.IdemMeta, in UpdatePreferencesInput) (ConversationDetail, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return ConversationDetail{}, err
	}

	settings, err := s.GetAISettings(ctx, ownerID)
	if err != nil {
		return ConversationDetail{}, err
	}

	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var kind, stage, curPolicy, curMode string
		var curAliasesRaw []byte
		var curVer int64
		var purgedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT kind, stage, reply_policy, generation_mode, model_aliases, version, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&kind, &stage, &curPolicy, &curMode, &curAliasesRaw, &curVer, &purgedAt)
		if err == pgx.ErrNoRows {
			return false, apperr.NotFound("대화를 찾을 수 없어요.")
		}
		if err != nil {
			return false, err
		}
		if purgedAt != nil || stage == "completed" {
			return false, apperr.ContentPurged()
		}
		if curVer != expVer {
			return false, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}

		nextPolicy := curPolicy
		if in.ReplyPolicy != nil && *in.ReplyPolicy != curPolicy {
			// Spec Section 8.2: Cannot switch reply_policy while an AI run is currently generating
			var activeRuns int
			_ = tx.QueryRow(ctx, `
				SELECT COUNT(*) FROM ai_runs r
				JOIN conversation_turns t ON t.id = r.turn_id
				WHERE r.conversation_id = $1 AND t.resolved_at IS NULL
				  AND r.superseded_at IS NULL AND r.status IN ('queued', 'running', 'streaming')
			`, convID).Scan(&activeRuns)
			if activeRuns > 0 {
				return false, apperr.New(apperr.CodeTurnUnresolved, http.StatusConflict, "답변 생성 중에는 먼저 답변을 멈춘 뒤 답장 방식을 바꿀 수 있어요.", false, nil)
			}

			p := strings.TrimSpace(*in.ReplyPolicy)
			if (kind == "release" && (p == "apology" || p == "quiet")) || (kind == "grow" && p == "supportive") {
				nextPolicy = p
			} else {
				return false, apperr.Validation("대화 유형에 맞지 않는 답장 방식이에요.", nil)
			}
		}

		nextMode := curMode
		if in.GenerationMode != nil {
			m := strings.TrimSpace(*in.GenerationMode)
			if m != "openai" && m != "gemini" && m != "compare" {
				return false, apperr.Validation("허용되지 않는 AI 모드예요.", nil)
			}
			nextMode = m
		}

		var curAliases []string
		_ = json.Unmarshal(curAliasesRaw, &curAliases)
		reqAliases := in.ModelAliases
		if len(reqAliases) == 0 && nextMode == curMode {
			reqAliases = curAliases
		}
		nextAliases, err := s.resolveAliasesForMode(nextMode, reqAliases, settings.ModelAliases)
		if err != nil {
			return false, err
		}
		aliasesJSON, _ := json.Marshal(nextAliases)

		newVer := curVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET reply_policy = $2,
			    generation_mode = $3,
			    model_aliases = $4,
			    version = $5,
			    updated_at = now()
			WHERE id = $1
		`, convID, nextPolicy, nextMode, aliasesJSON, newVer); err != nil {
			return false, err
		}

		if kind == "release" {
			_, _ = tx.Exec(ctx, `UPDATE user_settings SET release_mode = $2, updated_at = now() WHERE owner_id = $1`, ownerID, nextMode)
		} else {
			_, _ = tx.Exec(ctx, `UPDATE user_settings SET grow_mode = $2, updated_at = now() WHERE owner_id = $1`, ownerID, nextMode)
		}

		respBytes, _ := json.Marshal(map[string]string{"version": strconv.FormatInt(newVer, 10)})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ConversationDetail{}, err
		}
		return ConversationDetail{}, apperr.Internal(err)
	}
	return s.GetConversation(ctx, ownerID, convID)
}

func (s *Service) SaveDraft(ctx context.Context, ownerID, convID uuid.UUID, in SaveDraftInput) (DraftDTO, error) {
	expDraftVer, err := parsePositiveInt64(in.DraftVersion, "draft_version")
	if err != nil {
		return DraftDTO{}, err
	}
	capEpoch, err := parsePositiveInt64(in.CapturedEpoch, "captured_epoch")
	if err != nil {
		return DraftDTO{}, err
	}
	if utf8.RuneCountInString(in.Text) > 4000 {
		return DraftDTO{}, apperr.Validation("초안은 4,000자 이내여야 해요.", map[string]string{"text": "4,000자 이하로 입력해 주세요."})
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (DraftDTO, error) {
		var stage string
		var convEpoch int64
		var purgedAt *time.Time
		err := tx.QueryRow(ctx, `
			SELECT stage, content_epoch, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&stage, &convEpoch, &purgedAt)
		if err == pgx.ErrNoRows {
			return DraftDTO{}, apperr.NotFound("대화를 찾을 수 없어요.")
		}
		if err != nil {
			return DraftDTO{}, err
		}
		if purgedAt != nil || stage == "completed" {
			return DraftDTO{}, apperr.ContentPurged()
		}
		if stage != "chat" {
			return DraftDTO{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "마무리 단계에서는 초안을 저장할 수 없어요.", false, nil)
		}
		if capEpoch != convEpoch {
			return DraftDTO{}, apperr.VersionConflict(strconv.FormatInt(convEpoch, 10))
		}

		var curText string
		var curVer int64
		var updatedAt time.Time
		err = tx.QueryRow(ctx, `
			SELECT content, draft_version, updated_at
			FROM conversation_drafts
			WHERE conversation_id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&curText, &curVer, &updatedAt)
		if err != nil {
			return DraftDTO{}, err
		}

		// Idempotent resend of the exact already-saved text
		if expDraftVer == curVer-1 && curText == in.Text {
			return DraftDTO{
				Text:         curText,
				DraftVersion: strconv.FormatInt(curVer, 10),
				ContentEpoch: strconv.FormatInt(convEpoch, 10),
				UpdatedAt:    updatedAt,
			}, nil
		}
		// Reject stale autosaves invalidated by turn submission or another tab (CHAT09)
		if expDraftVer != curVer {
			return DraftDTO{}, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}

		newVer := curVer + 1
		var newUpdated time.Time
		if err := tx.QueryRow(ctx, `
			UPDATE conversation_drafts
			SET content = $3,
			    draft_version = $4,
			    content_epoch = $5,
			    updated_at = now()
			WHERE conversation_id = $1 AND owner_id = $2
			RETURNING updated_at
		`, convID, ownerID, in.Text, newVer, convEpoch).Scan(&newUpdated); err != nil {
			return DraftDTO{}, err
		}

		return DraftDTO{
			Text:         in.Text,
			DraftVersion: strconv.FormatInt(newVer, 10),
			ContentEpoch: strconv.FormatInt(convEpoch, 10),
			UpdatedAt:    newUpdated,
		}, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return DraftDTO{}, err
		}
		return DraftDTO{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) SubmitTurn(ctx context.Context, ownerID, convID uuid.UUID, idem character.IdemMeta, in SubmitTurnInput) (SubmitTurnResult, int, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return SubmitTurnResult{}, 0, err
	}
	clientTurnID := strings.TrimSpace(in.ClientTurnID)
	if clientTurnID == "" || len(clientTurnID) > 128 {
		return SubmitTurnResult{}, 0, apperr.Validation("유효한 client_turn_id가 필요해요.", map[string]string{"client_turn_id": "1~128자 식별자가 필요해요."})
	}
	trimmed := strings.TrimSpace(in.Text)
	runeLen := utf8.RuneCountInString(in.Text)
	if trimmed == "" || runeLen < 1 || runeLen > 4000 {
		return SubmitTurnResult{}, 0, apperr.Validation("메시지는 공백만으로 보낼 수 없으며 1~4,000자 이내여야 해요.", map[string]string{"text": "1~4,000자로 입력해 주세요."})
	}

	settings, err := s.GetAISettings(ctx, ownerID)
	if err != nil {
		return SubmitTurnResult{}, 0, err
	}

	type txOut struct {
		result     SubmitTurnResult
		httpStatus int
		queued     bool
	}

	out, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (txOut, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return txOut{}, err
		}
		if rec != nil {
			var replay SubmitTurnResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return txOut{}, err
			}
			return txOut{result: replay, httpStatus: rec.StatusCode}, nil
		}

		// Canonical lock order: conversation -> draft -> turns (Spec Section 21.1-21.2)
		var kind, stage, replyPolicy, convGenMode string
		var snapRaw, convAliasesRaw []byte
		var nextMsgSeq, nextTurnNo int
		var curVer, convEpoch int64
		var purgedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT kind, stage, character_snapshot, reply_policy, generation_mode, model_aliases,
			       next_message_seq, next_turn_no, version, content_epoch, purged_at
			FROM conversations
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(
			&kind, &stage, &snapRaw, &replyPolicy, &convGenMode, &convAliasesRaw,
			&nextMsgSeq, &nextTurnNo, &curVer, &convEpoch, &purgedAt,
		)
		if err == pgx.ErrNoRows {
			return txOut{}, apperr.NotFound("대화를 찾을 수 없어요.")
		}
		if err != nil {
			return txOut{}, err
		}
		if purgedAt != nil || stage == "completed" {
			return txOut{}, apperr.ContentPurged()
		}
		if stage != "chat" {
			return txOut{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "현재 대화 단계에서는 새 메시지를 보낼 수 없어요.", false, nil)
		}

		// Check if client_turn_id was already accepted (CHAT06)
		var existingTurnID uuid.UUID
		var existingMode string
		var existingTurnVer int64
		var existingResolvedAt *time.Time
		err = tx.QueryRow(ctx, `
			SELECT id, mode, version, resolved_at FROM conversation_turns
			WHERE conversation_id = $1 AND client_turn_id = $2
		`, convID, clientTurnID).Scan(&existingTurnID, &existingMode, &existingTurnVer, &existingResolvedAt)
		if err == nil {
			var curDraftVer int64
			_ = tx.QueryRow(ctx, `SELECT draft_version FROM conversation_drafts WHERE conversation_id = $1`, convID).Scan(&curDraftVer)
			st := "generating"
			code := http.StatusAccepted
			if existingResolvedAt != nil {
				st = "resolved"
				if existingMode == "quiet" {
					code = http.StatusCreated
				}
			}
			var sRuns []SubmittedRun
			rRows, _ := tx.Query(ctx, `SELECT id, slot, is_mock FROM ai_runs WHERE turn_id = $1 AND superseded_at IS NULL ORDER BY slot ASC`, existingTurnID)
			for rRows.Next() {
				var sr SubmittedRun
				_ = rRows.Scan(&sr.ID, &sr.Slot, &sr.IsMock)
				sRuns = append(sRuns, sr)
			}
			rRows.Close()
			res := SubmitTurnResult{
				TurnID:              existingTurnID,
				ConversationVersion: strconv.FormatInt(curVer, 10),
				DraftVersion:        strconv.FormatInt(curDraftVer, 10),
				TurnVersion:         strconv.FormatInt(existingTurnVer, 10),
				State:               st,
				Runs:                sRuns,
			}
			if existingMode != "quiet" {
				res.EventsURL = "/api/v1/turns/" + existingTurnID.String() + "/events"
			}
			return txOut{result: res, httpStatus: code}, nil
		}

		if curVer != expVer {
			return txOut{}, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}

		// Verify no unresolved turn exists
		var unresolvedID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM conversation_turns
			WHERE conversation_id = $1 AND resolved_at IS NULL
			LIMIT 1
		`, convID).Scan(&unresolvedID); err == nil {
			return txOut{}, apperr.New(
				apperr.CodeTurnUnresolved,
				http.StatusConflict,
				"이전 답변이 끝나거나 선택된 뒤에 다음 메시지를 보낼 수 있어요.",
				false,
				map[string]any{"turn_id": unresolvedID.String()},
			)
		}

		// Lock draft row
		var curDraftVer int64
		if err := tx.QueryRow(ctx, `
			SELECT draft_version FROM conversation_drafts
			WHERE conversation_id = $1 AND owner_id = $2
			FOR UPDATE
		`, convID, ownerID).Scan(&curDraftVer); err != nil {
			return txOut{}, err
		}

		// Load canonical history to build and validate portable_transcript_v1 BEFORE inserting (CHAT08)
		var charSnap character.Snapshot
		_ = json.Unmarshal(snapRaw, &charSnap)

		mRows, err := tx.Query(ctx, `
			SELECT role, content FROM conversation_messages
			WHERE conversation_id = $1 AND owner_id = $2
			ORDER BY seq ASC
		`, convID, ownerID)
		if err != nil {
			return txOut{}, err
		}
		var history []ai.TranscriptHistoryItem
		existingUserTurns := 0
		for mRows.Next() {
			var rRole, rContent string
			if err := mRows.Scan(&rRole, &rContent); err != nil {
				mRows.Close()
				return txOut{}, err
			}
			speaker := "character"
			if rRole == "user" {
				speaker = "user"
				existingUserTurns++
			}
			history = append(history, ai.TranscriptHistoryItem{
				Speaker: speaker,
				Text:    rContent,
			})
		}
		mRows.Close()

		builtCtx, err := ai.BuildPortableContext(
			kind,
			charSnap.Name,
			history,
			in.Text,
			existingUserTurns,
			s.bundle.Env.ConversationMaxTurns,
			s.bundle.Env.ContentContextMaxBytes,
		)
		if err != nil {
			return txOut{}, err
		}

		promptHash := s.bundle.Prompts.GrowHash
		if kind == "release" {
			promptHash = s.bundle.Prompts.ReleaseHash
		}

		// Quiet mode: zero AI calls, resolve turn immediately (Spec Section 8.2, CHAT04)
		if kind == "release" && replyPolicy == "quiet" {
			turnID := uuid.New()
			msgID := uuid.New()
			newConvVer := curVer + 1
			newDraftVer := curDraftVer + 1

			if _, err := tx.Exec(ctx, `
				INSERT INTO conversation_turns (
					id, owner_id, conversation_id, turn_no, client_turn_id, mode,
					slot_order, context_snapshot, context_hash, prompt_version, prompt_hash,
					resolution, resolved_at, version, content_epoch, next_event_seq
				) VALUES ($1, $2, $3, $4, $5, 'quiet', '[]'::jsonb, NULL, $6, $7, $8, 'no_reply', now(), 1, $9, 1)
			`, turnID, ownerID, convID, nextTurnNo, clientTurnID, builtCtx.Hash, s.bundle.Prompts.Version, promptHash, convEpoch); err != nil {
				return txOut{}, err
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO conversation_messages (
					id, owner_id, conversation_id, turn_id, seq, role, origin, content
				) VALUES ($1, $2, $3, $4, $5, 'user', 'user', $6)
			`, msgID, ownerID, convID, turnID, nextMsgSeq, in.Text); err != nil {
				return txOut{}, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE conversation_drafts
				SET content = '', draft_version = $3, updated_at = now()
				WHERE conversation_id = $1 AND owner_id = $2
			`, convID, ownerID, newDraftVer); err != nil {
				return txOut{}, err
			}

			if _, err := tx.Exec(ctx, `
				UPDATE conversations
				SET next_message_seq = next_message_seq + 1,
				    next_turn_no = next_turn_no + 1,
				    version = $2,
				    updated_at = now()
				WHERE id = $1
			`, convID, newConvVer); err != nil {
				return txOut{}, err
			}

			res := SubmitTurnResult{
				TurnID:              turnID,
				ConversationVersion: strconv.FormatInt(newConvVer, 10),
				DraftVersion:        strconv.FormatInt(newDraftVer, 10),
				TurnVersion:         "1",
				State:               "resolved",
				Runs:                []SubmittedRun{},
			}
			respBytes, _ := json.Marshal(res)
			if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusCreated, respBytes, map[string]string{"turn_id": turnID.String()}, s.bundle.Env.IdempotencyRetention); err != nil {
				return txOut{}, err
			}
			return txOut{result: res, httpStatus: http.StatusCreated}, nil
		}

		// Determine active generation mode & aliases for this turn
		mode := strings.TrimSpace(in.GenerationMode)
		if mode == "" {
			mode = convGenMode
		}
		if mode != "openai" && mode != "gemini" && mode != "compare" {
			return txOut{}, apperr.Validation("지원하지 않는 AI 모드예요.", nil)
		}

		var convAliases []string
		_ = json.Unmarshal(convAliasesRaw, &convAliases)
		reqAliases := in.ModelAliases
		if len(reqAliases) == 0 && mode == convGenMode {
			reqAliases = convAliases
		}
		aliases, err := s.resolveAliasesForMode(mode, reqAliases, settings.ModelAliases)
		if err != nil {
			return txOut{}, err
		}

		type plannedSlot struct {
			Slot        string `json:"slot"`
			Alias       string `json:"alias"`
			ProviderKey string `json:"provider_key"`
			Entry       config.ModelEntry `json:"-"`
			Meta        ai.AdapterMetadata `json:"-"`
		}

		var planned []plannedSlot
		if mode == "compare" {
			a0 := aliases[0]
			a1 := aliases[1]
			if settings.BlindCompare && randomCoinFlip() {
				a0, a1 = a1, a0
			}
			p0, e0, m0, err0 := s.resolveProviderTarget(a0)
			if err0 != nil {
				return txOut{}, err0
			}
			p1, e1, m1, err1 := s.resolveProviderTarget(a1)
			if err1 != nil {
				return txOut{}, err1
			}
			planned = []plannedSlot{
				{Slot: "a", Alias: a0, ProviderKey: p0, Entry: e0, Meta: m0},
				{Slot: "b", Alias: a1, ProviderKey: p1, Entry: e1, Meta: m1},
			}
		} else {
			pk, ent, meta, errP := s.resolveProviderTarget(aliases[0])
			if errP != nil {
				return txOut{}, errP
			}
			planned = []plannedSlot{
				{Slot: "primary", Alias: aliases[0], ProviderKey: pk, Entry: ent, Meta: meta},
			}
		}

		// Enforce optional daily budget cap if configured (Spec Section 15)
		if settings.DailyBudgetUSD != nil {
			if capDec, pErr := decimal.NewFromString(*settings.DailyBudgetUSD); pErr == nil && capDec.GreaterThan(decimal.Zero) {
				var spentToday decimal.Decimal
				_ = tx.QueryRow(ctx, `
					SELECT COALESCE(SUM(estimated_cost), 0)
					FROM ai_runs
					WHERE owner_id = $1 AND created_at >= date_trunc('day', now())
				`, ownerID).Scan(&spentToday)
				if !spentToday.LessThan(capDec) {
					return txOut{}, apperr.New(apperr.CodeRateLimited, http.StatusTooManyRequests, "오늘 설정한 일일 AI 예상 비용 한도에 도달했어요.", false, nil)
				}
			}
		}

		slotMetaJSON, _ := json.Marshal(map[string]any{
			"blind": settings.BlindCompare && mode == "compare",
			"slots": planned,
		})

		turnID := uuid.New()
		msgID := uuid.New()
		newConvVer := curVer + 1
		newDraftVer := curDraftVer + 1

		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_turns (
				id, owner_id, conversation_id, turn_no, client_turn_id, mode,
				slot_order, context_snapshot, context_hash, prompt_version, prompt_hash,
				version, content_epoch, next_event_seq
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 1, $12, 1)
		`, turnID, ownerID, convID, nextTurnNo, clientTurnID, mode, slotMetaJSON,
			builtCtx.JSONBytes, builtCtx.Hash, s.bundle.Prompts.Version, promptHash, convEpoch); err != nil {
			return txOut{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_messages (
				id, owner_id, conversation_id, turn_id, seq, role, origin, content
			) VALUES ($1, $2, $3, $4, $5, 'user', 'user', $6)
		`, msgID, ownerID, convID, turnID, nextMsgSeq, in.Text); err != nil {
			return txOut{}, err
		}

		submittedRuns := make([]SubmittedRun, 0, len(planned))
		for _, ps := range planned {
			runID := uuid.New()
			opts := ai.GenerationOptions{
				MaxOutputTokens: ps.Entry.MaxOutputTokens,
				ReasoningLevel:  ps.Entry.DefaultReasoningLevel,
			}
			optsJSON, _ := json.Marshal(opts)
			if _, err := tx.Exec(ctx, `
				INSERT INTO ai_runs (
					id, owner_id, conversation_id, turn_id, slot, attempt_no,
					provider, model_alias, requested_model, is_mock, settings_snapshot,
					prompt_version, prompt_hash, context_hash,
					sdk_version, adapter_version, api_revision,
					status, captured_epoch
				) VALUES (
					$1, $2, $3, $4, $5, 1,
					$6, $7, $8, $9, $10,
					$11, $12, $13,
					$14, $15, $16,
					'queued', $17
				)
			`, runID, ownerID, convID, turnID, ps.Slot,
				ps.ProviderKey, ps.Alias, ps.Entry.ModelID, ps.Meta.IsMock, optsJSON,
				s.bundle.Prompts.Version, promptHash, builtCtx.Hash,
				ps.Meta.SDKVersion, ps.Meta.AdapterVersion, ps.Meta.APIRevision,
				convEpoch); err != nil {
				return txOut{}, err
			}
			submittedRuns = append(submittedRuns, SubmittedRun{
				ID:     runID,
				Slot:   ps.Slot,
				IsMock: ps.Meta.IsMock,
			})
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversation_drafts
			SET content = '', draft_version = $3, updated_at = now()
			WHERE conversation_id = $1 AND owner_id = $2
		`, convID, ownerID, newDraftVer); err != nil {
			return txOut{}, err
		}

		aliasesJSON, _ := json.Marshal(aliases)
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET generation_mode = $2,
			    model_aliases = $3,
			    next_message_seq = next_message_seq + 1,
			    next_turn_no = next_turn_no + 1,
			    version = $4,
			    updated_at = now()
			WHERE id = $1
		`, convID, mode, aliasesJSON, newConvVer); err != nil {
			return txOut{}, err
		}

		eventsURL := "/api/v1/turns/" + turnID.String() + "/events"
		res := SubmitTurnResult{
			TurnID:              turnID,
			ConversationVersion: strconv.FormatInt(newConvVer, 10),
			DraftVersion:        strconv.FormatInt(newDraftVer, 10),
			TurnVersion:         "1",
			State:               "generating",
			Runs:                submittedRuns,
			EventsURL:           eventsURL,
		}
		respBytes, _ := json.Marshal(res)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusAccepted, respBytes, map[string]string{"turn_id": turnID.String()}, s.bundle.Env.IdempotencyRetention); err != nil {
			return txOut{}, err
		}
		return txOut{result: res, httpStatus: http.StatusAccepted, queued: true}, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return SubmitTurnResult{}, 0, err
		}
		return SubmitTurnResult{}, 0, apperr.Internal(err)
	}
	if out.queued {
		s.worker.Trigger()
	}
	return out.result, out.httpStatus, nil
}

func (s *Service) GetTurnSnapshot(ctx context.Context, ownerID, turnID uuid.UUID) (TurnSnapshotDTO, error) {
	return storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (TurnSnapshotDTO, error) {
		var ts TurnSnapshotDTO
		var slotOrderRaw []byte
		var resolution *string
		var ver, nextEventSeq int64
		var purgedAt, convPurgedAt *time.Time

		err := tx.QueryRow(ctx, `
			SELECT t.id, t.conversation_id, t.turn_no, t.client_turn_id, t.mode,
			       t.slot_order, t.selected_run_id, t.resolution, t.resolved_at,
			       t.version, t.next_event_seq, t.purged_at, c.purged_at
			FROM conversation_turns t
			JOIN conversations c ON c.id = t.conversation_id
			WHERE t.id = $1 AND t.owner_id = $2
		`, turnID, ownerID).Scan(
			&ts.ID, &ts.ConversationID, &ts.TurnNo, &ts.ClientTurnID, &ts.Mode,
			&slotOrderRaw, &ts.SelectedRunID, &resolution, &ts.ResolvedAt,
			&ver, &nextEventSeq, &purgedAt, &convPurgedAt,
		)
		if err == pgx.ErrNoRows {
			return TurnSnapshotDTO{}, apperr.NotFound("턴 정보를 찾을 수 없어요.")
		}
		if err != nil {
			return TurnSnapshotDTO{}, apperr.Internal(err)
		}
		if purgedAt != nil || convPurgedAt != nil {
			return TurnSnapshotDTO{}, apperr.ContentPurged()
		}

		if resolution != nil {
			ts.Resolution = *resolution
		}
		ts.Version = strconv.FormatInt(ver, 10)
		lastSeq := nextEventSeq - 1
		if lastSeq < 0 {
			lastSeq = 0
		}
		ts.LastEventSeq = strconv.FormatInt(lastSeq, 10)
		if ts.Mode != "quiet" {
			ts.EventsURL = "/api/v1/turns/" + ts.ID.String() + "/events"
		}

		var slotMeta struct {
			Blind bool `json:"blind"`
		}
		_ = json.Unmarshal(slotOrderRaw, &slotMeta)
		ts.BlindMode = slotMeta.Blind

		var fbExists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM turn_feedbacks WHERE turn_id = $1 AND owner_id = $2)`, turnID, ownerID).Scan(&fbExists)
		ts.HasFeedback = fbExists

		rows, err := tx.Query(ctx, `
			SELECT id, turn_id, slot, attempt_no, provider, model_alias, requested_model,
			       reported_model, is_mock, status, output_text, error_code, finish_reason,
			       queued_at, started_at, first_text_at, finished_at,
			       usage_json, usage_status, price_version, estimated_cost
			FROM ai_runs
			WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
			ORDER BY slot ASC
		`, turnID, ownerID)
		if err != nil {
			return TurnSnapshotDTO{}, apperr.Internal(err)
		}
		defer rows.Close()

		ts.Runs = []RunDTO{}
		anyRunning := false
		anySucceeded := false
		for rows.Next() {
			rd, err := scanRunDTO(rows, ts.ResolvedAt == nil, ts.BlindMode && ts.ResolvedAt == nil)
			if err != nil {
				return TurnSnapshotDTO{}, apperr.Internal(err)
			}
			if rd.Status == ai.StatusQueued || rd.Status == ai.StatusRunning || rd.Status == ai.StatusStreaming {
				anyRunning = true
			}
			if rd.Status == ai.StatusSucceeded {
				anySucceeded = true
			}
			ts.Runs = append(ts.Runs, rd)
		}
		rows.Close()

		if ts.ResolvedAt != nil {
			ts.State = "resolved"
		} else if ts.Mode == "compare" && anySucceeded {
			ts.State = "awaiting_selection"
		} else if anyRunning {
			ts.State = "generating"
		} else {
			ts.State = "failed"
		}
		return ts, nil
	})
}

func (s *Service) ListTurnEventsAfter(ctx context.Context, ownerID, turnID uuid.UUID, afterSeq int64) ([]TurnEventRecord, bool, error) {
	var nextSeq int64
	var resolvedAt, purgedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT t.next_event_seq, t.resolved_at, COALESCE(t.purged_at, c.purged_at)
		FROM conversation_turns t
		JOIN conversations c ON c.id = t.conversation_id
		WHERE t.id = $1 AND t.owner_id = $2
	`, turnID, ownerID).Scan(&nextSeq, &resolvedAt, &purgedAt)
	if err == pgx.ErrNoRows {
		return nil, false, apperr.NotFound("턴 정보를 찾을 수 없어요.")
	}
	if err != nil {
		return nil, false, apperr.Internal(err)
	}
	if purgedAt != nil {
		return nil, false, apperr.ContentPurged()
	}

	// Detect expired cursor when events were cleaned up past retention (Spec Section 14.3)
	if afterSeq > 0 && nextSeq > 1 {
		var minSeq *int64
		_ = s.pool.QueryRow(ctx, `SELECT MIN(seq) FROM ai_run_events WHERE turn_id = $1`, turnID).Scan(&minSeq)
		if minSeq == nil || afterSeq+1 < *minSeq {
			return nil, false, apperr.New(
				apperr.CodeEventCursorExpired,
				http.StatusGone,
				"오래된 스트림 커서예요. 최신 스냅샷으로 복원합니다.",
				true,
				nil,
			)
		}
	}

	rows, err := s.pool.Query(ctx, `
		SELECT seq, event_type, payload
		FROM ai_run_events
		WHERE turn_id = $1 AND owner_id = $2 AND seq > $3
		ORDER BY seq ASC
	`, turnID, ownerID, afterSeq)
	if err != nil {
		return nil, false, apperr.Internal(err)
	}
	defer rows.Close()

	var events []TurnEventRecord
	for rows.Next() {
		var ev TurnEventRecord
		if err := rows.Scan(&ev.Seq, &ev.EventType, &ev.Payload); err != nil {
			return nil, false, apperr.Internal(err)
		}
		events = append(events, ev)
	}
	return events, resolvedAt != nil, nil
}

func (s *Service) SelectCandidate(ctx context.Context, ownerID, turnID uuid.UUID, idem character.IdemMeta, in SelectCandidateInput) (TurnSnapshotDTO, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return TurnSnapshotDTO{}, err
	}
	runID, err := uuid.Parse(strings.TrimSpace(in.RunID))
	if err != nil {
		return TurnSnapshotDTO{}, apperr.Validation("유효한 run_id가 필요해요.", map[string]string{"run_id": "올바른 후보 ID가 필요해요."})
	}

	var peerToCancel []uuid.UUID

	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT conversation_id FROM conversation_turns WHERE id = $1 AND owner_id = $2`, turnID, ownerID).Scan(&convID); err != nil {
			if err == pgx.ErrNoRows {
				return false, apperr.NotFound("턴을 찾을 수 없어요.")
			}
			return false, err
		}

		// Canonical lock: conversation -> turn -> runs
		var nextMsgSeq int
		var convVer int64
		var purgedAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT next_message_seq, version, purged_at
			FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, convID, ownerID).Scan(&nextMsgSeq, &convVer, &purgedAt); err != nil {
			return false, err
		}
		if purgedAt != nil {
			return false, apperr.ContentPurged()
		}

		var selectedRunID *uuid.UUID
		var resolvedAt *time.Time
		var turnVer, nextEventSeq int64
		if err := tx.QueryRow(ctx, `
			SELECT selected_run_id, resolved_at, version, next_event_seq
			FROM conversation_turns WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, turnID, ownerID).Scan(&selectedRunID, &resolvedAt, &turnVer, &nextEventSeq); err != nil {
			return false, err
		}

		// Idempotent repeat selection of same run
		if resolvedAt != nil {
			if selectedRunID != nil && *selectedRunID == runID {
				return true, nil
			}
			return false, apperr.New(
				apperr.CodeAlreadySelected,
				http.StatusConflict,
				"이미 다른 답변이 선택되었어요.",
				false,
				map[string]any{"selected_run_id": selectedRunID},
			)
		}

		if turnVer != expVer {
			return false, apperr.VersionConflict(strconv.FormatInt(turnVer, 10))
		}

		rows, err := tx.Query(ctx, `
			SELECT id, status, output_text
			FROM ai_runs
			WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
			ORDER BY id ASC
			FOR UPDATE
		`, turnID, ownerID)
		if err != nil {
			return false, err
		}
		var targetStatus string
		var targetText *string
		found := false
		for rows.Next() {
			var rid uuid.UUID
			var st string
			var txt *string
			if err := rows.Scan(&rid, &st, &txt); err != nil {
				rows.Close()
				return false, err
			}
			if rid == runID {
				found = true
				targetStatus = st
				targetText = txt
			} else if st == ai.StatusQueued || st == ai.StatusRunning || st == ai.StatusStreaming {
				peerToCancel = append(peerToCancel, rid)
			}
		}
		rows.Close()

		if !found {
			return false, apperr.NotFound("선택한 후보 실행을 찾을 수 없어요.")
		}
		if targetStatus != ai.StatusSucceeded || targetText == nil || strings.TrimSpace(*targetText) == "" {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "정상적으로 완료된 답변만 채택할 수 있어요.", false, nil)
		}

		// Cancel any still-running peer candidate (Spec Section 11.2)
		if len(peerToCancel) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = 'cancelled',
				    lease_token = NULL,
				    lease_expires_at = NULL,
				    finished_at = COALESCE(finished_at, now()),
				    updated_at = now()
				WHERE turn_id = $1 AND id <> $2 AND superseded_at IS NULL
				  AND status IN ('queued', 'running', 'streaming')
			`, turnID, runID); err != nil {
				return false, err
			}
		}

		// Copy ONLY chosen run's output_text into 1 canonical assistant message (AI03)
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversation_messages (
				id, owner_id, conversation_id, turn_id, seq, role, origin, content, selected_run_id
			) VALUES ($1, $2, $3, $4, $5, 'assistant', 'ai_selected', $6, $7)
		`, uuid.New(), ownerID, convID, turnID, nextMsgSeq, *targetText, runID); err != nil {
			return false, err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversation_turns
			SET selected_run_id = $2,
			    resolution = 'user_selected',
			    resolved_at = now(),
			    version = version + 1,
			    next_event_seq = $3,
			    updated_at = now()
			WHERE id = $1
		`, turnID, runID, nextEventSeq+1); err != nil {
			return false, err
		}

		evPayload, _ := json.Marshal(map[string]any{
			"turn_id":         turnID.String(),
			"resolution":      "user_selected",
			"selected_run_id": runID.String(),
		})
		if _, err := tx.Exec(ctx, `
			INSERT INTO ai_run_events (turn_id, seq, owner_id, conversation_id, run_id, event_type, payload)
			VALUES ($1, $2, $3, $4, $5, 'turn.resolved', $6)
		`, turnID, nextEventSeq, ownerID, convID, runID, evPayload); err != nil {
			return false, err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET next_message_seq = next_message_seq + 1,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1
		`, convID); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]string{"selected_run_id": runID.String()})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return TurnSnapshotDTO{}, err
		}
		return TurnSnapshotDTO{}, apperr.Internal(err)
	}

	for _, pid := range peerToCancel {
		s.broker.CancelRun(pid)
	}
	s.broker.Notify(turnID)
	return s.GetTurnSnapshot(ctx, ownerID, turnID)
}

func (s *Service) SkipTurn(ctx context.Context, ownerID, turnID uuid.UUID, idem character.IdemMeta, in SkipTurnInput) (TurnSnapshotDTO, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return TurnSnapshotDTO{}, err
	}

	var cancelledIDs []uuid.UUID
	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT conversation_id FROM conversation_turns WHERE id = $1 AND owner_id = $2`, turnID, ownerID).Scan(&convID); err != nil {
			if err == pgx.ErrNoRows {
				return false, apperr.NotFound("턴을 찾을 수 없어요.")
			}
			return false, err
		}

		var purgedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT purged_at FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE`, convID, ownerID).Scan(&purgedAt); err != nil {
			return false, err
		}
		if purgedAt != nil {
			return false, apperr.ContentPurged()
		}

		var resolvedAt *time.Time
		var turnVer, nextEventSeq int64
		if err := tx.QueryRow(ctx, `
			SELECT resolved_at, version, next_event_seq
			FROM conversation_turns WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, turnID, ownerID).Scan(&resolvedAt, &turnVer, &nextEventSeq); err != nil {
			return false, err
		}
		if resolvedAt != nil {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 해결된 턴이에요.", false, nil)
		}
		if turnVer != expVer {
			return false, apperr.VersionConflict(strconv.FormatInt(turnVer, 10))
		}

		rows, err := tx.Query(ctx, `
			SELECT id FROM ai_runs
			WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
			  AND status IN ('queued', 'running', 'streaming')
			ORDER BY id ASC FOR UPDATE
		`, turnID, ownerID)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var rid uuid.UUID
			_ = rows.Scan(&rid)
			cancelledIDs = append(cancelledIDs, rid)
		}
		rows.Close()

		if _, err := tx.Exec(ctx, `
			UPDATE ai_runs
			SET status = 'cancelled',
			    lease_token = NULL,
			    lease_expires_at = NULL,
			    finished_at = COALESCE(finished_at, now()),
			    updated_at = now()
			WHERE turn_id = $1 AND superseded_at IS NULL AND status IN ('queued', 'running', 'streaming')
		`, turnID); err != nil {
			return false, err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversation_turns
			SET resolution = 'no_answer',
			    resolved_at = now(),
			    version = version + 1,
			    next_event_seq = $2,
			    updated_at = now()
			WHERE id = $1
		`, turnID, nextEventSeq+1); err != nil {
			return false, err
		}

		evPayload, _ := json.Marshal(map[string]any{"turn_id": turnID.String(), "resolution": "no_answer"})
		if _, err := tx.Exec(ctx, `
			INSERT INTO ai_run_events (turn_id, seq, owner_id, conversation_id, event_type, payload)
			VALUES ($1, $2, $3, $4, 'turn.resolved', $5)
		`, turnID, nextEventSeq, ownerID, convID, evPayload); err != nil {
			return false, err
		}

		if _, err := tx.Exec(ctx, `UPDATE conversations SET version = version + 1, updated_at = now() WHERE id = $1`, convID); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]string{"resolution": "no_answer"})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return TurnSnapshotDTO{}, err
		}
		return TurnSnapshotDTO{}, apperr.Internal(err)
	}

	for _, rid := range cancelledIDs {
		s.broker.CancelRun(rid)
	}
	s.broker.Notify(turnID)
	return s.GetTurnSnapshot(ctx, ownerID, turnID)
}

func (s *Service) CancelRun(ctx context.Context, ownerID, runID uuid.UUID, idem character.IdemMeta, in RunActionInput) (TurnSnapshotDTO, error) {
	expTurnVer, err := parsePositiveInt64(in.ExpectedTurnVersion, "expected_turn_version")
	if err != nil {
		return TurnSnapshotDTO{}, err
	}

	var turnID uuid.UUID
	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if err := tx.QueryRow(ctx, `SELECT turn_id FROM ai_runs WHERE id = $1 AND owner_id = $2`, runID, ownerID).Scan(&turnID); err != nil {
			if err == pgx.ErrNoRows {
				return false, apperr.NotFound("실행 정보를 찾을 수 없어요.")
			}
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var convID uuid.UUID
		var resolvedAt *time.Time
		var turnVer, nextSeq int64
		if err := tx.QueryRow(ctx, `
			SELECT conversation_id, resolved_at, version, next_event_seq
			FROM conversation_turns WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, turnID, ownerID).Scan(&convID, &resolvedAt, &turnVer, &nextSeq); err != nil {
			return false, err
		}
		if resolvedAt != nil {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 확정된 턴이에요.", false, nil)
		}
		if turnVer != expTurnVer {
			return false, apperr.VersionConflict(strconv.FormatInt(turnVer, 10))
		}

		var slot, status string
		var supersededAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT slot, status, superseded_at FROM ai_runs WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, runID, ownerID).Scan(&slot, &status, &supersededAt); err != nil {
			return false, err
		}
		if supersededAt != nil {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 대체된 실행이에요.", false, nil)
		}

		if status == ai.StatusQueued || status == ai.StatusRunning || status == ai.StatusStreaming {
			if _, err := tx.Exec(ctx, `
				UPDATE ai_runs
				SET status = 'cancelled',
				    lease_token = NULL,
				    lease_expires_at = NULL,
				    finished_at = now(),
				    updated_at = now()
				WHERE id = $1
			`, runID); err != nil {
				return false, err
			}
			evPayload, _ := json.Marshal(map[string]any{"run_id": runID.String(), "slot": slot, "status": "cancelled"})
			if _, err := tx.Exec(ctx, `
				INSERT INTO ai_run_events (turn_id, seq, owner_id, conversation_id, run_id, event_type, payload)
				VALUES ($1, $2, $3, $4, $5, 'run.cancelled', $6)
			`, turnID, nextSeq, ownerID, convID, runID, evPayload); err != nil {
				return false, err
			}
			nextSeq++
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversation_turns SET version = version + 1, next_event_seq = $2, updated_at = now() WHERE id = $1
		`, turnID, nextSeq); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]string{"run_id": runID.String()})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return TurnSnapshotDTO{}, err
		}
		return TurnSnapshotDTO{}, apperr.Internal(err)
	}
	s.broker.CancelRun(runID)
	s.broker.Notify(turnID)
	return s.GetTurnSnapshot(ctx, ownerID, turnID)
}

func (s *Service) RetryRun(ctx context.Context, ownerID, runID uuid.UUID, idem character.IdemMeta, in RunActionInput) (TurnSnapshotDTO, error) {
	expTurnVer, err := parsePositiveInt64(in.ExpectedTurnVersion, "expected_turn_version")
	if err != nil {
		return TurnSnapshotDTO{}, err
	}

	var turnID uuid.UUID
	queuedNew := false
	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if err := tx.QueryRow(ctx, `SELECT turn_id FROM ai_runs WHERE id = $1 AND owner_id = $2`, runID, ownerID).Scan(&turnID); err != nil {
			if err == pgx.ErrNoRows {
				return false, apperr.NotFound("실행 정보를 찾을 수 없어요.")
			}
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT conversation_id FROM conversation_turns WHERE id = $1 AND owner_id = $2`, turnID, ownerID).Scan(&convID); err != nil {
			return false, err
		}

		var convEpoch int64
		var purgedAt *time.Time
		if err := tx.QueryRow(ctx, `SELECT content_epoch, purged_at FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE`, convID, ownerID).Scan(&convEpoch, &purgedAt); err != nil {
			return false, err
		}
		if purgedAt != nil {
			return false, apperr.ContentPurged()
		}

		var resolvedAt *time.Time
		var turnVer, nextSeq int64
		if err := tx.QueryRow(ctx, `
			SELECT resolved_at, version, next_event_seq
			FROM conversation_turns WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, turnID, ownerID).Scan(&resolvedAt, &turnVer, &nextSeq); err != nil {
			return false, err
		}
		if resolvedAt != nil {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 해결된 턴이에요.", false, nil)
		}
		if turnVer != expTurnVer {
			return false, apperr.VersionConflict(strconv.FormatInt(turnVer, 10))
		}

		var slot, provider, alias, reqModel, promptVer, promptHash, ctxHash, sdkVer, adapterVer, apiRev, status string
		var attemptNo int
		var isMock bool
		var settingsRaw []byte
		var supersededAt *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT slot, attempt_no, provider, model_alias, requested_model, is_mock,
			       settings_snapshot, prompt_version, prompt_hash, context_hash,
			       sdk_version, adapter_version, api_revision, status, superseded_at
			FROM ai_runs WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, runID, ownerID).Scan(
			&slot, &attemptNo, &provider, &alias, &reqModel, &isMock,
			&settingsRaw, &promptVer, &promptHash, &ctxHash,
			&sdkVer, &adapterVer, &apiRev, &status, &supersededAt,
		); err != nil {
			return false, err
		}
		if supersededAt != nil {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 재시도된 실행이에요.", false, nil)
		}
		// Spec Section 14.5: Only retry failed/incomplete/interrupted/cancelled runs; keep original run row intact
		if status != ai.StatusFailed && status != ai.StatusIncomplete && status != ai.StatusInterrupted && status != ai.StatusCancelled {
			return false, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "실패하거나 중지된 후보만 재시도할 수 있어요.", false, nil)
		}

		if _, err := tx.Exec(ctx, `UPDATE ai_runs SET superseded_at = now(), updated_at = now() WHERE id = $1`, runID); err != nil {
			return false, err
		}

		// Re-resolve whether live API key is now present or mock should run, keeping model_alias & prompt fixed
		pk, _, meta, errP := s.resolveProviderTarget(alias)
		if errP != nil {
			return false, errP
		}

		newRunID := uuid.New()
		if _, err := tx.Exec(ctx, `
			INSERT INTO ai_runs (
				id, owner_id, conversation_id, turn_id, slot, attempt_no,
				provider, model_alias, requested_model, is_mock, settings_snapshot,
				prompt_version, prompt_hash, context_hash,
				sdk_version, adapter_version, api_revision, status, captured_epoch
			) VALUES (
				$1, $2, $3, $4, $5, $6,
				$7, $8, $9, $10, $11,
				$12, $13, $14,
				$15, $16, $17, 'queued', $18
			)
		`, newRunID, ownerID, convID, turnID, slot, attemptNo+1,
			pk, alias, reqModel, meta.IsMock, settingsRaw,
			promptVer, promptHash, ctxHash,
			meta.SDKVersion, meta.AdapterVersion, meta.APIRevision, convEpoch); err != nil {
			return false, err
		}

		evPayload, _ := json.Marshal(map[string]any{"run_id": newRunID.String(), "slot": slot, "attempt_no": attemptNo + 1})
		if _, err := tx.Exec(ctx, `
			INSERT INTO ai_run_events (turn_id, seq, owner_id, conversation_id, run_id, event_type, payload)
			VALUES ($1, $2, $3, $4, $5, 'run.retried', $6)
		`, turnID, nextSeq, ownerID, convID, newRunID, evPayload); err != nil {
			return false, err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE conversation_turns SET version = version + 1, next_event_seq = $2, updated_at = now() WHERE id = $1
		`, turnID, nextSeq+1); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]string{"new_run_id": newRunID.String()})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		queuedNew = true
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return TurnSnapshotDTO{}, err
		}
		return TurnSnapshotDTO{}, apperr.Internal(err)
	}
	if queuedNew {
		s.worker.Trigger()
	}
	s.broker.Notify(turnID)
	return s.GetTurnSnapshot(ctx, ownerID, turnID)
}

func (s *Service) SubmitFeedback(ctx context.Context, ownerID, turnID uuid.UUID, idem character.IdemMeta, in TurnFeedbackInput) error {
	allowedMetrics := map[string]bool{
		"empathy":             true,
		"context_reflection":  true,
		"apology_specificity": true,
		"unnecessary_advice":  true,
		"repetition":          true,
		"korean_naturalness":  true,
	}
	if len(in.Scores) == 0 {
		return apperr.Validation("평가 점수를 하나 이상 입력해 주세요.", nil)
	}
	for slot, m := range in.Scores {
		if slot != "a" && slot != "b" && slot != "primary" {
			return apperr.Validation("허용되지 않는 후보 슬롯이에요.", nil)
		}
		for metric, score := range m {
			if !allowedMetrics[metric] || score < 1 || score > 5 {
				return apperr.Validation("평가 항목 점수는 1~5 사이의 정수여야 해요.", nil)
			}
		}
	}

	_, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_turns WHERE id = $1 AND owner_id = $2)`, turnID, ownerID).Scan(&exists); err != nil {
			return false, err
		}
		if !exists {
			return false, apperr.NotFound("평가할 턴을 찾을 수 없어요.")
		}

		scoresRaw, _ := json.Marshal(in.Scores)
		if _, err := tx.Exec(ctx, `
			INSERT INTO turn_feedbacks (turn_id, owner_id, scores_json)
			VALUES ($1, $2, $3)
			ON CONFLICT (turn_id) DO UPDATE SET scores_json = EXCLUDED.scores_json, updated_at = now()
		`, turnID, ownerID, scoresRaw); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]bool{"saved": true})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return err
		}
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) GetAISettings(ctx context.Context, ownerID uuid.UUID) (AISettingsView, error) {
	var view AISettingsView
	var aliasesRaw []byte
	var budget *decimal.Decimal
	var ver int64

	err := s.pool.QueryRow(ctx, `
		SELECT release_mode, grow_mode, model_aliases, blind_compare,
		       detailed_eval_enabled, compare_notice_ack, reduced_motion,
		       daily_budget_usd, version
		FROM user_settings
		WHERE owner_id = $1
	`, ownerID).Scan(
		&view.ReleaseMode, &view.GrowMode, &aliasesRaw, &view.BlindCompare,
		&view.DetailedEvalEnabled, &view.CompareNoticeAck, &view.ReducedMotion,
		&budget, &ver,
	)
	if err != nil {
		return AISettingsView{}, apperr.Internal(err)
	}

	view.ModelAliases = map[string]string{"openai": "openai_chat", "gemini": "gemini_chat"}
	_ = json.Unmarshal(aliasesRaw, &view.ModelAliases)
	if budget != nil {
		bs := budget.StringFixed(2)
		view.DailyBudgetUSD = &bs
	}
	view.Version = strconv.FormatInt(ver, 10)
	view.OpenAIKeyConfigured = s.bundle.Env.OpenAIAPIKey != ""
	view.GeminiKeyConfigured = s.bundle.Env.GeminiAPIKey != ""
	view.MockModeEnabled = s.bundle.Env.AllowMockAI

	view.Models = []config.ModelEntry{}
	for _, m := range s.bundle.Models.Models {
		if m.Enabled {
			view.Models = append(view.Models, m)
		}
	}
	return view, nil
}

func (s *Service) UpdateAISettings(ctx context.Context, ownerID uuid.UUID, idem character.IdemMeta, in UpdateAISettingsInput) (AISettingsView, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return AISettingsView{}, err
	}

	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var relMode, growMode string
		var aliasesRaw []byte
		var blind, detailed, ack, reduced bool
		var budget *decimal.Decimal
		var curVer int64

		if err := tx.QueryRow(ctx, `
			SELECT release_mode, grow_mode, model_aliases, blind_compare,
			       detailed_eval_enabled, compare_notice_ack, reduced_motion,
			       daily_budget_usd, version
			FROM user_settings WHERE owner_id = $1 FOR UPDATE
		`, ownerID).Scan(&relMode, &growMode, &aliasesRaw, &blind, &detailed, &ack, &reduced, &budget, &curVer); err != nil {
			return false, err
		}
		if curVer != expVer {
			return false, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}

		if in.ReleaseMode != nil {
			m := strings.TrimSpace(*in.ReleaseMode)
			if m != "openai" && m != "gemini" && m != "compare" {
				return false, apperr.Validation("유효하지 않은 release_mode예요.", nil)
			}
			relMode = m
		}
		if in.GrowMode != nil {
			m := strings.TrimSpace(*in.GrowMode)
			if m != "openai" && m != "gemini" && m != "compare" {
				return false, apperr.Validation("유효하지 않은 grow_mode예요.", nil)
			}
			growMode = m
		}

		aliases := map[string]string{"openai": "openai_chat", "gemini": "gemini_chat"}
		_ = json.Unmarshal(aliasesRaw, &aliases)
		for prov, al := range in.ModelAliases {
			entry, ok := s.bundle.Models.GetByAlias(al)
			if !ok || entry.Provider != prov {
				return false, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "사용할 수 없는 모델 별칭이에요.", false, nil)
			}
			aliases[prov] = al
		}
		if in.BlindCompare != nil {
			blind = *in.BlindCompare
		}
		if in.DetailedEvalEnabled != nil {
			detailed = *in.DetailedEvalEnabled
		}
		if in.CompareNoticeAck != nil {
			ack = *in.CompareNoticeAck
		}
		if in.ReducedMotion != nil {
			reduced = *in.ReducedMotion
		}
		if in.DailyBudgetUSD != nil {
			trimmed := strings.TrimSpace(*in.DailyBudgetUSD)
			if trimmed == "" {
				budget = nil
			} else {
				d, dErr := decimal.NewFromString(trimmed)
				if dErr != nil || d.IsNegative() {
					return false, apperr.Validation("일일 예산은 0 이상의 숫자여야 해요.", nil)
				}
				budget = &d
			}
		}

		newAliasesRaw, _ := json.Marshal(aliases)
		newVer := curVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE user_settings
			SET release_mode = $2,
			    grow_mode = $3,
			    model_aliases = $4,
			    blind_compare = $5,
			    detailed_eval_enabled = $6,
			    compare_notice_ack = $7,
			    reduced_motion = $8,
			    daily_budget_usd = $9,
			    version = $10,
			    updated_at = now()
			WHERE owner_id = $1
		`, ownerID, relMode, growMode, newAliasesRaw, blind, detailed, ack, reduced, budget, newVer); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]string{"version": strconv.FormatInt(newVer, 10)})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.bundle.Env.IdempotencyRetention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return AISettingsView{}, err
		}
		return AISettingsView{}, apperr.Internal(err)
	}
	return s.GetAISettings(ctx, ownerID)
}

// ListComparisons returns strictly text-free comparison metadata and aggregates
// separating normal complete comparisons, incomplete early selections, both-skipped,
// and technical failures by model/prompt version (Spec Section 11.3 & 25.1).
func (s *Service) ListComparisons(ctx context.Context, ownerID uuid.UUID, limit int) (ComparisonsResponse, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.conversation_id, c.kind, t.prompt_version, t.context_hash,
		       t.selected_run_id, t.resolution, t.resolved_at, t.created_at,
		       fb.scores_json
		FROM conversation_turns t
		JOIN conversations c ON c.id = t.conversation_id
		LEFT JOIN turn_feedbacks fb ON fb.turn_id = t.id
		WHERE t.owner_id = $1 AND t.mode = 'compare'
		ORDER BY t.created_at DESC
		LIMIT $2
	`, ownerID, limit)
	if err != nil {
		return ComparisonsResponse{}, apperr.Internal(err)
	}
	defer rows.Close()

	type rawTurn struct {
		item        ComparisonItemDTO
		selectedID  *uuid.UUID
		resolvedAt  *time.Time
	}
	var turns []rawTurn
	for rows.Next() {
		var rt rawTurn
		var resPtr *string
		var fbRaw []byte
		if err := rows.Scan(
			&rt.item.TurnID, &rt.item.ConversationID, &rt.item.Kind,
			&rt.item.PromptVersion, &rt.item.ContextHash,
			&rt.selectedID, &resPtr, &rt.resolvedAt, &rt.item.CreatedAt, &fbRaw,
		); err != nil {
			return ComparisonsResponse{}, apperr.Internal(err)
		}
		if resPtr != nil {
			rt.item.Resolution = *resPtr
		}
		if len(fbRaw) > 0 {
			_ = json.Unmarshal(fbRaw, &rt.item.FeedbackScores)
		}
		turns = append(turns, rt)
	}
	rows.Close()

	groupsMap := map[string]*ComparisonGroupSummary{}
	var groupOrder []string
	items := make([]ComparisonItemDTO, 0, len(turns))

	for _, rt := range turns {
		rRows, err := s.pool.Query(ctx, `
			SELECT id, turn_id, slot, attempt_no, provider, model_alias, requested_model,
			       reported_model, is_mock, status, NULL::text AS output_text, error_code, finish_reason,
			       queued_at, started_at, first_text_at, finished_at,
			       usage_json, usage_status, price_version, estimated_cost
			FROM ai_runs
			WHERE turn_id = $1 AND owner_id = $2 AND superseded_at IS NULL
			ORDER BY slot ASC
		`, rt.item.TurnID, ownerID)
		if err != nil {
			return ComparisonsResponse{}, apperr.Internal(err)
		}

		var runs []RunDTO
		var openaiModel, geminiModel string
		succeededCount := 0
		failedOrBlocked := 0
		cancelledPeer := 0

		for rRows.Next() {
			rd, err := scanRunDTO(rRows, false, false)
			if err != nil {
				rRows.Close()
				return ComparisonsResponse{}, apperr.Internal(err)
			}
			rd.OutputText = "" // Never expose candidate text on /comparisons (Spec Section 11.3)
			if strings.Contains(rd.Provider, "openai") {
				openaiModel = rd.RequestedModel
			} else if strings.Contains(rd.Provider, "gemini") {
				geminiModel = rd.RequestedModel
			}
			switch rd.Status {
			case ai.StatusSucceeded:
				succeededCount++
			case ai.StatusFailed, ai.StatusBlocked, ai.StatusIncomplete, ai.StatusInterrupted:
				failedOrBlocked++
			case ai.StatusCancelled:
				cancelledPeer++
			}
			if rt.selectedID != nil && rd.ID == *rt.selectedID {
				rt.item.SelectedSlot = rd.Slot
				rt.item.SelectedProvider = canonicalProviderFamily(rd.Provider)
			}
			runs = append(runs, rd)
		}
		rRows.Close()
		rt.item.Runs = runs

		if rt.resolvedAt == nil {
			rt.item.Category = "pending"
		} else if failedOrBlocked > 0 {
			rt.item.Category = "technical_failure"
		} else if rt.item.Resolution == "user_selected" && succeededCount == 1 && cancelledPeer > 0 {
			rt.item.Category = "incomplete_early_selection"
		} else if rt.item.Resolution == "no_answer" && succeededCount == 2 {
			rt.item.Category = "both_skipped"
		} else if rt.item.Resolution == "user_selected" && succeededCount == 2 {
			rt.item.Category = "complete_comparison"
		} else {
			rt.item.Category = "incomplete_early_selection"
		}

		gKey := rt.item.PromptVersion + "|" + openaiModel + "|" + geminiModel
		grp, exists := groupsMap[gKey]
		if !exists {
			grp = &ComparisonGroupSummary{
				GroupKey:       gKey,
				PromptVersion:  rt.item.PromptVersion,
				OpenAIModel:    openaiModel,
				GeminiModel:    geminiModel,
				WinsByProvider: map[string]int{"openai": 0, "gemini": 0},
			}
			groupsMap[gKey] = grp
			groupOrder = append(groupOrder, gKey)
		}

		switch rt.item.Category {
		case "complete_comparison":
			grp.CompleteComparisons++
			if rt.item.SelectedProvider != "" {
				grp.WinsByProvider[rt.item.SelectedProvider]++
			}
		case "incomplete_early_selection":
			grp.IncompleteEarlySelections++
		case "both_skipped":
			grp.BothSkipped++
		case "technical_failure":
			grp.TechnicalFailures++
		}

		items = append(items, rt.item)
	}

	groups := make([]ComparisonGroupSummary, 0, len(groupOrder))
	for _, k := range groupOrder {
		groups = append(groups, *groupsMap[k])
	}
	return ComparisonsResponse{Groups: groups, Items: items}, nil
}

func (s *Service) resolveAliasesForMode(mode string, requested []string, defaults map[string]string) ([]string, error) {
	defaultOpenAI := defaults["openai"]
	if defaultOpenAI == "" {
		defaultOpenAI = "openai_chat"
	}
	defaultGemini := defaults["gemini"]
	if defaultGemini == "" {
		defaultGemini = "gemini_chat"
	}

	switch mode {
	case "openai":
		alias := defaultOpenAI
		if len(requested) > 0 && requested[0] != "" {
			alias = requested[0]
		}
		ent, ok := s.bundle.Models.GetByAlias(alias)
		if !ok || ent.Provider != "openai" {
			return nil, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "선택한 OpenAI 모델을 사용할 수 없어요.", false, nil)
		}
		return []string{alias}, nil

	case "gemini":
		alias := defaultGemini
		if len(requested) > 0 && requested[0] != "" {
			alias = requested[0]
		}
		ent, ok := s.bundle.Models.GetByAlias(alias)
		if !ok || ent.Provider != "gemini" {
			return nil, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "선택한 Gemini 모델을 사용할 수 없어요.", false, nil)
		}
		return []string{alias}, nil

	case "compare":
		a0, a1 := defaultOpenAI, defaultGemini
		if len(requested) == 2 {
			a0, a1 = requested[0], requested[1]
		}
		e0, ok0 := s.bundle.Models.GetByAlias(a0)
		e1, ok1 := s.bundle.Models.GetByAlias(a1)
		if !ok0 || !ok1 || e0.Provider == e1.Provider {
			return nil, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "비교 모드에는 서로 다른 두 공급자의 활성 모델이 필요해요.", false, nil)
		}
		return []string{a0, a1}, nil
	}
	return nil, apperr.Validation("지원하지 않는 생성 모드예요.", nil)
}

func (s *Service) resolveProviderTarget(alias string) (string, config.ModelEntry, ai.AdapterMetadata, error) {
	entry, ok := s.bundle.Models.GetByAlias(alias)
	if !ok {
		return "", config.ModelEntry{}, ai.AdapterMetadata{}, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "사용할 수 없는 모델이에요.", false, nil)
	}
	switch entry.Provider {
	case "openai":
		if s.bundle.Env.OpenAIAPIKey != "" {
			p := s.providers["openai"]
			return "openai", entry, p.Metadata(), nil
		}
		if s.bundle.Env.AllowMockAI {
			p := s.providers["mock_openai"]
			return "mock_openai", entry, p.Metadata(), nil
		}
		return "", config.ModelEntry{}, ai.AdapterMetadata{}, apperr.New(
			apperr.CodeProviderNotConfigured,
			http.StatusServiceUnavailable,
			"서버에 OpenAI API 키가 설정되지 않았어요. 작성한 글은 그대로 유지됩니다.",
			false,
			map[string]any{"provider": "openai"},
		)
	case "gemini":
		if s.bundle.Env.GeminiAPIKey != "" {
			p := s.providers["gemini"]
			return "gemini", entry, p.Metadata(), nil
		}
		if s.bundle.Env.AllowMockAI {
			p := s.providers["mock_gemini"]
			return "mock_gemini", entry, p.Metadata(), nil
		}
		return "", config.ModelEntry{}, ai.AdapterMetadata{}, apperr.New(
			apperr.CodeProviderNotConfigured,
			http.StatusServiceUnavailable,
			"서버에 Gemini API 키가 설정되지 않았어요. 작성한 글은 그대로 유지됩니다.",
			false,
			map[string]any{"provider": "gemini"},
		)
	}
	return "", config.ModelEntry{}, ai.AdapterMetadata{}, apperr.New(apperr.CodeModelUnavailable, http.StatusUnprocessableEntity, "지원하지 않는 공급자예요.", false, nil)
}

func scanRunDTO(rows pgx.Rows, turnOpen bool, hideIdentity bool) (RunDTO, error) {
	var rd RunDTO
	var repModel, outTxt, errCode, finReason, priceVer *string
	var queuedAt time.Time
	var startedAt, firstTextAt, finishedAt *time.Time
	var usageRaw []byte
	var estCost *decimal.Decimal

	if err := rows.Scan(
		&rd.ID, &rd.TurnID, &rd.Slot, &rd.AttemptNo, &rd.Provider, &rd.ModelAlias,
		&rd.RequestedModel, &repModel, &rd.IsMock, &rd.Status, &outTxt, &errCode, &finReason,
		&queuedAt, &startedAt, &firstTextAt, &finishedAt,
		&usageRaw, &rd.UsageStatus, &priceVer, &estCost,
	); err != nil {
		return RunDTO{}, err
	}

	if repModel != nil {
		rd.ReportedModel = *repModel
	}
	if outTxt != nil {
		rd.OutputText = *outTxt
	}
	if errCode != nil {
		rd.ErrorCode = *errCode
	}
	if finReason != nil {
		rd.FinishReason = *finReason
	}
	if priceVer != nil {
		rd.PriceVersion = *priceVer
	}
	if len(usageRaw) > 0 {
		rd.Usage = json.RawMessage(usageRaw)
	}
	if estCost != nil {
		cs := estCost.StringFixed(8)
		rd.EstimatedCostUSD = &cs
	}

	if startedAt != nil {
		qms := startedAt.Sub(queuedAt).Milliseconds()
		if qms < 0 {
			qms = 0
		}
		rd.QueueMs = &qms
		if firstTextAt != nil {
			ft := firstTextAt.Sub(*startedAt).Milliseconds()
			if ft < 0 {
				ft = 0
			}
			rd.FirstTextMs = &ft
		}
		if finishedAt != nil {
			tot := finishedAt.Sub(*startedAt).Milliseconds()
			if tot < 0 {
				tot = 0
			}
			rd.TotalMs = &tot
		}
	}

	if hideIdentity {
		rd.BlindHidden = true
		rd.DisplayLabel = "후보 " + strings.ToUpper(rd.Slot)
		rd.Provider = "blind"
		rd.ModelAlias = "blind"
		rd.RequestedModel = "선택 후 공개"
		rd.ReportedModel = ""
	} else {
		fam := "OpenAI"
		if strings.Contains(rd.Provider, "gemini") {
			fam = "Gemini"
		}
		if rd.IsMock {
			rd.DisplayLabel = fam + " (Mock · " + rd.RequestedModel + ")"
		} else {
			rd.DisplayLabel = fam + " · " + rd.RequestedModel
		}
	}

	if turnOpen {
		rd.CanAccept = rd.Status == ai.StatusSucceeded && strings.TrimSpace(rd.OutputText) != ""
		rd.CanCancel = rd.Status == ai.StatusQueued || rd.Status == ai.StatusRunning || rd.Status == ai.StatusStreaming
		rd.CanRetry = rd.Status == ai.StatusFailed || rd.Status == ai.StatusIncomplete || rd.Status == ai.StatusInterrupted || rd.Status == ai.StatusCancelled
	}
	return rd, nil
}

func canonicalProviderFamily(p string) string {
	if strings.Contains(p, "gemini") {
		return "gemini"
	}
	return "openai"
}

func randomCoinFlip() bool {
	n, err := rand.Int(rand.Reader, big.NewInt(2))
	return err == nil && n.Int64() == 1
}

func containsStr(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

func parsePositiveInt64(v, field string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation(field+" 형식이 올바르지 않아요.", map[string]string{field: "양의 정수 문자열이 필요해요."})
	}
	return n, nil
}
