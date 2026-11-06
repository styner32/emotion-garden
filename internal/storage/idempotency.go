package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"emotion-garden/internal/apperr"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type IdempotencyRecord struct {
	OwnerID            uuid.UUID
	Scope              string
	Key                string
	RequestFingerprint string
	StatusCode         int
	ResponseJSON       []byte
	ResultIDs          map[string]string
}

// ComputeFingerprint digests the canonical request scope and body with HMAC-SHA256
// so raw conversation text is never persisted inside idempotency_records (Spec Section 22).
func ComputeFingerprint(secret, scope string, canonicalBody []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(scope))
	mac.Write([]byte{0})
	mac.Write(canonicalBody)
	return hex.EncodeToString(mac.Sum(nil))
}

// LockAndCheckIdempotency acquires an advisory transaction lock for (owner_id, scope, key)
// as the VERY FIRST step in lock ordering (Spec Section 21.1) and checks for an existing record.
func LockAndCheckIdempotency(ctx context.Context, tx pgx.Tx, ownerID uuid.UUID, scope, key, fingerprint string) (*IdempotencyRecord, error) {
	lockHash := sha256.Sum256([]byte(ownerID.String() + "|" + scope + "|" + key))
	var lockID int64
	for i := 0; i < 8; i++ {
		lockID = (lockID << 8) | int64(lockHash[i])
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, lockID); err != nil {
		return nil, err
	}

	var rec IdempotencyRecord
	var resultIDsRaw []byte
	err := tx.QueryRow(ctx, `
		SELECT owner_id, scope, key, request_fingerprint, status_code, response_json, result_ids
		FROM idempotency_records
		WHERE owner_id = $1 AND scope = $2 AND key = $3 AND expires_at > now()
		FOR UPDATE
	`, ownerID, scope, key).Scan(
		&rec.OwnerID,
		&rec.Scope,
		&rec.Key,
		&rec.RequestFingerprint,
		&rec.StatusCode,
		&rec.ResponseJSON,
		&resultIDsRaw,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if rec.RequestFingerprint != fingerprint {
		return nil, apperr.New(
			apperr.CodeIdempotencyKeyReused,
			409,
			"같은 요청 키로 다른 내용의 요청이 전달되었어요.",
			false,
			nil,
		)
	}
	_ = json.Unmarshal(resultIDsRaw, &rec.ResultIDs)
	return &rec, nil
}

func SaveIdempotencyRecord(
	ctx context.Context,
	tx pgx.Tx,
	ownerID uuid.UUID,
	scope, key, fingerprint string,
	statusCode int,
	responseJSON []byte,
	resultIDs map[string]string,
	retention time.Duration,
) error {
	if resultIDs == nil {
		resultIDs = map[string]string{}
	}
	idsRaw, err := json.Marshal(resultIDs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO idempotency_records (
			owner_id, scope, key, request_fingerprint, status_code, response_json, result_ids, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (owner_id, scope, key) DO UPDATE SET
			request_fingerprint = EXCLUDED.request_fingerprint,
			status_code = EXCLUDED.status_code,
			response_json = EXCLUDED.response_json,
			result_ids = EXCLUDED.result_ids,
			expires_at = EXCLUDED.expires_at
	`, ownerID, scope, key, fingerprint, statusCode, responseJSON, idsRaw, time.Now().UTC().Add(retention))
	return err
}
