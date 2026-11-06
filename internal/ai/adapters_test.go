package ai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emotion-garden/internal/ai"
	"emotion-garden/internal/ai/gemini"
	"emotion-garden/internal/ai/mock"
	"emotion-garden/internal/ai/openai"
	"emotion-garden/internal/config"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func loadTestBundle(t *testing.T) *config.Bundle {
	t.Helper()
	root := repoRoot(t)
	t.Setenv("AI_MODEL_CONFIG", filepath.Join(root, "config", "models.yaml"))
	t.Setenv("AVATAR_CATALOG_CONFIG", filepath.Join(root, "config", "avatars.yaml"))
	t.Setenv("FLOWER_CATALOG_CONFIG", filepath.Join(root, "config", "flowers.yaml"))
	t.Setenv("RITUAL_CONFIG", filepath.Join(root, "config", "rituals.yaml"))
	t.Setenv("PROMPTS_DIR", filepath.Join(root, "prompts"))
	b, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("load config bundle: %v", err)
	}
	return b
}

func TestOpenAIAdapter_StreamingAndInvariants(t *testing.T) {
	t.Run("normal streaming with store=false and reasoning excluded (AI13)", func(t *testing.T) {
		var capturedBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/responses" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &capturedBody)

			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)

			fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_123\",\"model\":\"gpt-5.4-2026-03-05\",\"status\":\"in_progress\"}}\n\n")
			// Internal reasoning deltas MUST NOT appear in public text (AI13)
			fmt.Fprintf(w, "event: response.reasoning_text.delta\ndata: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"INTERNAL_SECRET_THOUGHT\"}\n\n")
			fmt.Fprintf(w, "event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"SUMMARY_SECRET\"}\n\n")
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"네가 느낀 서운함에 \"}\n\n")
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"진심으로 사과할게.\"}\n\n")
			fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_123\",\"model\":\"gpt-5.4-2026-03-05\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1000,\"input_tokens_details\":{\"cached_tokens\":200},\"output_tokens\":500,\"output_tokens_details\":{\"reasoning_tokens\":300},\"total_tokens\":1500}}}\n\n")
			flusher.Flush()
		}))
		defer srv.Close()

		adapter := openai.NewAdapter("sk-test-key", srv.URL, srv.Client())
		var streamed strings.Builder
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:            uuid.New(),
			ConversationKind: "release",
			Provider:         "openai",
			ModelAlias:       "openai_balanced",
			ModelID:          "gpt-5.4",
			SystemPrompt:     "시스템 프롬프트",
			ContextJSON:      []byte(`{"schema_version":"portable_transcript_v1"}`),
			Options: ai.GenerationOptions{
				MaxOutputTokens: 700,
				ReasoningLevel:  "low",
			},
		}, func(ev ai.ProviderEvent) error {
			if ev.Kind == ai.EventTextDelta {
				streamed.WriteString(ev.TextDelta)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if storeVal, ok := capturedBody["store"].(bool); !ok || storeVal != false {
			t.Fatalf("expected store=false in OpenAI request payload, got: %#v", capturedBody["store"])
		}
		if res.Status != ai.StatusSucceeded {
			t.Fatalf("expected succeeded, got %s", res.Status)
		}
		if strings.Contains(res.OutputText, "SECRET") || strings.Contains(streamed.String(), "SECRET") {
			t.Fatalf("reasoning text leaked into public output: %q", res.OutputText)
		}
		if res.OutputText != "네가 느낀 서운함에 진심으로 사과할게." {
			t.Fatalf("unexpected output text: %q", res.OutputText)
		}
		if res.Usage == nil || res.Usage.PublicOutputTokens == nil || *res.Usage.PublicOutputTokens != 200 {
			t.Fatalf("expected public_output_tokens=200 (500-300), got %+v", res.Usage)
		}
	})

	t.Run("socket closed without response.completed fails (AI06)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_cut\",\"model\":\"gpt-5.4\"}}\n\n")
			fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"중간에 끊긴 문장\"}\n\n")
			w.(http.Flusher).Flush()
			// Close handler without sending response.completed
		}))
		defer srv.Close()

		adapter := openai.NewAdapter("sk-test-key", srv.URL, srv.Client())
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:       uuid.New(),
			ModelID:     "gpt-5.4",
			ContextJSON: []byte(`{}`),
		}, func(ai.ProviderEvent) error { return nil })
		if err == nil || res.Status != ai.StatusFailed || res.ErrorCode != "STREAM_INTERRUPTED" {
			t.Fatalf("expected failed STREAM_INTERRUPTED on missing terminal event, got status=%s code=%s err=%v", res.Status, res.ErrorCode, err)
		}
	})

	t.Run("unexpected tool call rejected (AI13)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_tool\",\"model\":\"gpt-5.4\"}}\n\n")
			fmt.Fprintf(w, "event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{\\\"cmd\\\":\\\"ls\\\"}\"}\n\n")
			fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_tool\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
		}))
		defer srv.Close()

		adapter := openai.NewAdapter("sk-test-key", srv.URL, srv.Client())
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:       uuid.New(),
			ModelID:     "gpt-5.4",
			ContextJSON: []byte(`{}`),
		}, func(ai.ProviderEvent) error { return nil })
		if err == nil || res.Status != ai.StatusFailed || res.ErrorCode != "UNEXPECTED_MODALITY_OR_TOOL" {
			t.Fatalf("expected UNEXPECTED_MODALITY_OR_TOOL, got status=%s code=%s err=%v", res.Status, res.ErrorCode, err)
		}
	})

	t.Run("refusal classified as blocked (AI14)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_ref\",\"model\":\"gpt-5.4\"}}\n\n")
			fmt.Fprintf(w, "event: response.refusal.delta\ndata: {\"type\":\"response.refusal.delta\",\"delta\":\"cannot fulfill\"}\n\n")
			fmt.Fprintf(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ref\",\"model\":\"gpt-5.4\",\"status\":\"completed\"}}\n\n")
		}))
		defer srv.Close()

		adapter := openai.NewAdapter("sk-test-key", srv.URL, srv.Client())
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:       uuid.New(),
			ModelID:     "gpt-5.4",
			ContextJSON: []byte(`{}`),
		}, func(ai.ProviderEvent) error { return nil })
		if err != nil || res.Status != ai.StatusBlocked || res.ErrorCode != "PROVIDER_BLOCKED" {
			t.Fatalf("expected blocked PROVIDER_BLOCKED, got status=%s code=%s err=%v", res.Status, res.ErrorCode, err)
		}
	})

	t.Run("incompatible reasoning + temperature rejected (AI12)", func(t *testing.T) {
		adapter := openai.NewAdapter("sk-test-key", "http://127.0.0.1:1", nil)
		temp := 0.7
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:   uuid.New(),
			ModelID: "gpt-5.4",
			Options: ai.GenerationOptions{
				ReasoningLevel: "low",
				Temperature:    &temp,
			},
		}, func(ai.ProviderEvent) error { return nil })
		if err == nil || res.ErrorCode != "UNSUPPORTED_MODEL_OPTIONS" {
			t.Fatalf("expected UNSUPPORTED_MODEL_OPTIONS, got %+v err=%v", res, err)
		}
	})
}

func TestGeminiAdapter_StreamingAndInvariants(t *testing.T) {
	t.Run("normal SSE interaction with store=false and thought excluded (AI13)", func(t *testing.T) {
		var captured map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1beta/interactions" || r.URL.Query().Get("alt") != "sse" {
				t.Errorf("unexpected gemini endpoint: %s?%s", r.URL.Path, r.URL.RawQuery)
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &captured)

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: interaction.delta\ndata: {\"id\":\"int_99\",\"model\":\"gemini-3.8-flash\",\"delta\":{\"type\":\"thought\",\"text\":\"HIDDEN_GEMINI_THOUGHT\",\"thought\":true}}\n\n")
			fmt.Fprintf(w, "event: interaction.delta\ndata: {\"id\":\"int_99\",\"model\":\"gemini-3.8-flash\",\"delta\":{\"type\":\"text\",\"text\":\"오늘 하루 정말 고생 많았어.\"}}\n\n")
			fmt.Fprintf(w, "event: interaction.complete\ndata: {\"interaction\":{\"id\":\"int_99\",\"model\":\"gemini-3.8-flash\",\"status\":\"completed\",\"finish_reason\":\"STOP\",\"usage\":{\"total_input_tokens\":800,\"total_output_tokens\":120,\"total_thought_tokens\":90,\"total_tokens\":1010}}}\n\n")
			w.(http.Flusher).Flush()
		}))
		defer srv.Close()

		adapter := gemini.NewAdapter("gem-test-key", srv.URL+"/v1beta", srv.Client())
		var deltas strings.Builder
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:            uuid.New(),
			ConversationKind: "grow",
			ModelID:          "gemini-3.8-flash",
			SystemPrompt:     "시스템",
			ContextJSON:      []byte(`{}`),
			Options: ai.GenerationOptions{
				MaxOutputTokens: 700,
				ReasoningLevel:  "low",
			},
		}, func(ev ai.ProviderEvent) error {
			if ev.Kind == ai.EventTextDelta {
				deltas.WriteString(ev.TextDelta)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("gemini generate err: %v", err)
		}
		if storeVal, ok := captured["store"].(bool); !ok || storeVal != false {
			t.Fatalf("expected store=false in Gemini request, got %#v", captured["store"])
		}
		if strings.Contains(res.OutputText, "HIDDEN_GEMINI_THOUGHT") || strings.Contains(deltas.String(), "HIDDEN_GEMINI_THOUGHT") {
			t.Fatalf("gemini thought leaked: %q", res.OutputText)
		}
		if res.OutputText != "오늘 하루 정말 고생 많았어." {
			t.Fatalf("unexpected gemini output: %q", res.OutputText)
		}
		if res.Usage == nil || res.Usage.ReasoningTokens == nil || *res.Usage.ReasoningTokens != 90 {
			t.Fatalf("expected thought tokens=90, got %+v", res.Usage)
		}
	})

	t.Run("missing terminal event fails (AI06) and minimal thinking rejected (AI12)", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: interaction.delta\ndata: {\"id\":\"int_cut\",\"delta\":{\"type\":\"text\",\"text\":\"부분 텍스트\"}}\n\n")
			w.(http.Flusher).Flush()
		}))
		defer srv.Close()

		adapter := gemini.NewAdapter("gem-test-key", srv.URL+"/v1beta", srv.Client())
		res, err := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:   uuid.New(),
			ModelID: "gemini-3.8-flash",
		}, func(ai.ProviderEvent) error { return nil })
		if err == nil || res.Status != ai.StatusFailed || res.ErrorCode != "STREAM_INTERRUPTED" {
			t.Fatalf("expected STREAM_INTERRUPTED, got %+v err=%v", res, err)
		}

		resMin, errMin := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:   uuid.New(),
			ModelID: "gemini-3.8-flash",
			Options: ai.GenerationOptions{ReasoningLevel: "minimal"},
		}, func(ai.ProviderEvent) error { return nil })
		if errMin == nil || resMin.ErrorCode != "UNSUPPORTED_MODEL_OPTIONS" {
			t.Fatalf("expected UNSUPPORTED_MODEL_OPTIONS for minimal thinking, got %+v", resMin)
		}
	})

	t.Run("safety blocked and unexpected tool call (AI13, AI14)", func(t *testing.T) {
		srvBlocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: interaction.complete\ndata: {\"interaction\":{\"id\":\"int_blk\",\"status\":\"blocked\",\"finish_reason\":\"SAFETY\"}}\n\n")
		}))
		defer srvBlocked.Close()

		adapter := gemini.NewAdapter("gem-test-key", srvBlocked.URL+"/v1beta", srvBlocked.Client())
		resB, errB := adapter.Generate(context.Background(), ai.GenerationRequest{
			RunID:   uuid.New(),
			ModelID: "gemini-3.8-flash",
		}, func(ai.ProviderEvent) error { return nil })
		if errB != nil || resB.Status != ai.StatusBlocked {
			t.Fatalf("expected blocked status, got %+v err=%v", resB, errB)
		}

		srvTool := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: interaction.delta\ndata: {\"id\":\"int_tool\",\"delta\":{\"type\":\"function_call\",\"text\":\"exec\"}}\n\n")
			fmt.Fprintf(w, "event: interaction.complete\ndata: {\"interaction\":{\"id\":\"int_tool\",\"status\":\"completed\"}}\n\n")
		}))
		defer srvTool.Close()

		adapterTool := gemini.NewAdapter("gem-test-key", srvTool.URL+"/v1beta", srvTool.Client())
		resT, errT := adapterTool.Generate(context.Background(), ai.GenerationRequest{
			RunID:   uuid.New(),
			ModelID: "gemini-3.8-flash",
		}, func(ai.ProviderEvent) error { return nil })
		if errT == nil || resT.ErrorCode != "UNEXPECTED_MODALITY_OR_TOOL" {
			t.Fatalf("expected UNEXPECTED_MODALITY_OR_TOOL, got %+v err=%v", resT, errT)
		}
	})
}

func TestUsageCostEstimator_NoDoubleCountAndUnknownStatus(t *testing.T) {
	bundle := loadTestBundle(t)

	// OpenAI gpt-6.1-sol (openai_chat): input 1.50/1M, cached 0.375/1M, output 6.00/1M, reasoning_included_in_output = true
	oaiEntry, ok := bundle.Models.GetByAlias("openai_chat")
	if !ok {
		t.Fatal("missing openai_chat")
	}
	inTok := int64(1_000_000)
	cachedTok := int64(200_000)
	outTok := int64(500_000)    // includes 300_000 reasoning tokens
	reasonTok := int64(300_000) // MUST NOT be billed a second time!
	cost, status := ai.EstimateCost(bundle.Models, oaiEntry, &ai.Usage{
		InputTokens:       &inTok,
		CachedInputTokens: &cachedTok,
		OutputTokens:      &outTok,
		ReasoningTokens:   &reasonTok,
		Status:            "known",
	})
	if status != "known" || cost == nil {
		t.Fatalf("expected known cost, got status=%s cost=%v", status, cost)
	}
	// Expected: 0.8M * 1.50 + 0.2M * 0.375 + 0.5M * 6.00 = 1.20 + 0.075 + 3.00 = 4.275 USD
	expected := decimal.RequireFromString("4.275")
	if !cost.Equal(expected) {
		t.Fatalf("OpenAI cost mismatch (check double counting!): got %s, want %s", cost.String(), expected.String())
	}

	// Gemini gemini-3.8-flash (gemini_chat): reasoning_included_in_output = false -> reasoning tokens billed at 2.50/1M
	gemEntry, ok := bundle.Models.GetByAlias("gemini_chat")
	if !ok {
		t.Fatal("missing gemini_chat")
	}
	gemCost, _ := ai.EstimateCost(bundle.Models, gemEntry, &ai.Usage{
		InputTokens:     &inTok,
		OutputTokens:    &outTok,
		ReasoningTokens: &reasonTok,
		Status:          "known",
	})
	// 1.0M * 0.30 + 0.5M * 2.50 + 0.3M * 2.50 = 0.30 + 1.25 + 0.75 = 2.30 USD
	if gemCost == nil || !gemCost.Equal(decimal.RequireFromString("2.3")) {
		t.Fatalf("Gemini cost mismatch: got %v, want 2.30", gemCost)
	}

	// Missing usage -> nil cost & status="unknown" (never 0) (AI11)
	nilCost, unkStatus := ai.EstimateCost(bundle.Models, oaiEntry, &ai.Usage{Status: "unknown"})
	if nilCost != nil || unkStatus != "unknown" {
		t.Fatalf("expected nil cost and unknown status when usage missing, got cost=%v status=%s", nilCost, unkStatus)
	}
}

type evalScenario struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

func TestEvaluationFixtures_24Scenarios(t *testing.T) {
	bundle := loadTestBundle(t)
	fixturePath := filepath.Join(repoRoot(t), "tests", "fixtures", "eval_scenarios.json")
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read eval_scenarios.json: %v", err)
	}
	var doc struct {
		Scenarios []evalScenario `json:"scenarios"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal scenarios: %v", err)
	}
	if len(doc.Scenarios) != 24 {
		t.Fatalf("expected 24 scenarios (12 release + 12 grow), got %d", len(doc.Scenarios))
	}

	adapters := []ai.ChatProvider{
		mock.NewAdapter("mock_openai", 1*time.Millisecond, nil),
		mock.NewAdapter("mock_gemini", 1*time.Millisecond, nil),
	}

	for _, sc := range doc.Scenarios {
		for _, ad := range adapters {
			built, err := ai.BuildPortableContext(
				sc.Kind,
				"테스트상대",
				[]ai.TranscriptHistoryItem{{Speaker: "character", Text: "안녕."}},
				sc.Message,
				0,
				100,
				131072,
			)
			if err != nil {
				t.Fatalf("[%s] build transcript: %v", sc.ID, err)
			}

			sysPrompt := bundle.Prompts.GrowPrompt
			if sc.Kind == "release" {
				sysPrompt = bundle.Prompts.ReleasePrompt
			}

			res, err := ad.Generate(context.Background(), ai.GenerationRequest{
				RunID:            uuid.New(),
				ConversationKind: sc.Kind,
				Provider:         ad.Metadata().Provider,
				ModelID:          "eval-model",
				SystemPrompt:     sysPrompt,
				ContextJSON:      built.JSONBytes,
				ContextHash:      built.Hash,
			}, func(ai.ProviderEvent) error { return nil })
			if err != nil || res.Status != ai.StatusSucceeded {
				t.Fatalf("[%s/%s] failed: status=%s err=%v", sc.ID, ad.Metadata().Provider, res.Status, err)
			}
			if strings.TrimSpace(res.OutputText) == "" {
				t.Fatalf("[%s/%s] empty output", sc.ID, ad.Metadata().Provider)
			}
			if sc.Kind == "release" {
				// Release replies must acknowledge fault / apologize or gently ask what went wrong when ultra-short
				if !strings.Contains(res.OutputText, "미안") && !strings.Contains(res.OutputText, "사과") && !strings.Contains(res.OutputText, "인정") && !strings.Contains(res.OutputText, "들려") && !strings.Contains(res.OutputText, "알려") && !strings.Contains(res.OutputText, "짚어") {
					t.Fatalf("[%s/%s] release reply lacked apology/acknowledgment tone: %q", sc.ID, ad.Metadata().Provider, res.OutputText)
				}
			}
			if sc.Kind == "grow" {
				// Grow replies must have at most 1 question mark
				if qCount := strings.Count(res.OutputText, "?"); qCount > 1 {
					t.Fatalf("[%s/%s] grow reply asked %d questions (>1): %q", sc.ID, ad.Metadata().Provider, qCount, res.OutputText)
				}
			}
		}
	}
}

func TestBuildPortableContext_Limits_CHAT08(t *testing.T) {
	// 100 turns limit check
	_, err := ai.BuildPortableContext("release", "상대", nil, "새 메시지", 100, 100, 131072)
	if err == nil || !strings.Contains(err.Error(), "CONTEXT_LIMIT") {
		t.Fatalf("expected CONTEXT_LIMIT on >100 turns, got %v", err)
	}
	// 128KiB byte limit check
	bigHistory := []ai.TranscriptHistoryItem{{Speaker: "user", Text: strings.Repeat("가", 50000)}}
	_, errBytes := ai.BuildPortableContext("grow", "친구", bigHistory, "추가", 1, 100, 131072)
	if errBytes == nil || !strings.Contains(errBytes.Error(), "CONTEXT_LIMIT") {
		t.Fatalf("expected CONTEXT_LIMIT on >128KiB transcript, got %v", errBytes)
	}
}

