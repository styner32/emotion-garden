package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	CodeValidationError          = "VALIDATION_ERROR"
	CodeUnsentDraft              = "UNSENT_DRAFT"
	CodeActiveConversationExists = "ACTIVE_CONVERSATION_EXISTS"
	CodeTurnUnresolved           = "TURN_UNRESOLVED"
	CodeVersionConflict          = "VERSION_CONFLICT"
	CodeIdempotencyKeyReused     = "IDEMPOTENCY_KEY_REUSED"
	CodeAlreadySelected          = "ALREADY_SELECTED"
	CodeContextLimit             = "CONTEXT_LIMIT"
	CodeProviderNotConfigured    = "PROVIDER_NOT_CONFIGURED"
	CodeModelUnavailable         = "MODEL_UNAVAILABLE"
	CodeRateLimited              = "RATE_LIMITED"
	CodeInvalidTransition        = "INVALID_TRANSITION"
	CodeActionInProgress         = "ACTION_IN_PROGRESS"
	CodeCellOccupied             = "CELL_OCCUPIED"
	CodeGardenBusy               = "GARDEN_BUSY"
	CodeGardenShrinkConflict     = "GARDEN_SHRINK_CONFLICT"
	CodeContentPurged            = "CONTENT_PURGED"
	CodeEventCursorExpired       = "EVENT_CURSOR_EXPIRED"
	CodeNotFound                 = "NOT_FOUND"
	CodeOriginForbidden          = "ORIGIN_FORBIDDEN"
	CodeInternalError            = "INTERNAL_ERROR"
)

type Error struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Retryable  bool           `json:"retryable"`
	Details    map[string]any `json:"details,omitempty"`
	HTTPStatus int            `json:"-"`
	Cause      error          `json:"-"`
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func New(code string, status int, message string, retryable bool, details map[string]any) *Error {
	return &Error{
		Code:       code,
		HTTPStatus: status,
		Message:    message,
		Retryable:  retryable,
		Details:    details,
	}
}

func Validation(message string, fields map[string]string) *Error {
	var details map[string]any
	if len(fields) > 0 {
		details = map[string]any{"fields": fields}
	}
	return New(CodeValidationError, http.StatusUnprocessableEntity, message, false, details)
}

func NotFound(message string) *Error {
	if message == "" {
		message = "요청한 항목을 찾을 수 없어요."
	}
	return New(CodeNotFound, http.StatusNotFound, message, false, nil)
}

func VersionConflict(latestVersion string) *Error {
	details := map[string]any{}
	if latestVersion != "" {
		details["latest_version"] = latestVersion
	}
	return New(CodeVersionConflict, http.StatusConflict, "다른 화면에서 상태가 먼저 바뀌었어요. 최신 상태를 불러왔습니다.", false, details)
}

func ContentPurged() *Error {
	return New(CodeContentPurged, http.StatusGone, "이미 마무리되어 원문이 정리된 이야기예요.", false, nil)
}

func Internal(cause error) *Error {
	return &Error{
		Code:       CodeInternalError,
		HTTPStatus: http.StatusInternalServerError,
		Message:    "일시적인 서버 오류가 발생했어요. 잠시 후 다시 시도해 주세요.",
		Retryable:  true,
		Cause:      cause,
	}
}

func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
