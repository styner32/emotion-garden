package openai

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"emotion-garden/internal/ai"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type Adapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewAdapter(apiKey, baseURL string, httpClient *http.Client) *Adapter {
	return &Adapter{
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    strings.TrimSpace(baseURL),
		httpClient: httpClient,
	}
}

func (a *Adapter) Metadata() ai.AdapterMetadata {
	return ai.AdapterMetadata{
		Provider:       "openai",
		SDKVersion:     "openai-go/v3.96.0",
		AdapterVersion: "openai_responses_v1",
		APIRevision:    "2026-11-05",
		IsMock:         false,
	}
}

func (a *Adapter) Generate(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, error) {
	if a.apiKey == "" {
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			ErrorCode: "PROVIDER_NOT_CONFIGURED",
		}, errors.New("openai api key not configured")
	}

	// Reject incompatible sampling + reasoning combination (Spec Section 12.2, AI12)
	if req.Options.ReasoningLevel != "" && (req.Options.Temperature != nil || req.Options.TopP != nil) {
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			ErrorCode: "UNSUPPORTED_MODEL_OPTIONS",
		}, errors.New("reasoning models do not accept temperature/top_p in v1 profile")
	}

	opts := []option.RequestOption{
		option.WithAPIKey(a.apiKey),
		option.WithMaxRetries(0), // Spec Section 14.5: disable automatic SDK retries
	}
	if a.baseURL != "" {
		opts = append(opts, option.WithBaseURL(a.baseURL))
	}
	if a.httpClient != nil {
		opts = append(opts, option.WithHTTPClient(a.httpClient))
	}

	client := openai.NewClient(opts...)

	params := responses.ResponseNewParams{
		Model:        shared.ResponsesModel(req.ModelID),
		Instructions: openai.String(req.SystemPrompt),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(string(req.ContextJSON)),
		},
		Store: openai.Bool(false),
	}
	if req.Options.MaxOutputTokens > 0 {
		params.MaxOutputTokens = openai.Int(req.Options.MaxOutputTokens)
	}
	if req.Options.ReasoningLevel != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort: shared.ReasoningEffort(req.Options.ReasoningLevel),
		}
	}
	if req.Options.Temperature != nil {
		params.Temperature = openai.Float(*req.Options.Temperature)
	}
	if req.Options.TopP != nil {
		params.TopP = openai.Float(*req.Options.TopP)
	}

	stream := client.Responses.NewStreaming(ctx, params)
	defer stream.Close()

	var collected strings.Builder
	var sawCompleted bool
	var sawIncomplete bool
	var sawRefusal bool
	var sawUnexpectedTool bool
	var reportedModel string
	var requestID string
	var finishReason string
	var finalText string
	var finalUsage *ai.Usage

	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "response.created":
			created := ev.AsResponseCreated()
			requestID = created.Response.ID
			reportedModel = string(created.Response.Model)

		case "response.output_text.delta":
			d := ev.AsResponseOutputTextDelta()
			if d.Delta != "" {
				collected.WriteString(d.Delta)
				if err := emit(ai.ProviderEvent{
					Kind:      ai.EventTextDelta,
					TextDelta: d.Delta,
					RequestID: requestID,
				}); err != nil {
					return ai.GenerationResult{Status: ai.StatusCancelled, ErrorCode: "CANCELLED"}, err
				}
			}

		case "response.refusal.delta", "response.refusal.done":
			sawRefusal = true

		case "response.reasoning_text.delta",
			"response.reasoning_text.done",
			"response.reasoning_summary_text.delta",
			"response.reasoning_summary_text.done",
			"response.reasoning_summary_part.added",
			"response.reasoning_summary_part.done":
			// Internal reasoning/thought tokens are strictly excluded from public output (AI13).

		case "response.function_call_arguments.delta",
			"response.function_call_arguments.done",
			"response.custom_tool_call_input.delta",
			"response.custom_tool_call_input.done",
			"response.mcp_call.in_progress",
			"response.shell_call_command.added",
			"response.image_generation_call.in_progress",
			"response.audio.delta":
			sawUnexpectedTool = true

		case "response.output_item.added":
			added := ev.AsResponseOutputItemAdded()
			if added.Item.Type != "" && added.Item.Type != "message" && added.Item.Type != "reasoning" {
				sawUnexpectedTool = true
			}

		case "response.completed":
			comp := ev.AsResponseCompleted()
			sawCompleted = true
			if comp.Response.ID != "" {
				requestID = comp.Response.ID
			}
			if comp.Response.Model != "" {
				reportedModel = string(comp.Response.Model)
			}
			finishReason = string(comp.Response.Status)
			if outTxt := comp.Response.OutputText(); strings.TrimSpace(outTxt) != "" {
				finalText = outTxt
			}
			finalUsage = convertOpenAIUsage(comp.Response.Usage)

		case "response.incomplete":
			inc := ev.AsResponseIncomplete()
			sawIncomplete = true
			if inc.Response.ID != "" {
				requestID = inc.Response.ID
			}
			finishReason = inc.Response.IncompleteDetails.Reason
			finalUsage = convertOpenAIUsage(inc.Response.Usage)

		case "response.failed":
			failed := ev.AsResponseFailed()
			code := string(failed.Response.Error.Code)
			if code == "" {
				code = "PROVIDER_FAILED"
			}
			finalUsage = convertOpenAIUsage(failed.Response.Usage)
			return ai.GenerationResult{
				Status:        ai.StatusFailed,
				ReportedModel: reportedModel,
				RequestID:     failed.Response.ID,
				FinishReason:  "failed",
				ErrorCode:     classifyOpenAIErrorCode(code),
				Usage:         finalUsage,
			}, errors.New("openai response failed")

		case "error":
			errEv := ev.AsError()
			code := errEv.Code
			if code == "" {
				code = "PROVIDER_ERROR"
			}
			return ai.GenerationResult{
				Status:        ai.StatusFailed,
				ReportedModel: reportedModel,
				RequestID:     requestID,
				FinishReason:  "error",
				ErrorCode:     classifyOpenAIErrorCode(code),
				Usage:         finalUsage,
			}, errors.New("openai stream error event")
		}
	}

	if err := stream.Err(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ai.GenerationResult{
				Status:    ai.StatusCancelled,
				RequestID: requestID,
				ErrorCode: "CANCELLED",
				Usage:     finalUsage,
			}, ctx.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ai.GenerationResult{
				Status:    ai.StatusFailed,
				RequestID: requestID,
				ErrorCode: "PROVIDER_TIMEOUT",
				Usage:     finalUsage,
			}, ctx.Err()
		}
		return ai.GenerationResult{
			Status:    ai.StatusFailed,
			RequestID: requestID,
			ErrorCode: classifyOpenAIError(err),
			Usage:     finalUsage,
		}, err
	}

	if sawUnexpectedTool {
		return ai.GenerationResult{
			Status:        ai.StatusFailed,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "unexpected_modality",
			ErrorCode:     "UNEXPECTED_MODALITY_OR_TOOL",
			Usage:         finalUsage,
		}, errors.New("unexpected tool call or non-text modality")
	}

	if sawRefusal {
		return ai.GenerationResult{
			Status:        ai.StatusBlocked,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "refusal",
			ErrorCode:     "PROVIDER_BLOCKED",
			Usage:         finalUsage,
		}, nil
	}

	if sawIncomplete {
		return ai.GenerationResult{
			Status:        ai.StatusIncomplete,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  finishReason,
			ErrorCode:     "INCOMPLETE_OUTPUT",
			Usage:         finalUsage,
		}, nil
	}

	// Spec Section 12.1 & AI06: Never treat socket close without explicit terminal event as success
	if !sawCompleted {
		return ai.GenerationResult{
			Status:        ai.StatusFailed,
			ReportedModel: reportedModel,
			RequestID:     requestID,
			FinishReason:  "missing_terminal_event",
			ErrorCode:     "STREAM_INTERRUPTED",
			Usage:         finalUsage,
		}, errors.New("stream closed without response.completed event")
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

	if finalUsage != nil {
		_ = emit(ai.ProviderEvent{Kind: ai.EventUsage, Usage: finalUsage, RequestID: requestID})
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

func convertOpenAIUsage(u responses.ResponseUsage) *ai.Usage {
	if !u.JSON.InputTokens.Valid() && !u.JSON.OutputTokens.Valid() {
		return &ai.Usage{Status: "unknown"}
	}
	inTok := u.InputTokens
	outTok := u.OutputTokens
	totTok := u.TotalTokens
	reasonTok := u.OutputTokensDetails.ReasoningTokens
	cachedTok := u.InputTokensDetails.CachedTokens
	cacheWriteTok := u.InputTokensDetails.CacheWriteTokens

	pubOut := outTok - reasonTok
	if pubOut < 0 {
		pubOut = 0
	}

	status := "known"
	if !u.JSON.InputTokens.Valid() || !u.JSON.OutputTokens.Valid() {
		status = "partial"
	}

	return &ai.Usage{
		InputTokens:        &inTok,
		OutputTokens:       &outTok,
		PublicOutputTokens: &pubOut,
		ReasoningTokens:    &reasonTok,
		CachedInputTokens:  &cachedTok,
		CacheWriteTokens:   &cacheWriteTok,
		TotalTokens:        &totTok,
		Status:             status,
	}
}

func classifyOpenAIError(err error) string {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "PROVIDER_AUTH_ERROR"
		case http.StatusTooManyRequests:
			return "RATE_LIMITED"
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return "PROVIDER_BAD_REQUEST"
		}
	}
	return "PROVIDER_FAILED"
}

func classifyOpenAIErrorCode(code string) string {
	lower := strings.ToLower(code)
	switch {
	case strings.Contains(lower, "rate_limit"):
		return "RATE_LIMITED"
	case strings.Contains(lower, "quota") || strings.Contains(lower, "billing"):
		return "PROVIDER_QUOTA_EXCEEDED"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "key"):
		return "PROVIDER_AUTH_ERROR"
	default:
		return "PROVIDER_FAILED"
	}
}
