package ai

import (
	"context"

	"github.com/google/uuid"
)

const (
	EventTextDelta = "text_delta"
	EventUsage     = "usage"
	EventTerminal  = "terminal"

	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusStreaming   = "streaming"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusIncomplete  = "incomplete"
	StatusBlocked     = "blocked"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"
)

type GenerationOptions struct {
	MaxOutputTokens int64    `json:"max_output_tokens"`
	ReasoningLevel  string   `json:"reasoning_level,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
}

type GenerationRequest struct {
	RunID            uuid.UUID
	ConversationKind string
	Provider         string
	ModelAlias       string
	ModelID          string
	SystemPrompt     string
	ContextJSON      []byte
	ContextHash      string
	Options          GenerationOptions
}

type Usage struct {
	InputTokens        *int64 `json:"input_tokens,omitempty"`
	PublicOutputTokens *int64 `json:"public_output_tokens,omitempty"`
	OutputTokens       *int64 `json:"output_tokens,omitempty"`
	ReasoningTokens    *int64 `json:"reasoning_tokens,omitempty"`
	CachedInputTokens  *int64 `json:"cached_input_tokens,omitempty"`
	CacheWriteTokens   *int64 `json:"cache_write_tokens,omitempty"`
	TotalTokens        *int64 `json:"total_tokens,omitempty"`
	Status             string `json:"status"` // known, partial, unknown
}

type ProviderEvent struct {
	Kind         string
	TextDelta    string
	Usage        *Usage
	FinishReason string
	RequestID    string
}

type GenerationResult struct {
	Status        string // succeeded, incomplete, blocked, failed
	OutputText    string
	ReportedModel string
	RequestID     string
	FinishReason  string
	ErrorCode     string
	Usage         *Usage
}

type AdapterMetadata struct {
	Provider       string
	SDKVersion     string
	AdapterVersion string
	APIRevision    string
	IsMock         bool
}

type ChatProvider interface {
	Metadata() AdapterMetadata
	Generate(ctx context.Context, req GenerationRequest, emit func(ProviderEvent) error) (GenerationResult, error)
}
