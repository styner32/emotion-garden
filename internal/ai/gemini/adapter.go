package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"emotion-garden/internal/ai"
)

type Adapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewAdapter(apiKey, baseURL string, httpClient *http.Client) *Adapter {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Adapter{
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: httpClient,
	}
}

func (a *Adapter) Metadata() ai.AdapterMetadata {
	return ai.AdapterMetadata{
		Provider:       "gemini",
		SDKVersion:     "gemini-interactions-sse/v1.77-compat",
		AdapterVersion: "gemini_interactions_v1",
		APIRevision:    "v1beta-2026-11",
		IsMock:         false,
	}
}

type interactionRequest struct {
	Model             string            `json:"model"`
	Store             bool              `json:"store"`
	SystemInstruction string            `json:"system_instruction"`
	Input             string            `json:"input"`
	GenerationConfig  *generationConfig `json:"generation_config,omitempty"`
}

type generationConfig struct {
	MaxOutputTokens int64           `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	ThinkingConfig  *thinkingConfig `json:"thinking_config,omitempty"`
}

type thinkingConfig struct {
	ThinkingLevel string `json:"thinking_level"`
}

type sseEnvelope struct {
	EventType   string          `json:"event_type"`
	ID          string          `json:"id"`
	Model       string          `json:"model"`
	Status      string          `json:"status"`
	Delta       *sseDelta       `json:"delta,omitempty"`
	Interaction *sseInteraction `json:"interaction,omitempty"`
	Usage       *geminiUsage    `json:"usage,omitempty"`
	Error       *sseError       `json:"error,omitempty"`
}

type sseDelta struct {
	Type    string `json:"type"` // "text", "thought", "function_call", etc.
	Text    string `json:"text"`
	Thought bool   `json:"thought"`
}

type sseInteraction struct {
	ID           string       `json:"id"`
	Model        string       `json:"model"`
	Status       string       `json:"status"` // "completed", "incomplete", "blocked", "failed"
	FinishReason string       `json:"finish_reason"`
	Outputs      []sseOutput  `json:"outputs"`
	Usage        *geminiUsage `json:"usage,omitempty"`
}

type sseOutput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type geminiUsage struct {
	TotalInputTokens        *int64 `json:"total_input_tokens"`
	TotalOutputTokens       *int64 `json:"total_output_tokens"`
	TotalThoughtTokens      *int64 `json:"total_thought_tokens"`
	TotalCachedTokens       *int64 `json:"total_cached_tokens"`
	TotalCacheWriteTokens   *int64 `json:"total_cache_write_tokens"`
	TotalTokens             *int64 `json:"total_tokens"`
}

type sseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a *Adapter) Generate(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, error) {
	if a.apiKey == "" {
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			ErrorCode: "PROVIDER_NOT_CONFIGURED",
		}, errors.New("gemini api key not configured")
	}

	// Spec Section 12.2: Gemini 3.8 Flash does not support minimal thinking level
	if strings.EqualFold(req.Options.ReasoningLevel, "minimal") {
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			ErrorCode: "UNSUPPORTED_MODEL_OPTIONS",
		}, errors.New("minimal thinking level is unsupported for gemini-3.8-flash")
	}

	genCfg := &generationConfig{
		MaxOutputTokens: req.Options.MaxOutputTokens,
		Temperature:     req.Options.Temperature,
		TopP:            req.Options.TopP,
	}
	if req.Options.ReasoningLevel != "" {
		genCfg.ThinkingConfig = &thinkingConfig{
			ThinkingLevel: req.Options.ReasoningLevel,
		}
	}

	payload := interactionRequest{
		Model:             req.ModelID,
		Store:             false, // Spec Section 12.1: foreground + store=false
		SystemInstruction: req.SystemPrompt,
		Input:             string(req.ContextJSON),
		GenerationConfig:  genCfg,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "INTERNAL_ERROR"}, err
	}

	endpoint := fmt.Sprintf("%s/interactions?alt=sse", a.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "INTERNAL_ERROR"}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-goog-api-key", a.apiKey)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ai.GenerationResult{Status: ai.StatusCancelled, ErrorCode: "CANCELLED"}, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "PROVIDER_TIMEOUT"}, ctx.Err()
		}
		return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "PROVIDER_FAILED"}, err
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Goog-Request-Id")
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		code := "PROVIDER_FAILED"
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			code = "PROVIDER_AUTH_ERROR"
		case http.StatusTooManyRequests:
			code = "RATE_LIMITED"
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			code = "PROVIDER_BAD_REQUEST"
		}
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			RequestID: requestID,
			ErrorCode: code,
		}, fmt.Errorf("gemini http status %d", resp.StatusCode)
	}

	var collected strings.Builder
	var sawTerminal bool
	var terminalStatus string
	var finishReason string
	var reportedModel string
	var finalText string
	var finalUsage *ai.Usage
	var sawUnexpectedModality bool

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var eventName string
	var dataLines []string

	processFrame := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		joined := strings.Join(dataLines, "\n")
		curEvent := eventName
		eventName = ""
		dataLines = dataLines[:0]

		if strings.TrimSpace(joined) == "[DONE]" {
			return nil
		}

		var env sseEnvelope
		if err := json.Unmarshal([]byte(joined), &env); err != nil {
			return fmt.Errorf("invalid gemini sse json: %w", err)
		}

		evKind := curEvent
		if evKind == "" {
			evKind = env.EventType
		}
		if env.ID != "" {
			requestID = env.ID
		}
		if env.Model != "" {
			reportedModel = env.Model
		}
		if env.Usage != nil {
			finalUsage = convertGeminiUsage(env.Usage)
		}

		if env.Error != nil {
			sawTerminal = true
			terminalStatus = ai.StatusFailed
			finishReason = "error"
			return errors.New("gemini stream error")
		}

		if env.Delta != nil {
			dtype := strings.ToLower(env.Delta.Type)
			switch {
			case env.Delta.Thought || dtype == "thought" || dtype == "reasoning":
				// Exclude internal thought content from public answer (AI13)
			case dtype == "" || dtype == "text":
				if env.Delta.Text != "" {
					collected.WriteString(env.Delta.Text)
					if err := emit(ai.ProviderEvent{
						Kind:      ai.EventTextDelta,
						TextDelta: env.Delta.Text,
						RequestID: requestID,
					}); err != nil {
						return err
					}
				}
			default:
				sawUnexpectedModality = true
			}
		}

		if env.Interaction != nil {
			if env.Interaction.ID != "" {
				requestID = env.Interaction.ID
			}
			if env.Interaction.Model != "" {
				reportedModel = env.Interaction.Model
			}
			if env.Interaction.Usage != nil {
				finalUsage = convertGeminiUsage(env.Interaction.Usage)
			}
			if env.Interaction.FinishReason != "" {
				finishReason = env.Interaction.FinishReason
			}
			var outBuilder strings.Builder
			for _, o := range env.Interaction.Outputs {
				ot := strings.ToLower(o.Type)
				if ot == "thought" || ot == "reasoning" {
					continue
				}
				if ot != "" && ot != "text" {
					sawUnexpectedModality = true
					continue
				}
				outBuilder.WriteString(o.Text)
			}
			if outBuilder.Len() > 0 {
				finalText = outBuilder.String()
			}
			st := strings.ToLower(env.Interaction.Status)
			switch st {
			case "completed", "succeeded":
				sawTerminal = true
				terminalStatus = ai.StatusSucceeded
			case "incomplete":
				sawTerminal = true
				terminalStatus = ai.StatusIncomplete
			case "blocked", "safety_blocked":
				sawTerminal = true
				terminalStatus = ai.StatusBlocked
			case "failed", "cancelled":
				sawTerminal = true
				terminalStatus = ai.StatusFailed
			}
		}

		if evKind == "interaction.complete" && !sawTerminal {
			st := strings.ToLower(env.Status)
			if st == "" || st == "completed" || st == "succeeded" {
				sawTerminal = true
				terminalStatus = ai.StatusSucceeded
			}
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := processFrame(); err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					return ai.GenerationResult{Status: ai.StatusCancelled, RequestID: requestID, ErrorCode: "CANCELLED"}, ctx.Err()
				}
				return ai.GenerationResult{
					Status:    ai.StatusFailed,
					RequestID: requestID,
					ErrorCode: "PROVIDER_FAILED",
					Usage:     finalUsage,
				}, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(dataLines) > 0 {
		_ = processFrame()
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ai.GenerationResult{Status: ai.StatusCancelled, RequestID: requestID, ErrorCode: "CANCELLED"}, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ai.GenerationResult{Status: ai.StatusFailed, RequestID: requestID, ErrorCode: "PROVIDER_TIMEOUT"}, ctx.Err()
		}
		return ai.GenerationResult{Status: ai.StatusFailed, RequestID: requestID, ErrorCode: "STREAM_INTERRUPTED", Usage: finalUsage}, err
	}

	if sawUnexpectedModality {
		return ai.GenerationResult{
			Status:        ai.StatusFailed,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "unexpected_modality",
			ErrorCode:     "UNEXPECTED_MODALITY_OR_TOOL",
			Usage:         finalUsage,
		}, errors.New("unexpected gemini tool call or modality")
	}

	if !sawTerminal {
		return ai.GenerationResult{
			Status:        ai.StatusFailed,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "missing_terminal_event",
			ErrorCode:     "STREAM_INTERRUPTED",
			Usage:         finalUsage,
		}, errors.New("gemini stream closed without terminal event")
	}

	switch terminalStatus {
	case ai.StatusBlocked:
		return ai.GenerationResult{
			Status:        ai.StatusBlocked,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  finishReason,
			ErrorCode:     "PROVIDER_BLOCKED",
			Usage:         finalUsage,
		}, nil
	case ai.StatusIncomplete:
		return ai.GenerationResult{
			Status:        ai.StatusIncomplete,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  finishReason,
			ErrorCode:     "INCOMPLETE_OUTPUT",
			Usage:         finalUsage,
		}, nil
	case ai.StatusFailed:
		return ai.GenerationResult{
			Status:        ai.StatusFailed,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  finishReason,
			ErrorCode:     "PROVIDER_FAILED",
			Usage:         finalUsage,
		}, errors.New("gemini terminal failed")
	}

	if strings.TrimSpace(finalText) == "" {
		finalText = collected.String()
	}
	finalText = strings.TrimSpace(finalText)
	if finalText == "" {
		return ai.GenerationResult{
			Status:        ai.StatusIncomplete,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "empty_output",
			ErrorCode:     "EMPTY_OUTPUT",
			Usage:         finalUsage,
		}, nil
	}

	if finalUsage == nil {
		finalUsage = &ai.Usage{Status: "unknown"}
	} else {
		_ = emit(ai.ProviderEvent{Kind: ai.EventUsage, Usage: finalUsage, RequestID: requestID})
	}
	if finishReason == "" {
		finishReason = "completed"
	}
	_ = emit(ai.ProviderEvent{Kind: ai.EventTerminal, FinishReason: finishReason, RequestID: requestID})

	return ai.GenerationResult{
		Status:        ai.StatusSucceeded,
		OutputText:    finalText,
		ReportedModel: reportedModel,
		RequestID:     requestID,
		FinishReason:  finishReason,
		Usage:         finalUsage,
	}, nil
}

func convertGeminiUsage(u *geminiUsage) *ai.Usage {
	if u == nil || (u.TotalInputTokens == nil && u.TotalOutputTokens == nil) {
		return &ai.Usage{Status: "unknown"}
	}
	status := "known"
	if u.TotalInputTokens == nil || u.TotalOutputTokens == nil {
		status = "partial"
	}
	return &ai.Usage{
		InputTokens:        u.TotalInputTokens,
		OutputTokens:       u.TotalOutputTokens,
		PublicOutputTokens: u.TotalOutputTokens,
		ReasoningTokens:    u.TotalThoughtTokens,
		CachedInputTokens:  u.TotalCachedTokens,
		CacheWriteTokens:   u.TotalCacheWriteTokens,
		TotalTokens:        u.TotalTokens,
		Status:             status,
	}
}
