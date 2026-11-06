package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"emotion-garden/internal/apperr"
)

const SchemaPortableTranscriptV1 = "portable_transcript_v1"

type TranscriptCharacter struct {
	DisplayName string `json:"display_name"`
}

type TranscriptHistoryItem struct {
	Speaker string `json:"speaker"` // "character" or "user"
	Text    string `json:"text"`
}

type PortableTranscript struct {
	SchemaVersion      string                  `json:"schema_version"`
	ConversationKind   string                  `json:"conversation_kind"`
	Character          TranscriptCharacter     `json:"character"`
	History            []TranscriptHistoryItem `json:"history"`
	CurrentUserMessage string                  `json:"current_user_message"`
}

type BuiltContext struct {
	JSONBytes []byte
	Hash      string
}

// BuildPortableContext normalizes canonical public history and current user message
// into deterministic JSON bytes and validates turn count & byte limits BEFORE saving
// a new turn (Spec Section 13.1-13.2, CHAT08).
func BuildPortableContext(
	conversationKind string,
	displayName string,
	history []TranscriptHistoryItem,
	currentUserMessage string,
	existingUserTurnCount int,
	maxTurns int,
	maxBytes int,
) (BuiltContext, error) {
	if existingUserTurnCount+1 > maxTurns {
		return BuiltContext{}, apperr.New(
			apperr.CodeContextLimit,
			http.StatusUnprocessableEntity,
			"대화가 최대 길이에 도달했어요. 현재 이야기를 마무리하고 새 이야기를 시작해 주세요.",
			false,
			map[string]any{"max_turns": maxTurns},
		)
	}

	if history == nil {
		history = []TranscriptHistoryItem{}
	}
	payload := PortableTranscript{
		SchemaVersion:    SchemaPortableTranscriptV1,
		ConversationKind: conversationKind,
		Character: TranscriptCharacter{
			DisplayName: displayName,
		},
		History:            history,
		CurrentUserMessage: currentUserMessage,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return BuiltContext{}, apperr.Internal(err)
	}

	if len(raw) > maxBytes {
		return BuiltContext{}, apperr.New(
			apperr.CodeContextLimit,
			http.StatusUnprocessableEntity,
			"대화 문맥 크기가 한도를 넘었어요. 작성한 글을 유지한 채 현재 이야기를 마무리할 수 있어요.",
			false,
			map[string]any{"max_bytes": maxBytes, "actual_bytes": len(raw)},
		)
	}

	sum := sha256.Sum256(raw)
	return BuiltContext{
		JSONBytes: raw,
		Hash:      hex.EncodeToString(sum[:]),
	}, nil
}
