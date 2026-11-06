package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/ai/mock"
	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/conversation"
	"emotion-garden/internal/garden"
	"emotion-garden/internal/httpapi"
	"emotion-garden/internal/identity"
	"emotion-garden/internal/ritual"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type headerIdentityResolver struct {
	defaultUser uuid.UUID
}

func (r *headerIdentityResolver) Resolve(req *http.Request) (identity.Principal, error) {
	if raw := strings.TrimSpace(req.Header.Get("X-Test-User-ID")); raw != "" {
		uid, err := uuid.Parse(raw)
		if err != nil {
			return identity.Principal{}, apperr.Validation("invalid test user id", nil)
		}
		return identity.Principal{UserID: uid, Mode: "multi_user_test"}, nil
	}
	return identity.Principal{UserID: r.defaultUser, Mode: "single_user"}, nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type testEnv struct {
	t            *testing.T
	ctx          context.Context
	cancel       context.CancelFunc
	pool         *pgxpool.Pool
	bundle       *config.Bundle
	srv          *httptest.Server
	worker       *ai.Worker
	workerWG     *sync.WaitGroup
	defaultOwner uuid.UUID
	logBuf       *syncBuffer
	mockHookMu   sync.RWMutex
	mockHook     mock.HookFunc
	openaiCalls  atomic.Int64
	geminiCalls  atomic.Int64
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	t.Setenv("AI_MODEL_CONFIG", filepath.Join(root, "config", "models.yaml"))
	t.Setenv("AVATAR_CATALOG_CONFIG", filepath.Join(root, "config", "avatars.yaml"))
	t.Setenv("FLOWER_CATALOG_CONFIG", filepath.Join(root, "config", "flowers.yaml"))
	t.Setenv("RITUAL_CONFIG", filepath.Join(root, "config", "rituals.yaml"))
	t.Setenv("PROMPTS_DIR", filepath.Join(root, "prompts"))
	t.Setenv("PUBLIC_ORIGIN", "http://127.0.0.1:8080")
	t.Setenv("IDEMPOTENCY_SECRET", "test-secret-for-integration-suite")
	if os.Getenv("DATABASE_URL") == "" {
		t.Setenv("DATABASE_URL", "postgres://app:change_me@127.0.0.1:5432/emotion_garden?sslmode=disable")
	}

	bundle, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pool, err := storage.NewPool(ctx, bundle.Env.DatabaseURL)
	if err != nil {
		cancel()
		t.Fatalf("connect postgres: %v", err)
	}

	if err := storage.MigrateUp(ctx, pool, filepath.Join(root, "migrations")); err != nil {
		pool.Close()
		cancel()
		t.Fatalf("migrate up: %v", err)
	}

	// Clean tables for isolated test run
	_, err = pool.Exec(ctx, `
		TRUNCATE TABLE
			idempotency_records,
			garden_occupancies,
			flower_gifts,
			gardens,
			ritual_actions,
			rituals,
			turn_feedbacks,
			ai_run_events,
			ai_runs,
			conversation_messages,
			conversation_turns,
			conversation_drafts,
			conversations,
			characters,
			user_settings,
			auth_identities,
			app_users
		RESTART IDENTITY
	`)
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("truncate tables: %v", err)
	}

	ownerID, err := storage.EnsureBootstrap(
		ctx,
		pool,
		bundle.Env.DefaultTimezone,
		bundle.Env.GardenDefaultCols,
		bundle.Env.GardenDefaultRows,
		bundle.Models.DefaultReleaseMode,
		bundle.Models.DefaultGrowMode,
	)
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("ensure bootstrap: %v", err)
	}

	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	env := &testEnv{
		t:            t,
		ctx:          ctx,
		cancel:       cancel,
		pool:         pool,
		bundle:       bundle,
		defaultOwner: ownerID,
		logBuf:       logBuf,
	}

	dispatchHook := func(provider string) mock.HookFunc {
		return func(c context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, bool, error) {
			if strings.Contains(provider, "openai") {
				env.openaiCalls.Add(1)
			} else {
				env.geminiCalls.Add(1)
			}
			env.mockHookMu.RLock()
			hk := env.mockHook
			env.mockHookMu.RUnlock()
			if hk != nil {
				return hk(c, req, emit)
			}
			return ai.GenerationResult{}, false, nil
		}
	}

	providers := map[string]ai.ChatProvider{
		"mock_openai": mock.NewAdapter("mock_openai", 8*time.Millisecond, dispatchHook("mock_openai")),
		"mock_gemini": mock.NewAdapter("mock_gemini", 10*time.Millisecond, dispatchHook("mock_gemini")),
		"openai":      mock.NewAdapter("mock_openai", 8*time.Millisecond, dispatchHook("openai")),
		"gemini":      mock.NewAdapter("mock_gemini", 10*time.Millisecond, dispatchHook("gemini")),
	}

	broker := ai.NewSSEBroker()
	worker := ai.NewWorker(pool, bundle.Models, bundle.Prompts, providers, broker, ai.WorkerConfig{
		Concurrency:      4,
		RunTimeout:       15 * time.Second,
		FirstTextTimeout: 8 * time.Second,
		IdleTimeout:      8 * time.Second,
		LeaseDuration:    3 * time.Second,
		HeartbeatEvery:   400 * time.Millisecond,
		PollInterval:     40 * time.Millisecond,
	}, logger)
	workerWG := worker.Start(ctx)

	charSvc := character.NewService(pool, bundle.Avatars, bundle.Env.IdempotencyRetention)
	ritualSvc := ritual.NewService(pool, bundle.Rituals, bundle.Flowers, broker, bundle.Env.IdempotencyRetention)
	gardenSvc := garden.NewService(pool, bundle.Rituals, ritualSvc, bundle.Env.GardenMaxCols, bundle.Env.GardenMaxRows, bundle.Env.IdempotencyRetention)
	convSvc := conversation.NewService(pool, bundle, charSvc, ritualSvc, worker, broker, providers)

	resolver := &headerIdentityResolver{defaultUser: ownerID}
	apiSrv := httpapi.NewServer(pool, bundle, resolver, charSvc, convSvc, ritualSvc, gardenSvc, broker)
	ts := httptest.NewServer(apiSrv.Router(httpapi.RequestContextAndSafeLogger(logger)))

	env.srv = ts
	env.worker = worker
	env.workerWG = workerWG

	t.Cleanup(func() {
		ts.Close()
		cancel()
		workerWG.Wait()
		pool.Close()
	})
	return env
}

func (e *testEnv) createSecondUser() uuid.UUID {
	e.t.Helper()
	u := uuid.New()
	aliases, _ := json.Marshal(map[string]string{"openai": "openai_chat", "gemini": "gemini_chat"})
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO app_users (id, account_kind, timezone) VALUES ($1, 'registered', 'Asia/Seoul')`, u); err != nil {
		e.t.Fatalf("insert second user: %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO user_settings (owner_id, release_mode, grow_mode, model_aliases) VALUES ($1, 'openai', 'gemini', $2)`, u, aliases); err != nil {
		e.t.Fatalf("insert second settings: %v", err)
	}
	if _, err := e.pool.Exec(e.ctx, `INSERT INTO gardens (id, owner_id, cols, rows) VALUES ($1, $2, 4, 3)`, uuid.New(), u); err != nil {
		e.t.Fatalf("insert second garden: %v", err)
	}
	return u
}

type apiResp struct {
	Status int
	Data   json.RawMessage
	Err    *apperr.Error
}

func (e *testEnv) doJSON(method, path string, body any, idemKey string, extraHeaders map[string]string) apiResp {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case string:
			reader = strings.NewReader(v)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				e.t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(raw)
		}
	} else if method != http.MethodGet {
		reader = strings.NewReader("{}")
	}

	req, err := http.NewRequest(method, e.srv.URL+path, reader)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:8080")
		if idemKey == "" {
			idemKey = "idem_" + uuid.NewString()
		}
		if idemKey != "OMIT" {
			req.Header.Set("Idempotency-Key", idemKey)
		}
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := e.srv.Client().Do(req)
	if err != nil {
		e.t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	rawResp, _ := io.ReadAll(resp.Body)

	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *apperr.Error   `json:"error"`
	}
	_ = json.Unmarshal(rawResp, &env)
	return apiResp{
		Status: resp.StatusCode,
		Data:   env.Data,
		Err:    env.Error,
	}
}

func defaultAppearance(hair string) character.Appearance {
	return character.Appearance{
		FacePreset:    "calm_oval",
		HairStyle:     hair,
		HairColor:     "#231F20",
		SkinTone:      "tone_02",
		EyeStyle:      "sparkling",
		AcneLevel:     "mild",
		FrecklesLevel: "mild",
		WrinklesLevel: "none",
		ClothingColor: "#5E7A68",
	}
}

func (e *testEnv) waitForTurnState(turnID uuid.UUID, wantStates ...string) conversation.TurnSnapshotDTO {
	e.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r := e.doJSON(http.MethodGet, "/api/v1/turns/"+turnID.String(), nil, "", nil)
		if r.Status == http.StatusOK {
			var snap conversation.TurnSnapshotDTO
			_ = json.Unmarshal(r.Data, &snap)
			for _, ws := range wantStates {
				if snap.State == ws {
					return snap
				}
			}
		}
		time.Sleep(35 * time.Millisecond)
	}
	e.t.Fatalf("timed out waiting for turn %s to reach %v", turnID, wantStates)
	return conversation.TurnSnapshotDTO{}
}

func TestBackendSuite_AuthCharactersAndConversations(t *testing.T) {
	env := setupTestEnv(t)

	// AUTH03: Concurrent EnsureBootstrap creates exactly 1 bootstrap user & 1 garden
	var wg sync.WaitGroup
	ids := make([]uuid.UUID, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			uid, err := storage.EnsureBootstrap(env.ctx, env.pool, "Asia/Seoul", 4, 3, "openai", "gemini")
			if err != nil {
				t.Errorf("concurrent bootstrap err: %v", err)
			}
			ids[idx] = uid
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != env.defaultOwner {
			t.Fatalf("expected single bootstrap owner %s, got %s", env.defaultOwner, id)
		}
	}

	// AUTH02: Spoofed owner_id field in request body rejected (422 DisallowUnknownFields)
	spoofResp := env.doJSON(http.MethodPost, "/api/v1/characters",
		`{"owner_id":"`+uuid.NewString()+`","kind":"release","name":"침입자","gender":"male","appearance":{"face_preset":"calm_oval","hair_style":"perm","hair_color":"#231F20","skin_tone":"tone_02","eye_style":"sparkling","acne_level":"none","freckles_level":"none","wrinkles_level":"none","clothing_color":"#5E7A68"}}`,
		"", nil)
	if spoofResp.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on spoofed owner_id field, got %d", spoofResp.Status)
	}

	// Origin CSRF guard check
	badOriginResp := env.doJSON(http.MethodPost, "/api/v1/characters",
		character.CreateInput{Kind: "release", Name: "외부출처", Gender: "male", Appearance: defaultAppearance("perm")},
		"", map[string]string{"Origin": "https://evil.example"})
	if badOriginResp.Status != http.StatusForbidden || badOriginResp.Err == nil || badOriginResp.Err.Code != apperr.CodeOriginForbidden {
		t.Fatalf("expected 403 ORIGIN_FORBIDDEN, got %d (%+v)", badOriginResp.Status, badOriginResp.Err)
	}

	// CHAR01 & CHAR02: Create characters with all 6 hair styles & independent skin traits
	hairs := []string{"perm", "long_straight", "braid", "sport", "leaf_cut", "dandy_cut"}
	var releaseChar character.Character
	var growChar character.Character
	for idx, h := range hairs {
		kind := "release"
		if idx%2 == 1 {
			kind = "grow"
		}
		age := int16(25 + idx)
		app := defaultAppearance(h)
		app.AcneLevel = "distinct"
		app.FrecklesLevel = "mild"
		app.WrinklesLevel = "distinct"
		res := env.doJSON(http.MethodPost, "/api/v1/characters", character.CreateInput{
			Kind:       kind,
			Name:       fmt.Sprintf("인물_%s", h),
			Age:        &age,
			Gender:     "female",
			Appearance: app,
		}, "", nil)
		if res.Status != http.StatusCreated {
			t.Fatalf("create character %s failed: %d %+v", h, res.Status, res.Err)
		}
		var created character.Character
		_ = json.Unmarshal(res.Data, &created)
		if idx == 0 {
			releaseChar = created
		}
		if idx == 1 {
			growChar = created
		}
	}

	// CHAT01 & CHAT02 & CHAT03: Parallel release + grow active conversations; duplicate release -> 409 ACTIVE_CONVERSATION_EXISTS
	var cwg sync.WaitGroup
	statuses := make([]int, 2)
	var releaseConv conversation.ConversationDetail
	for i := 0; i < 2; i++ {
		cwg.Add(1)
		go func(idx int) {
			defer cwg.Done()
			r := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
				Kind:           "release",
				CharacterID:    releaseChar.ID.String(),
				GenerationMode: "openai",
			}, fmt.Sprintf("conc_create_%d", idx), nil)
			statuses[idx] = r.Status
			if r.Status == http.StatusCreated {
				_ = json.Unmarshal(r.Data, &releaseConv)
			}
		}(i)
	}
	cwg.Wait()
	if (statuses[0] == 201 && statuses[1] == 409) || (statuses[0] == 409 && statuses[1] == 201) {
		// Expected!
	} else {
		t.Fatalf("expected one 201 and one 409 on concurrent release creation, got %v", statuses)
	}

	growRes := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
		Kind:           "grow",
		CharacterID:    growChar.ID.String(),
		GenerationMode: "openai",
		ReplyPolicy:    "supportive",
	}, "", nil)
	if growRes.Status != http.StatusCreated {
		t.Fatalf("create parallel grow conversation failed: %d %+v", growRes.Status, growRes.Err)
	}
	var growConv conversation.ConversationDetail
	_ = json.Unmarshal(growRes.Data, &growConv)

	// Reload check (CHAT03): opening message remains exactly 1
	reloadGrow := env.doJSON(http.MethodGet, "/api/v1/conversations/"+growConv.ID.String(), nil, "", nil)
	var reloadedGrow conversation.ConversationDetail
	_ = json.Unmarshal(reloadGrow.Data, &reloadedGrow)
	if len(reloadedGrow.Messages) != 1 || reloadedGrow.Messages[0].Origin != "opening" {
		t.Fatalf("expected single opening message after reload, got %+v", reloadedGrow.Messages)
	}

	// CHAR03 & CHAR04: Edit and archive character while conversation is active -> snapshot in active conversation stays immutable
	newName := "수정된이름"
	updCharRes := env.doJSON(http.MethodPatch, "/api/v1/characters/"+releaseChar.ID.String(), character.UpdateInput{
		ExpectedVersion: releaseChar.Version,
		Name:            &newName,
	}, "", nil)
	if updCharRes.Status != http.StatusOK {
		t.Fatalf("update character failed: %d %+v", updCharRes.Status, updCharRes.Err)
	}
	var updatedChar character.Character
	_ = json.Unmarshal(updCharRes.Data, &updatedChar)

	archRes := env.doJSON(http.MethodPost, "/api/v1/characters/"+releaseChar.ID.String()+"/archive", character.ArchiveInput{
		ExpectedVersion: updatedChar.Version,
	}, "", nil)
	if archRes.Status != http.StatusOK {
		t.Fatalf("archive character failed: %d %+v", archRes.Status, archRes.Err)
	}

	checkSnapRes := env.doJSON(http.MethodGet, "/api/v1/conversations/"+releaseConv.ID.String(), nil, "", nil)
	var snapConv conversation.ConversationDetail
	_ = json.Unmarshal(checkSnapRes.Data, &snapConv)
	if snapConv.Character.Name != releaseChar.Name {
		t.Fatalf("active conversation character snapshot mutated! got %s, want %s", snapConv.Character.Name, releaseChar.Name)
	}

	// AUTH01 & AUTH03: Cross-owner isolation returns 404 on GET/POST/SSE
	secondUser := env.createSecondUser()
	otherHeaders := map[string]string{"X-Test-User-ID": secondUser.String()}
	crossGet := env.doJSON(http.MethodGet, "/api/v1/conversations/"+releaseConv.ID.String(), nil, "", otherHeaders)
	if crossGet.Status != http.StatusNotFound {
		t.Fatalf("expected 404 on cross-owner GET conversation, got %d", crossGet.Status)
	}
	crossPost := env.doJSON(http.MethodPost, "/api/v1/conversations/"+releaseConv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "cross_1",
		ExpectedVersion: snapConv.Version,
		Text:            "타인 접근",
	}, "", otherHeaders)
	if crossPost.Status != http.StatusNotFound {
		t.Fatalf("expected 404 on cross-owner POST turn, got %d", crossPost.Status)
	}

	// CHAT04, CHAT06, CHAT09: Switch release conversation to quiet reply_policy -> 0 AI runs, HTTP 201
	quietPolicy := "quiet"
	prefRes := env.doJSON(http.MethodPatch, "/api/v1/conversations/"+snapConv.ID.String()+"/preferences", conversation.UpdatePreferencesInput{
		ExpectedVersion: snapConv.Version,
		ReplyPolicy:     &quietPolicy,
	}, "", nil)
	if prefRes.Status != http.StatusOK {
		t.Fatalf("update preferences to quiet failed: %d %+v", prefRes.Status, prefRes.Err)
	}
	_ = json.Unmarshal(prefRes.Data, &snapConv)

	saveDraftRes := env.doJSON(http.MethodPut, "/api/v1/conversations/"+snapConv.ID.String()+"/draft", conversation.SaveDraftInput{
		DraftVersion:  snapConv.Draft.DraftVersion,
		CapturedEpoch: snapConv.ContentEpoch,
		Text:          "초안 텍스트입니다",
	}, "", nil)
	if saveDraftRes.Status != http.StatusOK {
		t.Fatalf("save draft failed: %d %+v", saveDraftRes.Status, saveDraftRes.Err)
	}

	idemTurnKey := "idem_quiet_turn_1"
	quietPayload := conversation.SubmitTurnInput{
		ClientTurnID:    "client_quiet_1",
		ExpectedVersion: snapConv.Version,
		Text:            "그냥 조용히 들어주면 좋겠어 SECRET_LOG_CANARY_42",
	}
	q1 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+snapConv.ID.String()+"/turns", quietPayload, idemTurnKey, nil)
	if q1.Status != http.StatusCreated {
		t.Fatalf("expected 201 on quiet turn, got %d %+v", q1.Status, q1.Err)
	}
	var qRes1 conversation.SubmitTurnResult
	_ = json.Unmarshal(q1.Data, &qRes1)
	if len(qRes1.Runs) != 0 || qRes1.State != "resolved" {
		t.Fatalf("quiet turn should create 0 AI runs and resolve immediately: %+v", qRes1)
	}

	// Exact same Idempotency-Key + same body -> replays identical turn
	q2 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+snapConv.ID.String()+"/turns", quietPayload, idemTurnKey, nil)
	var qRes2 conversation.SubmitTurnResult
	_ = json.Unmarshal(q2.Data, &qRes2)
	if qRes2.TurnID != qRes1.TurnID {
		t.Fatalf("expected idempotent replay to return same TurnID, got %s vs %s", qRes2.TurnID, qRes1.TurnID)
	}

	// Same Idempotency-Key + different body -> 409 IDEMPOTENCY_KEY_REUSED
	diffPayload := quietPayload
	diffPayload.Text = "다른 본문"
	qConflict := env.doJSON(http.MethodPost, "/api/v1/conversations/"+snapConv.ID.String()+"/turns", diffPayload, idemTurnKey, nil)
	if qConflict.Status != http.StatusConflict || qConflict.Err == nil || qConflict.Err.Code != apperr.CodeIdempotencyKeyReused {
		t.Fatalf("expected 409 IDEMPOTENCY_KEY_REUSED, got %d %+v", qConflict.Status, qConflict.Err)
	}

	// CHAT09: Late stale draft autosave with old DraftVersion before turn submit -> 409 VERSION_CONFLICT
	staleDraft := env.doJSON(http.MethodPut, "/api/v1/conversations/"+snapConv.ID.String()+"/draft", conversation.SaveDraftInput{
		DraftVersion:  snapConv.Draft.DraftVersion, // stale version before submit
		CapturedEpoch: snapConv.ContentEpoch,
		Text:          "전송 전 뒤늦게 도착한 초안",
	}, "", nil)
	if staleDraft.Status != http.StatusConflict || staleDraft.Err == nil || staleDraft.Err.Code != apperr.CodeVersionConflict {
		t.Fatalf("expected 409 VERSION_CONFLICT on stale draft autosave, got %d %+v", staleDraft.Status, staleDraft.Err)
	}

	// DATA05: Ensure secret canary message text NEVER appeared in structured logs
	if strings.Contains(env.logBuf.String(), "SECRET_LOG_CANARY_42") {
		t.Fatalf("PRIVACY VIOLATION: user message text leaked into server logs!")
	}
}

func TestBackendSuite_CompareStreamingRacesAndLeaseRecovery(t *testing.T) {
	env := setupTestEnv(t)

	age := int16(31)
	chRes := env.doJSON(http.MethodPost, "/api/v1/characters", character.CreateInput{
		Kind:       "release",
		Name:       "비교상대",
		Age:        &age,
		Gender:     "male",
		Appearance: defaultAppearance("dandy_cut"),
	}, "", nil)
	var ch character.Character
	_ = json.Unmarshal(chRes.Data, &ch)

	convRes := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
		Kind:           "release",
		CharacterID:    ch.ID.String(),
		GenerationMode: "compare",
	}, "", nil)
	var conv conversation.ConversationDetail
	_ = json.Unmarshal(convRes.Data, &conv)

	// Submit compare turn (AI02, AI08)
	turnRes := env.doJSON(http.MethodPost, "/api/v1/conversations/"+conv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "cmp_turn_1",
		ExpectedVersion: conv.Version,
		Text:            "회의 시간에 내 발표를 가로채서 정말 화가 났어.",
		GenerationMode:  "compare",
	}, "", nil)
	if turnRes.Status != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for compare turn, got %d %+v", turnRes.Status, turnRes.Err)
	}
	var sub conversation.SubmitTurnResult
	_ = json.Unmarshal(turnRes.Data, &sub)
	if len(sub.Runs) != 2 {
		t.Fatalf("expected 2 runs in compare turn, got %d", len(sub.Runs))
	}

	snap := env.waitForTurnState(sub.TurnID, "awaiting_selection")

	// AI02: Verify identical context_hash and prompt_hash across both candidate runs in DB
	var hashes []string
	rows, err := env.pool.Query(env.ctx, `SELECT context_hash || ':' || prompt_hash FROM ai_runs WHERE turn_id = $1 AND superseded_at IS NULL`, sub.TurnID)
	if err != nil {
		t.Fatalf("query run hashes: %v", err)
	}
	for rows.Next() {
		var h string
		_ = rows.Scan(&h)
		hashes = append(hashes, h)
	}
	rows.Close()
	if len(hashes) != 2 || hashes[0] != hashes[1] {
		t.Fatalf("expected identical context_hash:prompt_hash for compare pair, got %v", hashes)
	}

	// AI07 & AI08: Reconnect SSE with after_seq=1 -> replays events deterministically in strictly monotonic order without re-calling providers
	beforeOpenAI := env.openaiCalls.Load()
	beforeGemini := env.geminiCalls.Load()

	sseReq, _ := http.NewRequest(http.MethodGet, env.srv.URL+"/api/v1/turns/"+sub.TurnID.String()+"/events?after_seq=1", nil)
	// Turn isn't resolved yet so cancel SSE read once we receive turn.selection_required
	sseCtx, cancelSSE := context.WithTimeout(env.ctx, 2*time.Second)
	defer cancelSSE()
	sseResp, err := env.srv.Client().Do(sseReq.WithContext(sseCtx))
	if err != nil {
		t.Fatalf("sse connect err: %v", err)
	}
	scanner := bufio.NewScanner(sseResp.Body)
	var lastID int64 = 1
	sawSelectionReq := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "id:") {
			var seq int64
			_, _ = fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "id:")), "%d", &seq)
			if seq <= lastID {
				t.Fatalf("non-monotonic SSE event id: %d after %d", seq, lastID)
			}
			lastID = seq
		}
		if strings.HasPrefix(line, "event: turn.selection_required") {
			sawSelectionReq = true
			cancelSSE()
			break
		}
	}
	sseResp.Body.Close()
	if !sawSelectionReq {
		t.Fatalf("expected turn.selection_required in SSE replay")
	}
	if env.openaiCalls.Load() != beforeOpenAI || env.geminiCalls.Load() != beforeGemini {
		t.Fatalf("SSE replay unexpectedly re-invoked AI providers!")
	}

	// AI03 & AI04: Concurrent candidate selection for candidate A vs candidate B -> exactly 1 succeeds (200), loser gets 409 ALREADY_SELECTED or VERSION_CONFLICT
	var selWG sync.WaitGroup
	selCodes := make([]int, 2)
	for i := 0; i < 2; i++ {
		selWG.Add(1)
		go func(idx int) {
			defer selWG.Done()
			r := env.doJSON(http.MethodPost, "/api/v1/turns/"+sub.TurnID.String()+"/selection", conversation.SelectCandidateInput{
				ExpectedVersion: snap.Version,
				RunID:           snap.Runs[idx].ID.String(),
			}, fmt.Sprintf("sel_race_%d", idx), nil)
			selCodes[idx] = r.Status
		}(i)
	}
	selWG.Wait()
	if !((selCodes[0] == 200 && selCodes[1] == 409) || (selCodes[0] == 409 && selCodes[1] == 200)) {
		t.Fatalf("expected one 200 and one 409 on concurrent candidate selection, got %v", selCodes)
	}

	// Verify only 1 canonical assistant message was promoted for this turn (AI03)
	var promotedCount int
	_ = env.pool.QueryRow(env.ctx, `SELECT COUNT(*) FROM conversation_messages WHERE turn_id = $1 AND role = 'assistant'`, sub.TurnID).Scan(&promotedCount)
	if promotedCount != 1 {
		t.Fatalf("expected 1 canonical assistant message for turn, got %d", promotedCount)
	}

	// AI01: Switch provider to single gemini and verify turn 2 uses canonical history from turn 1
	convAfter1 := env.doJSON(http.MethodGet, "/api/v1/conversations/"+conv.ID.String(), nil, "", nil)
	var cDetail conversation.ConversationDetail
	_ = json.Unmarshal(convAfter1.Data, &cDetail)

	var sawHistoryInNextTurn atomic.Bool
	env.mockHookMu.Lock()
	env.mockHook = func(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, bool, error) {
		if strings.Contains(string(req.ContextJSON), "회의 시간에 내 발표를 가로채서") {
			sawHistoryInNextTurn.Store(true)
		}
		return ai.GenerationResult{}, false, nil
	}
	env.mockHookMu.Unlock()

	t2Res := env.doJSON(http.MethodPost, "/api/v1/conversations/"+conv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "single_gem_2",
		ExpectedVersion: cDetail.Version,
		Text:            "그래도 사과해줘서 조금 나아졌어.",
		GenerationMode:  "gemini",
	}, "", nil)
	var sub2 conversation.SubmitTurnResult
	_ = json.Unmarshal(t2Res.Data, &sub2)
	env.waitForTurnState(sub2.TurnID, "resolved")
	if !sawHistoryInNextTurn.Load() {
		t.Fatalf("expected canonical history from turn 1 in switched provider turn 2")
	}

	// AI05: Partial failure in compare mode -> retry failed side succeeds without re-running succeeded side
	convAfter2 := env.doJSON(http.MethodGet, "/api/v1/conversations/"+conv.ID.String(), nil, "", nil)
	_ = json.Unmarshal(convAfter2.Data, &cDetail)

	var failOnce atomic.Bool
	failOnce.Store(true)
	env.mockHookMu.Lock()
	env.mockHook = func(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, bool, error) {
		if strings.Contains(req.Provider, "gemini") && failOnce.CompareAndSwap(true, false) {
			return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "RATE_LIMITED"}, true, fmt.Errorf("rate limit")
		}
		return ai.GenerationResult{}, false, nil
	}
	env.mockHookMu.Unlock()

	t3Res := env.doJSON(http.MethodPost, "/api/v1/conversations/"+conv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "cmp_partial_fail_3",
		ExpectedVersion: cDetail.Version,
		Text:            "한쪽 실패 복구 테스트",
		GenerationMode:  "compare",
	}, "", nil)
	var sub3 conversation.SubmitTurnResult
	_ = json.Unmarshal(t3Res.Data, &sub3)

	// Wait until both runs finish (one succeeded, one failed)
	var snap3 conversation.TurnSnapshotDTO
	for i := 0; i < 80; i++ {
		r := env.doJSON(http.MethodGet, "/api/v1/turns/"+sub3.TurnID.String(), nil, "", nil)
		_ = json.Unmarshal(r.Data, &snap3)
		if len(snap3.Runs) == 2 &&
			(snap3.Runs[0].Status == "succeeded" || snap3.Runs[0].Status == "failed") &&
			(snap3.Runs[1].Status == "succeeded" || snap3.Runs[1].Status == "failed") {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	var failedRunID uuid.UUID
	for _, rn := range snap3.Runs {
		if rn.Status == "failed" {
			failedRunID = rn.ID
		}
	}
	if failedRunID == uuid.Nil {
		t.Fatalf("expected one failed run in partial failure test, got %+v", snap3.Runs)
	}

	openaiCallsBeforeRetry := env.openaiCalls.Load()
	retryRes := env.doJSON(http.MethodPost, "/api/v1/runs/"+failedRunID.String()+"/retry", conversation.RunActionInput{
		ExpectedTurnVersion: snap3.Version,
	}, "", nil)
	if retryRes.Status != http.StatusOK {
		t.Fatalf("retry failed run returned %d %+v", retryRes.Status, retryRes.Err)
	}
	snap3Recovered := env.waitForTurnState(sub3.TurnID, "awaiting_selection")
	if env.openaiCalls.Load() != openaiCallsBeforeRetry {
		t.Fatalf("retrying failed gemini run should NOT re-call openai!")
	}
	// Skip turn to clean up unresolved state
	skipRes := env.doJSON(http.MethodPost, "/api/v1/turns/"+sub3.TurnID.String()+"/skip", conversation.SkipTurnInput{
		ExpectedVersion: snap3Recovered.Version,
	}, "", nil)
	if skipRes.Status != http.StatusOK {
		t.Fatalf("skip turn failed: %d %+v", skipRes.Status, skipRes.Err)
	}

	// AI09: Expired lease recovery marks orphaned running row as interrupted without auto-reexecuting
	convAfter3 := env.doJSON(http.MethodGet, "/api/v1/conversations/"+conv.ID.String(), nil, "", nil)
	_ = json.Unmarshal(convAfter3.Data, &cDetail)

	orphanTurnID := uuid.New()
	orphanRunID := uuid.New()
	expiredAt := time.Now().UTC().Add(-10 * time.Second)
	_, err = env.pool.Exec(env.ctx, `
		INSERT INTO conversation_turns (id, owner_id, conversation_id, turn_no, client_turn_id, mode, prompt_version, prompt_hash, context_snapshot, context_hash)
		VALUES ($1, $2, $3, 99, 'orphan_turn', 'openai', 'v1', 'ph', '{}'::jsonb, 'h')
	`, orphanTurnID, env.defaultOwner, conv.ID)
	if err != nil {
		t.Fatalf("insert orphan turn: %v", err)
	}
	_, err = env.pool.Exec(env.ctx, `
		INSERT INTO ai_runs (
			id, owner_id, conversation_id, turn_id, slot, attempt_no, provider, model_alias,
			requested_model, api_revision, sdk_version, adapter_version, prompt_version, prompt_hash,
			context_hash, settings_snapshot, status, lease_token, lease_expires_at, captured_epoch
		) VALUES (
			$1, $2, $3, $4, 'primary', 1, 'mock_openai', 'openai_chat',
			'gpt-6.1-sol', '2026-11-05', 'test', 'v1', 'v1', 'ph',
			'ch', '{}'::jsonb, 'running', 'dead_lease', $5, 1
		)
	`, orphanRunID, env.defaultOwner, conv.ID, orphanTurnID, expiredAt)
	if err != nil {
		t.Fatalf("insert orphan run: %v", err)
	}

	reaped, err := env.worker.ReapExpiredLeases(env.ctx)
	if err != nil || reaped < 1 {
		t.Fatalf("expected ReapExpiredLeases >= 1, got %d err=%v", reaped, err)
	}
	var orphanStatus string
	_ = env.pool.QueryRow(env.ctx, `SELECT status FROM ai_runs WHERE id = $1`, orphanRunID).Scan(&orphanStatus)
	if orphanStatus != "interrupted" {
		t.Fatalf("expected orphan run status=interrupted, got %s", orphanStatus)
	}
}

func TestBackendSuite_RitualsFlowersGardenConcurrencyAndPurge(t *testing.T) {
	env := setupTestEnv(t)

	// 1. Release ritual flow: pack -> lift -> return-to-chat -> closure again -> sea route -> skip/step -> complete + purge (RIT01~07, DATA01, DATA04)
	rCharRes := env.doJSON(http.MethodPost, "/api/v1/characters", character.CreateInput{
		Kind:       "release",
		Name:       "비움상대",
		Gender:     "male",
		Appearance: defaultAppearance("sport"),
	}, "", nil)
	var rChar character.Character
	_ = json.Unmarshal(rCharRes.Data, &rChar)

	rConvRes := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
		Kind:           "release",
		CharacterID:    rChar.ID.String(),
		GenerationMode: "openai",
		ReplyPolicy:    "quiet",
	}, "", nil)
	if rConvRes.Status != http.StatusCreated {
		t.Fatalf("create release conv failed: %d %+v", rConvRes.Status, rConvRes.Err)
	}
	var rConv conversation.ConversationDetail
	_ = json.Unmarshal(rConvRes.Data, &rConv)

	// Add a sensitive user message so we can verify complete purge
	subR := env.doJSON(http.MethodPost, "/api/v1/conversations/"+rConv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "rel_msg_1",
		ExpectedVersion: rConv.Version,
		Text:            "절대 남아있으면 안 되는 비움 대화 원문입니다.",
	}, "", nil)
	if subR.Status != http.StatusCreated {
		t.Fatalf("submit quiet turn failed: %d %+v", subR.Status, subR.Err)
	}
	var subRRes conversation.SubmitTurnResult
	_ = json.Unmarshal(subR.Data, &subRRes)

	// Begin release closure -> immediately enters common.pack
	clos1 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+rConv.ID.String()+"/closure", ritual.ClosureInput{
		ExpectedVersion: subRRes.ConversationVersion,
	}, "", nil)
	if clos1.Status != http.StatusOK {
		t.Fatalf("begin release closure failed: %d %+v", clos1.Status, clos1.Err)
	}
	var clos1Res ritual.ClosureResult
	_ = json.Unmarshal(clos1.Data, &clos1Res)
	if clos1Res.Ritual == nil || clos1Res.Ritual.ActiveAction == nil || clos1Res.Ritual.ActiveAction.Key != "pack" {
		t.Fatalf("expected common.pack active on release closure start, got %+v", clos1Res.Ritual)
	}

	// RIT06: Double skip on pack -> first succeeds, second conflicts cleanly without jumping lift
	packActID := clos1Res.Ritual.ActiveAction.ID
	sk1 := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", clos1Res.Ritual.ID, packActID),
		map[string]string{"expected_version": clos1Res.Ritual.Version}, "", nil)
	if sk1.Status != http.StatusOK {
		t.Fatalf("skip pack failed: %d %+v", sk1.Status, sk1.Err)
	}
	var rvAfterPack ritual.RitualView
	_ = json.Unmarshal(sk1.Data, &rvAfterPack)
	if rvAfterPack.Cursor != 1 || rvAfterPack.NextActionKey != "lift" {
		t.Fatalf("expected cursor=1 next=lift after skipping pack, got %+v", rvAfterPack)
	}
	skDup := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", clos1Res.Ritual.ID, packActID),
		map[string]string{"expected_version": rvAfterPack.Version}, "", nil)
	if skDup.Status != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate skip of completed action, got %d", skDup.Status)
	}

	// Start lift & skip lift -> reaches route selection where return_to_chat is allowed (RIT01)
	stLift := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions", clos1Res.Ritual.ID),
		map[string]string{"expected_version": rvAfterPack.Version, "action_key": "lift"}, "", nil)
	var rvLift ritual.RitualView
	_ = json.Unmarshal(stLift.Data, &rvLift)

	skLift := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", clos1Res.Ritual.ID, rvLift.ActiveAction.ID),
		map[string]string{"expected_version": rvLift.Version}, "", nil)
	var rvRouteChoice ritual.RitualView
	_ = json.Unmarshal(skLift.Data, &rvRouteChoice)

	// Return to chat preserves conversation messages! (RIT01)
	retRes := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/return-to-chat", clos1Res.Ritual.ID),
		map[string]string{"expected_version": rvRouteChoice.Version}, "", nil)
	if retRes.Status != http.StatusOK {
		t.Fatalf("return-to-chat failed: %d %+v", retRes.Status, retRes.Err)
	}
	reopenedGet := env.doJSON(http.MethodGet, "/api/v1/conversations/"+rConv.ID.String(), nil, "", nil)
	var reopenedConv conversation.ConversationDetail
	_ = json.Unmarshal(reopenedGet.Data, &reopenedConv)
	if reopenedConv.Stage != "chat" || len(reopenedConv.Messages) != 2 {
		t.Fatalf("expected stage=chat and 2 preserved messages after return-to-chat, got stage=%s msgs=%d", reopenedConv.Stage, len(reopenedConv.Messages))
	}

	// Re-enter closure and complete via sea route (RIT02, RIT05, RIT07)
	clos2 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+rConv.ID.String()+"/closure", ritual.ClosureInput{
		ExpectedVersion: reopenedConv.Version,
	}, "", nil)
	var clos2Res ritual.ClosureResult
	_ = json.Unmarshal(clos2.Data, &clos2Res)
	rv := *clos2Res.Ritual

	// RIT07: Simulate long reconnect delay by backdating planned_end_at in DB -> GetRitual settles ONLY current step, never jumping ahead
	_, _ = env.pool.Exec(env.ctx, `UPDATE ritual_actions SET planned_end_at = now() - interval '10 minutes' WHERE id = $1`, rv.ActiveAction.ID)
	getAfterDelay := env.doJSON(http.MethodGet, "/api/v1/rituals/"+rv.ID.String(), nil, "", nil)
	_ = json.Unmarshal(getAfterDelay.Data, &rv)
	if rv.Cursor != 1 || rv.NextActionKey != "lift" {
		t.Fatalf("expected single-step settle to cursor=1 next=lift after long delay, got cursor=%d next=%s", rv.Cursor, rv.NextActionKey)
	}

	// Lift -> skip
	sLift2 := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions", rv.ID),
		map[string]string{"expected_version": rv.Version, "action_key": "lift"}, "", nil)
	_ = json.Unmarshal(sLift2.Data, &rv)
	skLift2 := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", rv.ID, rv.ActiveAction.ID),
		map[string]string{"expected_version": rv.Version}, "", nil)
	_ = json.Unmarshal(skLift2.Data, &rv)

	// Select route "sea" -> automatically begins "arrive"
	selSea := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/route", rv.ID),
		map[string]string{"expected_version": rv.Version, "route": "sea"}, "", nil)
	if selSea.Status != http.StatusOK {
		t.Fatalf("select route sea failed: %d %+v", selSea.Status, selSea.Err)
	}
	_ = json.Unmarshal(selSea.Data, &rv)
	if rv.ActiveAction == nil || rv.ActiveAction.Key != "arrive" {
		t.Fatalf("expected arrive auto-started on route select, got %+v", rv.ActiveAction)
	}
	skArrive := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", rv.ID, rv.ActiveAction.ID),
		map[string]string{"expected_version": rv.Version}, "", nil)
	_ = json.Unmarshal(skArrive.Data, &rv)

	// RIT05: Out-of-order action ("throw" before "prepare") rejected with 409 INVALID_TRANSITION
	badOrder := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions", rv.ID),
		map[string]string{"expected_version": rv.Version, "action_key": "throw"}, "", nil)
	if badOrder.Status != http.StatusConflict || badOrder.Err == nil || badOrder.Err.Code != apperr.CodeInvalidTransition {
		t.Fatalf("expected 409 INVALID_TRANSITION on out-of-order action, got %d %+v", badOrder.Status, badOrder.Err)
	}

	// Execute remaining prepare -> throw
	for _, stepKey := range []string{"prepare", "throw"} {
		st := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions", rv.ID),
			map[string]string{"expected_version": rv.Version, "action_key": stepKey}, "", nil)
		if st.Status != http.StatusOK {
			t.Fatalf("start sea step %s failed: %d %+v", stepKey, st.Status, st.Err)
		}
		_ = json.Unmarshal(st.Data, &rv)
		sk := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", rv.ID, rv.ActiveAction.ID),
			map[string]string{"expected_version": rv.Version}, "", nil)
		_ = json.Unmarshal(sk.Data, &rv)
	}
	hasComplete := false
	for _, cmd := range rv.AllowedCommands {
		if cmd == "complete_ritual" {
			hasComplete = true
		}
	}
	if !hasComplete {
		t.Fatalf("expected 'complete_ritual' in AllowedCommands after final sea step, got %+v", rv.AllowedCommands)
	}

	compSea := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/complete", rv.ID),
		map[string]string{"expected_version": rv.Version}, "", nil)
	if compSea.Status != http.StatusOK {
		t.Fatalf("complete sea ritual failed: %d %+v", compSea.Status, compSea.Err)
	}

	// DATA01 & DATA04: Release conversation raw content is purged & GET returns 410 CONTENT_PURGED
	purgedGet := env.doJSON(http.MethodGet, "/api/v1/conversations/"+rConv.ID.String(), nil, "", nil)
	if purgedGet.Status != http.StatusGone || purgedGet.Err == nil || purgedGet.Err.Code != apperr.CodeContentPurged {
		t.Fatalf("expected 410 CONTENT_PURGED on completed conversation, got %d %+v", purgedGet.Status, purgedGet.Err)
	}
	var remMsgs int
	_ = env.pool.QueryRow(env.ctx, `SELECT COUNT(*) FROM conversation_messages WHERE conversation_id = $1`, rConv.ID).Scan(&remMsgs)
	if remMsgs != 0 {
		t.Fatalf("expected 0 remaining messages after release purge, got %d", remMsgs)
	}

	// 2. Grow flower gift + garden resize + concurrent planting + atomic completion & purge (FLOW01~02, GARD01~11, DATA02)
	gCharRes := env.doJSON(http.MethodPost, "/api/v1/characters", character.CreateInput{
		Kind:       "grow",
		Name:       "피움친구",
		Gender:     "female",
		Appearance: defaultAppearance("braid"),
	}, "", nil)
	var gChar character.Character
	_ = json.Unmarshal(gCharRes.Data, &gChar)

	gConvRes := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
		Kind:           "grow",
		CharacterID:    gChar.ID.String(),
		GenerationMode: "openai",
	}, "", nil)
	var gConv conversation.ConversationDetail
	_ = json.Unmarshal(gConvRes.Data, &gConv)

	gTurn := env.doJSON(http.MethodPost, "/api/v1/conversations/"+gConv.ID.String()+"/turns", conversation.SubmitTurnInput{
		ClientTurnID:    "grow_msg_1",
		ExpectedVersion: gConv.Version,
		Text:            "피움 대화 원문도 심기 완료 후 완전히 삭제되어야 해.",
		GenerationMode:  "openai",
	}, "", nil)
	var gSub conversation.SubmitTurnResult
	_ = json.Unmarshal(gTurn.Data, &gSub)
	env.waitForTurnState(gSub.TurnID, "resolved")

	gAfterTurn := env.doJSON(http.MethodGet, "/api/v1/conversations/"+gConv.ID.String(), nil, "", nil)
	_ = json.Unmarshal(gAfterTurn.Data, &gConv)

	// FLOW01: Idempotent grow closure creates exactly 1 flower gift
	idemGrowClose := "idem_grow_closure_1"
	gc1 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+gConv.ID.String()+"/closure", ritual.ClosureInput{
		ExpectedVersion: gConv.Version,
	}, idemGrowClose, nil)
	gc2 := env.doJSON(http.MethodPost, "/api/v1/conversations/"+gConv.ID.String()+"/closure", ritual.ClosureInput{
		ExpectedVersion: gConv.Version,
	}, idemGrowClose, nil)
	var gcRes1, gcRes2 ritual.ClosureResult
	_ = json.Unmarshal(gc1.Data, &gcRes1)
	_ = json.Unmarshal(gc2.Data, &gcRes2)
	if gcRes1.Flower == nil || gcRes2.Flower == nil || gcRes1.Flower.ID != gcRes2.Flower.ID {
		t.Fatalf("expected identical flower gift on idempotent grow closure: %+v vs %+v", gcRes1.Flower, gcRes2.Flower)
	}
	flower := *gcRes1.Flower

	// FLOW02: kept_line > 80 runes rejected with 422, <= 80 runes accepted
	tooLong := strings.Repeat("꽃", 81)
	badLine := env.doJSON(http.MethodPatch, "/api/v1/flowers/"+flower.ID.String(), garden.UpdateFlowerInput{
		ExpectedVersion: flower.Version,
		KeptLine:        &tooLong,
	}, "", nil)
	if badLine.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on >80 char kept_line, got %d", badLine.Status)
	}
	goodLine := "오늘의 다정한 한 줄"
	okLine := env.doJSON(http.MethodPatch, "/api/v1/flowers/"+flower.ID.String(), garden.UpdateFlowerInput{
		ExpectedVersion: flower.Version,
		KeptLine:        &goodLine,
	}, "", nil)
	if okLine.Status != http.StatusOK {
		t.Fatalf("update kept_line failed: %d %+v", okLine.Status, okLine.Err)
	}
	_ = json.Unmarshal(okLine.Data, &flower)

	// GARD04: Two concurrent requests attempting to plant on the same cell (row=2, col=3) -> 1 succeeds, 1 conflicts
	gViewRes := env.doJSON(http.MethodGet, "/api/v1/garden", nil, "", nil)
	var gView garden.GardenView
	_ = json.Unmarshal(gViewRes.Data, &gView)

	var pWG sync.WaitGroup
	pStatuses := make([]int, 2)
	var pSuccess garden.BeginPlantingResult
	for i := 0; i < 2; i++ {
		pWG.Add(1)
		go func(idx int) {
			defer pWG.Done()
			r := env.doJSON(http.MethodPost, "/api/v1/flowers/"+flower.ID.String()+"/planting", garden.BeginPlantingInput{
				Row:                         2,
				Col:                         3,
				ExpectedGardenVersion:       gView.Version,
				ExpectedConversationVersion: gcRes1.ConversationVersion,
				ExpectedFlowerVersion:       flower.Version,
			}, fmt.Sprintf("plant_race_%d", idx), nil)
			pStatuses[idx] = r.Status
			if r.Status == http.StatusOK {
				_ = json.Unmarshal(r.Data, &pSuccess)
			}
		}(i)
	}
	pWG.Wait()
	if !((pStatuses[0] == 200 && pStatuses[1] == 409) || (pStatuses[0] == 409 && pStatuses[1] == 200)) {
		t.Fatalf("expected one 200 and one 409 on concurrent planting race, got %v", pStatuses)
	}

	// GARD05: Resize garden while reserved occupancy exists -> 409 GARDEN_BUSY
	busyResize := env.doJSON(http.MethodPatch, "/api/v1/garden", garden.ResizeInput{
		ExpectedVersion: pSuccess.GardenVersion,
		Cols:            5,
		Rows:            4,
	}, "", nil)
	if busyResize.Status != http.StatusConflict || busyResize.Err == nil || busyResize.Err.Code != apperr.CodeGardenBusy {
		t.Fatalf("expected 409 GARDEN_BUSY during active planting, got %d %+v", busyResize.Status, busyResize.Err)
	}

	// GARD06 & GARD07: BeginPlanting auto-started arrive -> skip arrive (visual_stage=0), then step through dig -> plant -> cover -> water (visual_stage=1,2,3,4)
	plantRV := pSuccess.Ritual
	if plantRV.ActiveAction == nil || plantRV.ActiveAction.Key != "arrive" {
		t.Fatalf("expected arrive auto-started on BeginPlanting, got %+v", plantRV.ActiveAction)
	}
	skPlantArrive := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", plantRV.ID, plantRV.ActiveAction.ID),
		map[string]string{"expected_version": plantRV.Version}, "", nil)
	if skPlantArrive.Status != http.StatusOK {
		t.Fatalf("skip garden arrive failed: %d %+v", skPlantArrive.Status, skPlantArrive.Err)
	}
	_ = json.Unmarshal(skPlantArrive.Data, &plantRV)
	if plantRV.VisualStage != 0 {
		t.Fatalf("expected visual_stage=0 after arrive, got %d", plantRV.VisualStage)
	}

	expectedVisualStages := []int{1, 2, 3, 4}
	for idx, stepKey := range []string{"dig", "plant", "cover", "water"} {
		st := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions", plantRV.ID),
			map[string]string{"expected_version": plantRV.Version, "action_key": stepKey}, "", nil)
		if st.Status != http.StatusOK {
			t.Fatalf("start garden step %s failed: %d %+v", stepKey, st.Status, st.Err)
		}
		_ = json.Unmarshal(st.Data, &plantRV)
		sk := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/actions/%s/skip", plantRV.ID, plantRV.ActiveAction.ID),
			map[string]string{"expected_version": plantRV.Version}, "", nil)
		_ = json.Unmarshal(sk.Data, &plantRV)
		if plantRV.VisualStage != expectedVisualStages[idx] {
			t.Fatalf("step %s expected visual_stage=%d, got %d", stepKey, expectedVisualStages[idx], plantRV.VisualStage)
		}
	}

	// GARD07: After water finishes, occupancy in DB MUST still be reserved until POST /complete
	var occStateBeforeComplete string
	_ = env.pool.QueryRow(env.ctx, `SELECT state FROM garden_occupancies WHERE flower_id = $1`, flower.ID).Scan(&occStateBeforeComplete)
	if occStateBeforeComplete != "reserved" {
		t.Fatalf("expected occupancy state=reserved before complete, got %s", occStateBeforeComplete)
	}

	// GARD08, GARD09, DATA02: Complete planting ritual atomically + idempotent replay
	idemComplete := "idem_complete_planting_1"
	cPlant1 := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/complete", plantRV.ID),
		map[string]string{"expected_version": plantRV.Version}, idemComplete, nil)
	cPlant2 := env.doJSON(http.MethodPost, fmt.Sprintf("/api/v1/rituals/%s/complete", plantRV.ID),
		map[string]string{"expected_version": plantRV.Version}, idemComplete, nil)
	if cPlant1.Status != http.StatusOK || cPlant2.Status != http.StatusOK {
		t.Fatalf("complete planting or idempotent replay failed: %d / %d", cPlant1.Status, cPlant2.Status)
	}

	// Verify occupancy is now planted, kept_line preserved, and raw grow conversation messages/context/run output are purged!
	gFinalRes := env.doJSON(http.MethodGet, "/api/v1/garden", nil, "", nil)
	_ = json.Unmarshal(gFinalRes.Data, &gView)
	if len(gView.Occupancies) != 1 || gView.Occupancies[0].State != "planted" || gView.Occupancies[0].KeptLine != goodLine {
		t.Fatalf("unexpected final garden occupancies: %+v", gView.Occupancies)
	}

	var rawRemain int
	_ = env.pool.QueryRow(env.ctx, `
		SELECT
			(SELECT COUNT(*) FROM conversation_messages WHERE conversation_id = $1) +
			(SELECT COUNT(*) FROM conversation_drafts WHERE conversation_id = $1) +
			(SELECT COUNT(*) FROM ai_run_events WHERE conversation_id = $1) +
			(SELECT COUNT(*) FROM conversation_turns WHERE conversation_id = $1 AND context_snapshot IS NOT NULL) +
			(SELECT COUNT(*) FROM ai_runs WHERE conversation_id = $1 AND output_text IS NOT NULL)
	`, gConv.ID).Scan(&rawRemain)
	if rawRemain != 0 {
		t.Fatalf("DATA02 VIOLATION: %d raw text records remained after grow purge!", rawRemain)
	}

	// GARD02: Shrink garden below 1-indexed planted coordinates (row=2, col=3 requires cols>=3, rows>=2) -> 409 GARDEN_SHRINK_CONFLICT
	shrinkBad := env.doJSON(http.MethodPatch, "/api/v1/garden", garden.ResizeInput{
		ExpectedVersion: gView.Version,
		Cols:            2,
		Rows:            1,
	}, "", nil)
	if shrinkBad.Status != http.StatusConflict || shrinkBad.Err == nil || shrinkBad.Err.Code != apperr.CodeGardenShrinkConflict {
		t.Fatalf("expected 409 GARDEN_SHRINK_CONFLICT, got %d %+v", shrinkBad.Status, shrinkBad.Err)
	}

	// Expand garden to 6x6 -> 200 OK (GARD01)
	expandOK := env.doJSON(http.MethodPatch, "/api/v1/garden", garden.ResizeInput{
		ExpectedVersion: gView.Version,
		Cols:            6,
		Rows:            6,
	}, "", nil)
	if expandOK.Status != http.StatusOK {
		t.Fatalf("expand garden to 6x6 failed: %d %+v", expandOK.Status, expandOK.Err)
	}

	// GARD10 & GARD11: Create another grow conversation and discard flower without planting -> purges conversation cleanly
	gConv2Res := env.doJSON(http.MethodPost, "/api/v1/conversations", conversation.CreateConversationInput{
		Kind:           "grow",
		CharacterID:    gChar.ID.String(),
		GenerationMode: "openai",
		ReplyPolicy:    "supportive",
	}, "", nil)
	var gConv2 conversation.ConversationDetail
	_ = json.Unmarshal(gConv2Res.Data, &gConv2)

	gcDiscard := env.doJSON(http.MethodPost, "/api/v1/conversations/"+gConv2.ID.String()+"/closure", ritual.ClosureInput{
		ExpectedVersion: gConv2.Version,
	}, "", nil)
	var gcDiscardRes ritual.ClosureResult
	_ = json.Unmarshal(gcDiscard.Data, &gcDiscardRes)

	discRes := env.doJSON(http.MethodPost, "/api/v1/flowers/"+gcDiscardRes.Flower.ID.String()+"/discard",
		map[string]string{"expected_version": gcDiscardRes.Flower.Version}, "", nil)
	if discRes.Status != http.StatusOK {
		t.Fatalf("discard flower failed: %d %+v", discRes.Status, discRes.Err)
	}

	// Verify schema & zero logical orphans across all tables!
	if err := storage.VerifySchemaAndOrphans(env.ctx, env.pool); err != nil {
		t.Fatalf("VerifySchemaAndOrphans failed: %v", err)
	}
}
