package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"emotion-garden/internal/ai"
)

type HookFunc func(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, bool, error)

type Adapter struct {
	providerName string
	chunkDelay   time.Duration
	hook         HookFunc
}

func NewAdapter(providerName string, chunkDelay time.Duration, hook HookFunc) *Adapter {
	if chunkDelay <= 0 {
		chunkDelay = 35 * time.Millisecond
	}
	return &Adapter{
		providerName: providerName,
		chunkDelay:   chunkDelay,
		hook:         hook,
	}
}

func (a *Adapter) Metadata() ai.AdapterMetadata {
	return ai.AdapterMetadata{
		Provider:       a.providerName,
		SDKVersion:     "builtin-mock/v1.0.0",
		AdapterVersion: "mock_streaming_v1",
		APIRevision:    "mock-2026-11",
		IsMock:         true,
	}
}

func (a *Adapter) Generate(ctx context.Context, req ai.GenerationRequest, emit func(ai.ProviderEvent) error) (ai.GenerationResult, error) {
	if a.hook != nil {
		res, handled, err := a.hook(ctx, req, emit)
		if handled {
			return res, err
		}
	}

	var transcript ai.PortableTranscript
	if err := json.Unmarshal(req.ContextJSON, &transcript); err != nil {
		return ai.GenerationResult{Status: ai.StatusFailed, ErrorCode: "INVALID_CONTEXT"}, err
	}

	variant := 0
	if strings.Contains(a.providerName, "gemini") {
		variant = 1
	}
	text := composeRoleReply(transcript, variant)
	chunks := splitRunes(text, 10)
	reqID := fmt.Sprintf("mock_%s_%s", a.providerName, req.RunID.String()[:8])

	for _, ch := range chunks {
		select {
		case <-ctx.Done():
			return ai.GenerationResult{
				Status:    ai.StatusCancelled,
				RequestID: reqID,
				ErrorCode: "CANCELLED",
			}, ctx.Err()
		case <-time.After(a.chunkDelay):
		}
		if err := emit(ai.ProviderEvent{
			Kind:      ai.EventTextDelta,
			TextDelta: ch,
			RequestID: reqID,
		}); err != nil {
			return ai.GenerationResult{
				Status:    ai.StatusCancelled,
				RequestID: reqID,
				ErrorCode: "CANCELLED",
			}, err
		}
	}

	inTok := int64(len(req.ContextJSON)/3 + len(req.SystemPrompt)/3)
	outTok := int64(utf8.RuneCountInString(text))
	reasonTok := int64(18)
	totalTok := inTok + outTok + reasonTok
	usage := &ai.Usage{
		InputTokens:        &inTok,
		OutputTokens:       &outTok,
		PublicOutputTokens: &outTok,
		ReasoningTokens:    &reasonTok,
		TotalTokens:        &totalTok,
		Status:             "known",
	}

	_ = emit(ai.ProviderEvent{Kind: ai.EventUsage, Usage: usage, RequestID: reqID})
	_ = emit(ai.ProviderEvent{Kind: ai.EventTerminal, FinishReason: "completed", RequestID: reqID})

	return ai.GenerationResult{
		Status:        ai.StatusSucceeded,
		OutputText:    text,
		ReportedModel: req.ModelID + "-mock",
		RequestID:     reqID,
		FinishReason:  "completed",
		Usage:         usage,
	}, nil
}

func composeRoleReply(t ai.PortableTranscript, variant int) string {
	msg := strings.TrimSpace(t.CurrentUserMessage)
	summary := conciseAnchor(msg)
	turnIdx := len(t.History)

	if t.ConversationKind == "release" {
		if utf8.RuneCountInString(msg) <= 5 {
			if variant == 0 {
				return "내가 어떤 말이나 행동으로 너를 힘들게 했는지 조금만 더 알려줄래? 변명하지 않고 네 이야기를 먼저 제대로 들을게."
			}
			return "방금 말한 일에서 내가 구체적으로 어떤 잘못을 했는지 한 가지만 더 짚어줘. 내 입장을 내세우지 않고 인정할게."
		}

		if variant == 0 {
			if turnIdx <= 2 {
				return fmt.Sprintf("%s 일로 네 마음을 상하게 한 건 분명히 내 잘못이야. 네 입장과 그로 인해 겪은 불편을 가볍게 넘겨서 정말 미안해.", summary)
			}
			return fmt.Sprintf("앞선 일에 더해 %s 부분까지 네가 혼자 감당하게 만든 점을 인정해. 변명할 여지 없이 내가 생각이 짧았고 진심으로 사과할게.", summary)
		}
		if turnIdx <= 2 {
			return fmt.Sprintf("%s 상황에서 너를 배려하지 못하고 상처를 준 점은 전적으로 내 책임이야. 그 순간 네가 느꼈을 답답함과 속상함에 대해 진심으로 미안해.", summary)
		}
		return fmt.Sprintf("%s 결과까지 이어지게 해서 네 하루를 더 무겁게 만들었네. 내 행동을 합리화하지 않고 잘못을 그대로 받아들일게, 미안해.", summary)
	}

	// grow conversation: warm listening friend, max 1 gentle question, advice only when asked
	asksAdvice := strings.Contains(msg, "어떻게") || strings.Contains(msg, "방법") || strings.Contains(msg, "조언") || strings.Contains(msg, "어떡하")
	if asksAdvice {
		if variant == 0 {
			return fmt.Sprintf("%s 때문에 마음이 많이 복잡했겠다. 당장 모든 걸 해결하려 하기보다 지금 가장 부담이 큰 한 가지만 먼저 적어보거나 잠시 숨을 고르는 시간을 가져보면 어떨까? 두 가지 중 지금 네 마음에 조금 더 편한 쪽이 있어?", summary)
		}
		return fmt.Sprintf("%s 상황에서 어떻게 움직일지 고민되는 게 당연해 보여. 오늘은 핵심으로 전하고 싶은 한 문장만 가볍게 정리해 두고 내일 다시 살펴보는 작은 걸음부터 시작해 봐도 좋겠어. 지금 가장 걸리는 지점이 어디인지 더 이야기해 줄래?", summary)
	}

	if variant == 0 {
		return fmt.Sprintf("%s 일을 겪으면서 마음이 참 무겁고 지쳤을 것 같아. 지금 당장 답을 내지 않아도 괜찮으니, 그중에서 유독 마음에 오래 남는 장면이 있었는지 편하게 들려줄래?", summary)
	}
	return fmt.Sprintf("%s 이야기만 들어도 오늘 네가 얼마나 신경을 많이 썼을지 느껴져. 조급하게 정리하지 않아도 되니까 지금 마음속에 올라오는 감정을 천천히 더 말해줘도 괜찮아.", summary)
}

func conciseAnchor(msg string) string {
	clean := strings.Join(strings.Fields(msg), " ")
	runes := []rune(clean)
	if len(runes) == 0 {
		return "방금 말한"
	}
	if len(runes) > 28 {
		return fmt.Sprintf("'%s…'", string(runes[:28]))
	}
	return fmt.Sprintf("'%s'", string(runes))
}

func splitRunes(s string, size int) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	var out []string
	for i := 0; i < len(runes); i += size {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
