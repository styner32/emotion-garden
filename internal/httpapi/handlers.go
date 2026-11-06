package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/conversation"
	"emotion-garden/internal/garden"
	"emotion-garden/internal/identity"
	"emotion-garden/internal/ritual"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	pool      *pgxpool.Pool
	bundle    *config.Bundle
	resolver  identity.IdentityResolver
	charSvc   *character.Service
	convSvc   *conversation.Service
	ritualSvc *ritual.Service
	gardenSvc *garden.Service
	broker    *ai.SSEBroker
}

func NewServer(
	pool *pgxpool.Pool,
	bundle *config.Bundle,
	resolver identity.IdentityResolver,
	charSvc *character.Service,
	convSvc *conversation.Service,
	ritualSvc *ritual.Service,
	gardenSvc *garden.Service,
	broker *ai.SSEBroker,
) *Server {
	return &Server{
		pool:      pool,
		bundle:    bundle,
		resolver:  resolver,
		charSvc:   charSvc,
		convSvc:   convSvc,
		ritualSvc: ritualSvc,
		gardenSvc: gardenSvc,
		broker:    broker,
	}
}

func (s *Server) Router(loggerMiddleware func(http.Handler) http.Handler) http.Handler {
	r := chi.NewRouter()
	if loggerMiddleware != nil {
		r.Use(loggerMiddleware)
	}
	r.Use(SecurityOriginGuard(s.bundle.Env.PublicOrigin, s.bundle.Env.ExtraAllowedOrigins))

	r.Get("/healthz", s.handleHealthz)
	r.Head("/healthz", s.handleHealthz)
	r.Get("/readyz", s.handleReadyz)
	r.Head("/readyz", s.handleReadyz)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(ResolveIdentityMiddleware(s.resolver))

		api.Get("/bootstrap", s.handleBootstrap)
		api.Get("/catalogs/avatars", s.handleGetAvatarCatalog)

		api.Get("/characters", s.handleListCharacters)
		api.Post("/characters", s.handleCreateCharacter)
		api.Patch("/characters/{id}", s.handleUpdateCharacter)
		api.Post("/characters/{id}/archive", s.handleArchiveCharacter)

		api.Get("/conversations/active", s.handleGetActiveConversations)
		api.Post("/conversations", s.handleCreateConversation)
		api.Get("/conversations/{id}", s.handleGetConversation)
		api.Patch("/conversations/{id}/preferences", s.handleUpdatePreferences)
		api.Put("/conversations/{id}/draft", s.handleSaveDraft)
		api.Post("/conversations/{id}/turns", s.handleSubmitTurn)
		api.Post("/conversations/{id}/closure", s.handleBeginClosure)

		api.Get("/turns/{id}", s.handleGetTurn)
		api.Get("/turns/{id}/events", s.handleStreamTurnEvents)
		api.Post("/turns/{id}/selection", s.handleSelectCandidate)
		api.Post("/turns/{id}/skip", s.handleSkipTurn)
		api.Post("/turns/{id}/feedback", s.handleSubmitFeedback)

		api.Post("/runs/{id}/cancel", s.handleCancelRun)
		api.Post("/runs/{id}/retry", s.handleRetryRun)

		api.Get("/rituals/{id}", s.handleGetRitual)
		api.Post("/rituals/{id}/return-to-chat", s.handleReturnToChat)
		api.Post("/rituals/{id}/route", s.handleSelectRoute)
		api.Post("/rituals/{id}/actions", s.handleStartRitualAction)
		api.Post("/rituals/{id}/actions/{actionId}/skip", s.handleSkipRitualAction)
		api.Post("/rituals/{id}/complete", s.handleCompleteRitual)

		api.Patch("/flowers/{id}", s.handleUpdateFlower)
		api.Post("/flowers/{id}/planting", s.handleBeginPlanting)
		api.Post("/flowers/{id}/discard", s.handleDiscardFlower)

		api.Get("/garden", s.handleGetGarden)
		api.Patch("/garden", s.handleResizeGarden)

		api.Get("/settings/ai", s.handleGetAISettings)
		api.Patch("/settings/ai", s.handleUpdateAISettings)
		api.Get("/comparisons", s.handleListComparisons)
	})

	// Serve built SPA static assets from same origin with fallback to index.html
	s.mountStaticSPA(r)
	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.Ping(r.Context()); err != nil {
		WriteError(w, r, apperr.New(apperr.CodeInternalError, http.StatusServiceUnavailable, "데이터베이스 연결을 확인하고 있어요.", true, nil))
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{
		"status":          "ready",
		"catalog_version": s.bundle.Avatars.CatalogVersion,
		"prompt_version":  s.bundle.Prompts.Version,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	slots, err := s.convSvc.GetActiveSlots(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	aiSettings, err := s.convSvc.GetAISettings(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	gView, err := s.gardenSvc.GetGarden(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	WriteJSON(w, r, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":        p.UserID.String(),
			"auth_mode": p.Mode,
			"timezone":  gView.Timezone,
		},
		"capabilities": map[string]any{
			"garden_max_cols":           s.bundle.Env.GardenMaxCols,
			"garden_max_rows":           s.bundle.Env.GardenMaxRows,
			"conversation_max_turns":    s.bundle.Env.ConversationMaxTurns,
			"content_context_max_bytes": s.bundle.Env.ContentContextMaxBytes,
			"message_max_chars":         4000,
			"kept_line_max_chars":       80,
			"openai_key_configured":     aiSettings.OpenAIKeyConfigured,
			"gemini_key_configured":     aiSettings.GeminiKeyConfigured,
			"mock_mode_enabled":         aiSettings.MockModeEnabled,
		},
		"active_conversations": slots,
		"garden_summary": map[string]any{
			"cols":           gView.Cols,
			"rows":           gView.Rows,
			"planted_count":  countPlanted(gView.Occupancies),
			"has_reserved":   gView.HasReserved,
			"garden_version": gView.Version,
		},
		"settings":      aiSettings,
		"ritual_routes": s.bundle.Rituals,
		"flowers":       s.bundle.Flowers,
	})
}

func countPlanted(occs []garden.OccupancyDTO) int {
	n := 0
	for _, o := range occs {
		if o.State == "planted" {
			n++
		}
	}
	return n
}

func (s *Server) handleGetAvatarCatalog(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, r, http.StatusOK, s.bundle.Avatars)
}

func (s *Server) handleListCharacters(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	list, err := s.charSvc.List(r.Context(), p.UserID, includeArchived)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]any{"characters": list})
}

func (s *Server) handleCreateCharacter(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	in, raw, err := DecodeStrictJSON[character.CreateInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/characters", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	ch, _, err := s.charSvc.Create(r.Context(), p.UserID, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusCreated, ch)
}

func (s *Server) handleUpdateCharacter(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[character.UpdateInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "PATCH:/characters/"+id.String(), raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	ch, err := s.charSvc.Update(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, ch)
}

func (s *Server) handleArchiveCharacter(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[character.ArchiveInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/characters/"+id.String()+"/archive", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	ch, err := s.charSvc.Archive(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, ch)
}

func (s *Server) handleGetActiveConversations(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	slots, err := s.convSvc.GetActiveSlots(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, slots)
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	in, raw, err := DecodeStrictJSON[conversation.CreateConversationInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/conversations", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	detail, _, err := s.convSvc.CreateConversation(r.Context(), p.UserID, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusCreated, detail)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	detail, err := s.convSvc.GetConversation(r.Context(), p.UserID, id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, detail)
}

func (s *Server) handleUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.UpdatePreferencesInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "PATCH:/conversations/"+id.String()+"/preferences", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	detail, err := s.convSvc.UpdatePreferences(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, detail)
}

func (s *Server) handleSaveDraft(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, _, err := DecodeStrictJSON[conversation.SaveDraftInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	draft, err := s.convSvc.SaveDraft(r.Context(), p.UserID, id, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, draft)
}

func (s *Server) handleSubmitTurn(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.SubmitTurnInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/conversations/"+id.String()+"/turns", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, status, err := s.convSvc.SubmitTurn(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, status, res)
}

func (s *Server) handleGetTurn(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	snap, err := s.convSvc.GetTurnSnapshot(r.Context(), p.UserID, id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, snap)
}

func (s *Server) handleStreamTurnEvents(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	turnID, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	var afterSeq int64
	cursorRaw := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if cursorRaw == "" {
		cursorRaw = strings.TrimSpace(r.URL.Query().Get("after_seq"))
	}
	if cursorRaw != "" {
		parsed, pErr := strconv.ParseInt(cursorRaw, 10, 64)
		if pErr != nil || parsed < 0 {
			WriteError(w, r, apperr.Validation("after_seq 커서 값이 올바르지 않아요.", nil))
			return
		}
		afterSeq = parsed
	}

	// Subscribe BEFORE initial DB read so no event committed concurrently is missed
	notifyCh, unsub := s.broker.Subscribe(turnID)
	defer unsub()

	initialEvents, resolved, err := s.convSvc.ListTurnEventsAfter(r.Context(), p.UserID, turnID, afterSeq)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, r, apperr.Internal(fmt.Errorf("streaming unsupported")))
		return
	}

	// Disable short HTTP WriteTimeout on long-lived SSE route (Spec Section 26.2)
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSE := func(ev conversation.TurnEventRecord) error {
		_, wErr := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.EventType, string(ev.Payload))
		if wErr != nil {
			return wErr
		}
		flusher.Flush()
		afterSeq = ev.Seq
		return nil
	}

	for _, ev := range initialEvents {
		if err := writeSSE(ev); err != nil {
			return
		}
	}
	flusher.Flush()

	if resolved {
		return
	}

	// 15-second SSE heartbeat comment (Spec Section 14.3)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat %s\n\n", time.Now().UTC().Format(time.RFC3339)); err != nil {
				return
			}
			flusher.Flush()
		case <-notifyCh:
			events, nowResolved, lErr := s.convSvc.ListTurnEventsAfter(r.Context(), p.UserID, turnID, afterSeq)
			if lErr != nil {
				if appE, ok := apperr.As(lErr); ok && appE.Code == apperr.CodeContentPurged {
					_, _ = fmt.Fprintf(w, "event: conversation.purged\ndata: {\"code\":\"CONTENT_PURGED\"}\n\n")
					flusher.Flush()
				}
				return
			}
			for _, ev := range events {
				if err := writeSSE(ev); err != nil {
					return
				}
			}
			if nowResolved {
				return
			}
		}
	}
}

func (s *Server) handleSelectCandidate(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.SelectCandidateInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/turns/"+id.String()+"/selection", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	snap, err := s.convSvc.SelectCandidate(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, snap)
}

func (s *Server) handleSkipTurn(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.SkipTurnInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/turns/"+id.String()+"/skip", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	snap, err := s.convSvc.SkipTurn(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, snap)
}

func (s *Server) handleSubmitFeedback(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.TurnFeedbackInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/turns/"+id.String()+"/feedback", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if err := s.convSvc.SubmitFeedback(r.Context(), p.UserID, id, idem, in); err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, map[string]bool{"saved": true})
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.RunActionInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/runs/"+id.String()+"/cancel", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	snap, err := s.convSvc.CancelRun(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, snap)
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[conversation.RunActionInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/runs/"+id.String()+"/retry", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	snap, err := s.convSvc.RetryRun(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, snap)
}

func (s *Server) handleBeginClosure(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[ritual.ClosureInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/conversations/"+id.String()+"/closure", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := s.ritualSvc.BeginClosure(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) handleGetRitual(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rv, err := s.ritualSvc.GetRitual(r.Context(), p.UserID, id)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, rv)
}

type versionOnlyReq struct {
	ExpectedVersion string `json:"expected_version"`
}

type selectRouteReq struct {
	ExpectedVersion string `json:"expected_version"`
	Route           string `json:"route"`
}

type startActionReq struct {
	ExpectedVersion string `json:"expected_version"`
	ActionKey       string `json:"action_key"`
}

func (s *Server) handleReturnToChat(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[versionOnlyReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/rituals/"+id.String()+"/return-to-chat", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := s.ritualSvc.ReturnToChat(r.Context(), p.UserID, id, idem, in.ExpectedVersion)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) handleSelectRoute(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[selectRouteReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/rituals/"+id.String()+"/route", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rv, err := s.ritualSvc.SelectRoute(r.Context(), p.UserID, id, idem, in.ExpectedVersion, in.Route)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, rv)
}

func (s *Server) handleStartRitualAction(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[startActionReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/rituals/"+id.String()+"/actions", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rv, err := s.ritualSvc.StartAction(r.Context(), p.UserID, id, idem, in.ExpectedVersion, in.ActionKey)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, rv)
}

func (s *Server) handleSkipRitualAction(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	actionID, err := parseUUIDParam(r, "actionId")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[versionOnlyReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/rituals/"+id.String()+"/actions/"+actionID.String()+"/skip", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	rv, err := s.ritualSvc.SkipAction(r.Context(), p.UserID, id, actionID, idem, in.ExpectedVersion)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, rv)
}

func (s *Server) handleCompleteRitual(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[versionOnlyReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/rituals/"+id.String()+"/complete", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := s.ritualSvc.CompleteRitual(r.Context(), p.UserID, id, idem, in.ExpectedVersion)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) handleUpdateFlower(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[garden.UpdateFlowerInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "PATCH:/flowers/"+id.String(), raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	flw, err := s.gardenSvc.UpdateFlowerGift(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, flw)
}

func (s *Server) handleBeginPlanting(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[garden.BeginPlantingInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/flowers/"+id.String()+"/planting", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := s.gardenSvc.BeginPlanting(r.Context(), p.UserID, id, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) handleDiscardFlower(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	in, raw, err := DecodeStrictJSON[versionOnlyReq](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "POST:/flowers/"+id.String()+"/discard", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	res, err := s.gardenSvc.DiscardFlower(r.Context(), p.UserID, id, idem, in.ExpectedVersion)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) handleGetGarden(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	g, err := s.gardenSvc.GetGarden(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, g)
}

func (s *Server) handleResizeGarden(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	in, raw, err := DecodeStrictJSON[garden.ResizeInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "PATCH:/garden", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	g, err := s.gardenSvc.ResizeGarden(r.Context(), p.UserID, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, g)
}

func (s *Server) handleGetAISettings(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	st, err := s.convSvc.GetAISettings(r.Context(), p.UserID)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, st)
}

func (s *Server) handleUpdateAISettings(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	in, raw, err := DecodeStrictJSON[conversation.UpdateAISettingsInput](w, r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	idem, err := ExtractIdemMeta(r, s.bundle.Env.IdempotencySecret, "PATCH:/settings/ai", raw)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	st, err := s.convSvc.UpdateAISettings(r.Context(), p.UserID, idem, in)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, st)
}

func (s *Server) handleListComparisons(w http.ResponseWriter, r *http.Request) {
	p, _ := identity.FromContext(r.Context())
	limit := 30
	if q := r.URL.Query().Get("page_size"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	res, err := s.convSvc.ListComparisons(r.Context(), p.UserID, limit)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	WriteJSON(w, r, http.StatusOK, res)
}

func (s *Server) mountStaticSPA(r chi.Router) {
	staticDir := s.bundle.Env.StaticDir
	fileServer := http.FileServer(http.Dir(staticDir))

	spaHandler := func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			WriteError(w, req, apperr.NotFound("요청한 API 경로를 찾을 수 없어요."))
			return
		}
		cleanPath := filepath.Clean(strings.TrimPrefix(req.URL.Path, "/"))
		if cleanPath == "." {
			cleanPath = "index.html"
		}
		fullPath := filepath.Join(staticDir, cleanPath)
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, req)
			return
		}
		indexFile := filepath.Join(staticDir, "index.html")
		if _, err := os.Stat(indexFile); err == nil {
			http.ServeFile(w, req, indexFile)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("SPA build not found; run npm --prefix web run build"))
	}

	r.Get("/*", spaHandler)
	r.Head("/*", spaHandler)
}

func parseUUIDParam(r *http.Request, key string) (uuid.UUID, error) {
	raw := strings.TrimSpace(chi.URLParam(r, key))
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperr.NotFound("요청한 항목을 찾을 수 없어요.")
	}
	return id, nil
}
