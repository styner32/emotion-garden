package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/identity"
	"emotion-garden/internal/storage"
)

const maxRequestBodyBytes = 128 * 1024 // 128 KiB (Spec Section 26.2)

type reqIDKey struct{}

func RequestIDFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(reqIDKey{}).(string); ok && s != "" {
		return s
	}
	return "req_unknown"
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "req_" + hex.EncodeToString(b[:])
}

type Meta struct {
	RequestID string `json:"request_id"`
	ServerNow string `json:"server_now"`
}

type SuccessEnvelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

type ErrorEnvelope struct {
	Error *apperr.Error `json:"error"`
	Meta  Meta          `json:"meta"`
}

func WriteJSON(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	env := SuccessEnvelope{
		Data: data,
		Meta: Meta{
			RequestID: RequestIDFromContext(r.Context()),
			ServerNow: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	_ = json.NewEncoder(w).Encode(env)
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	appErr, ok := apperr.As(err)
	if !ok {
		appErr = apperr.Internal(err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(appErr.HTTPStatus)
	env := ErrorEnvelope{
		Error: appErr,
		Meta: Meta{
			RequestID: RequestIDFromContext(r.Context()),
			ServerNow: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	_ = json.NewEncoder(w).Encode(env)
}

// DecodeStrictJSON enforces Content-Type: application/json, caps body size at 128KiB,
// rejects unknown fields (preventing client-supplied owner_id/cost/status spoofing),
// and returns the raw bytes for HMAC idempotency fingerprinting.
func DecodeStrictJSON[T any](w http.ResponseWriter, r *http.Request) (T, []byte, error) {
	var zero T
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return zero, nil, apperr.Validation("Content-Type은 application/json이어야 해요.", nil)
	}

	reader := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return zero, nil, apperr.Validation("요청 크기가 128KiB 한도를 초과했어요.", nil)
		}
		return zero, nil, apperr.Validation("요청 본문을 읽을 수 없어요.", nil)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out T
	if err := dec.Decode(&out); err != nil {
		return zero, nil, apperr.Validation("요청 JSON 형식이 올바르지 않거나 허용되지 않는 필드가 포함되어 있어요.", nil)
	}
	// Ensure no trailing garbage
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return zero, nil, apperr.Validation("요청 JSON 끝에 불필요한 데이터가 있어요.", nil)
	}
	return out, raw, nil
}

func ExtractIdemMeta(r *http.Request, secret, scope string, rawBody []byte) (character.IdemMeta, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		return character.IdemMeta{}, apperr.Validation(
			"변경 요청에는 1~128자의 Idempotency-Key 헤더가 필요해요.",
			map[string]string{"Idempotency-Key": "필수 헤더예요."},
		)
	}
	fp := storage.ComputeFingerprint(secret, scope, rawBody)
	return character.IdemMeta{
		Scope:       scope,
		Key:         key,
		Fingerprint: fp,
	}, nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Flush() {
	if fl, ok := sr.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (sr *statusRecorder) Unwrap() http.ResponseWriter {
	return sr.ResponseWriter
}

// RequestContextAndSafeLogger attaches request_id and logs ONLY safe operational metadata
// without ever logging request bodies, user text, prompts, or API keys (DATA05, Spec Section 26).
func RequestContextAndSafeLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqID := newRequestID()
			ctx := context.WithValue(r.Context(), reqIDKey{}, reqID)
			w.Header().Set("X-Request-Id", reqID)
			w.Header().Set("X-Content-Type-Options", "nosniff")

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			if strings.HasPrefix(r.URL.Path, "/api/") {
				logger.Info("api_request",
					"request_id", reqID,
					"method", r.Method,
					"path", r.URL.Path,
					"status", rec.status,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	}
}

// SecurityOriginGuard enforces Host and state-changing Origin allowlists without wildcard CORS (Spec Section 26).
func SecurityOriginGuard(publicOrigin string, extraOrigins []string) func(http.Handler) http.Handler {
	allowedOrigins := map[string]bool{}
	allowedHosts := map[string]bool{
		"localhost": true,
		"127.0.0.1": true,
	}

	addOrigin := func(orig string) {
		orig = strings.TrimRight(strings.TrimSpace(orig), "/")
		if orig == "" {
			return
		}
		allowedOrigins[orig] = true
		if u, err := url.Parse(orig); err == nil && u.Host != "" {
			allowedHosts[u.Host] = true
			if h, _, splitErr := net.SplitHostPort(u.Host); splitErr == nil {
				allowedHosts[h] = true
			}
		}
	}
	addOrigin(publicOrigin)
	for _, eo := range extraOrigins {
		addOrigin(eo)
	}

	allowLoopbackTunnel := os.Getenv("ALLOW_LOOPBACK_PREVIEW_TUNNEL") == "true"

	isLoopbackPeer := func(remoteAddr string) bool {
		h, _, err := net.SplitHostPort(remoteAddr)
		if err != nil {
			h = remoteAddr
		}
		ip := net.ParseIP(h)
		return ip != nil && ip.IsLoopback()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tunnelOK := allowLoopbackTunnel && isLoopbackPeer(r.RemoteAddr)
			hostOnly := r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil {
				hostOnly = h
			}
			if !tunnelOK && hostOnly != "" && !allowedHosts[r.Host] && !allowedHosts[hostOnly] {
				WriteError(w, r, apperr.New(apperr.CodeOriginForbidden, http.StatusForbidden, "허용되지 않은 호스트 요청이에요.", false, nil))
				return
			}

			if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				if origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/"); origin != "" {
					sameForwardedHost := false
					if tunnelOK {
						if u, err := url.Parse(origin); err == nil && u.Host != "" {
							fwdHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
							if u.Host == r.Host || (fwdHost != "" && u.Host == fwdHost) {
								sameForwardedHost = true
							}
						}
					}
					if !allowedOrigins[origin] && !sameForwardedHost {
						WriteError(w, r, apperr.New(apperr.CodeOriginForbidden, http.StatusForbidden, "허용되지 않은 출처(Origin)의 요청이에요.", false, nil))
						return
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ResolveIdentityMiddleware(resolver identity.IdentityResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := resolver.Resolve(r)
			if err != nil {
				WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(identity.WithPrincipal(r.Context(), principal)))
		})
	}
}
