package character

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"emotion-garden/internal/apperr"
	"emotion-garden/internal/config"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var hexColorRegex = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type Appearance struct {
	SchemaVersion       string `json:"schema_version"`
	FacePreset          string `json:"face_preset"`
	HairStyle           string `json:"hair_style"`
	HairColor           string `json:"hair_color"`
	SkinTone            string `json:"skin_tone"`
	EyeStyle            string `json:"eye_style"`
	AcneLevel           string `json:"acne_level"`
	FrecklesLevel       string `json:"freckles_level"`
	WrinklesLevel       string `json:"wrinkles_level"`
	ClothingColor       string `json:"clothing_color"`
	AssetCatalogVersion string `json:"asset_catalog_version"`
}

type Character struct {
	ID                uuid.UUID  `json:"id"`
	Kind              string     `json:"kind"`
	Name              string     `json:"name"`
	Age               int16      `json:"age"`
	Gender            string     `json:"gender"`
	GenderCustom      *string    `json:"gender_custom,omitempty"`
	Appearance        Appearance `json:"appearance"`
	AppearanceVersion string     `json:"appearance_version"`
	Version           string     `json:"version"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Snapshot struct {
	ID                uuid.UUID  `json:"id"`
	Kind              string     `json:"kind"`
	Name              string     `json:"name"`
	Age               int16      `json:"age"`
	Gender            string     `json:"gender"`
	GenderCustom      *string    `json:"gender_custom,omitempty"`
	Appearance        Appearance `json:"appearance"`
	AppearanceVersion string     `json:"appearance_version"`
}

func (c Character) ToSnapshot() Snapshot {
	return Snapshot{
		ID:                c.ID,
		Kind:              c.Kind,
		Name:              c.Name,
		Age:               c.Age,
		Gender:            c.Gender,
		GenderCustom:      c.GenderCustom,
		Appearance:        c.Appearance,
		AppearanceVersion: c.AppearanceVersion,
	}
}

type CreateInput struct {
	Kind         string     `json:"kind"`
	Name         string     `json:"name"`
	Age          *int16     `json:"age,omitempty"`
	Gender       string     `json:"gender"`
	GenderCustom *string    `json:"gender_custom,omitempty"`
	Appearance   Appearance `json:"appearance"`
}

type UpdateInput struct {
	ExpectedVersion string      `json:"expected_version"`
	Name            *string     `json:"name,omitempty"`
	Age             *int16      `json:"age,omitempty"`
	Gender          *string     `json:"gender,omitempty"`
	GenderCustom    *string     `json:"gender_custom,omitempty"`
	Appearance      *Appearance `json:"appearance,omitempty"`
}

type ArchiveInput struct {
	ExpectedVersion string `json:"expected_version"`
}

type IdemMeta struct {
	Scope       string
	Key         string
	Fingerprint string
}

type Service struct {
	pool      *pgxpool.Pool
	catalog   *config.AvatarCatalog
	retention time.Duration
}

func NewService(pool *pgxpool.Pool, catalog *config.AvatarCatalog, retention time.Duration) *Service {
	return &Service{
		pool:      pool,
		catalog:   catalog,
		retention: retention,
	}
}

func (s *Service) ValidateAppearance(in Appearance) (Appearance, map[string]string) {
	errs := map[string]string{}
	out := in
	out.SchemaVersion = s.catalog.SchemaVersion
	out.AssetCatalogVersion = s.catalog.CatalogVersion

	if !hasPreset(s.catalog.FacePresets, out.FacePreset) {
		errs["appearance.face_preset"] = "지원하지 않는 얼굴 프리셋이에요."
	}
	if !hasSimpleOption(s.catalog.HairStyles, out.HairStyle) {
		errs["appearance.hair_style"] = "지원하지 않는 머리 모양이에요."
	}
	out.HairColor = strings.ToUpper(strings.TrimSpace(out.HairColor))
	if !hexColorRegex.MatchString(out.HairColor) {
		errs["appearance.hair_color"] = "머리 색상은 #RRGGBB 형식이어야 해요."
	}
	if !hasSkinTone(s.catalog.SkinTones, out.SkinTone) {
		errs["appearance.skin_tone"] = "지원하지 않는 피부색이에요."
	}
	if !hasSimpleOption(s.catalog.EyeStyles, out.EyeStyle) {
		errs["appearance.eye_style"] = "지원하지 않는 눈 스타일이에요."
	}
	if !hasSimpleOption(s.catalog.FeatureLevels, out.AcneLevel) {
		errs["appearance.acne_level"] = "여드름 단계 설정이 올바르지 않아요."
	}
	if !hasSimpleOption(s.catalog.FeatureLevels, out.FrecklesLevel) {
		errs["appearance.freckles_level"] = "주근깨 단계 설정이 올바르지 않아요."
	}
	if !hasSimpleOption(s.catalog.FeatureLevels, out.WrinklesLevel) {
		errs["appearance.wrinkles_level"] = "주름 단계 설정이 올바르지 않아요."
	}
	out.ClothingColor = strings.ToUpper(strings.TrimSpace(out.ClothingColor))
	if !hexColorRegex.MatchString(out.ClothingColor) {
		errs["appearance.clothing_color"] = "상의 색상은 #RRGGBB 형식이어야 해요."
	}
	if len(errs) == 0 {
		return out, nil
	}
	return out, errs
}

func (s *Service) List(ctx context.Context, ownerID uuid.UUID, includeArchived bool) ([]Character, error) {
	query := `
		SELECT id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
		       version, archived_at, created_at, updated_at
		FROM characters
		WHERE owner_id = $1 AND ($2::boolean OR archived_at IS NULL)
		ORDER BY created_at DESC, id DESC
	`
	rows, err := s.pool.Query(ctx, query, ownerID, includeArchived)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	defer rows.Close()

	out := []Character{}
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return nil, apperr.Internal(err)
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, ownerID, charID uuid.UUID) (Character, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
		       version, archived_at, created_at, updated_at
		FROM characters
		WHERE id = $1 AND owner_id = $2
	`, charID, ownerID)
	ch, err := scanCharacter(row)
	if err == pgx.ErrNoRows {
		return Character{}, apperr.NotFound("캐릭터를 찾을 수 없어요.")
	}
	if err != nil {
		return Character{}, apperr.Internal(err)
	}
	return ch, nil
}

func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, idem IdemMeta, in CreateInput) (Character, bool, error) {
	fields := map[string]string{}
	if in.Kind != "release" && in.Kind != "grow" {
		fields["kind"] = "역할은 release 또는 grow여야 해요."
	}
	name := strings.TrimSpace(in.Name)
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 1 || nameLen > 20 {
		fields["name"] = "이름은 앞뒤 공백을 제외하고 1~20자로 입력해 주세요."
	}
	age := int16(28)
	if in.Age != nil {
		age = *in.Age
	}
	if age < 1 || age > 120 {
		fields["age"] = "나이는 1~120 사이의 정수여야 해요."
	}

	var genderCustom *string
	switch in.Gender {
	case "female", "male", "unspecified":
	case "custom":
		if in.GenderCustom == nil {
			fields["gender_custom"] = "직접 설정 성별을 1~20자로 입력해 주세요."
		} else {
			gc := strings.TrimSpace(*in.GenderCustom)
			gcLen := utf8.RuneCountInString(gc)
			if gcLen < 1 || gcLen > 20 {
				fields["gender_custom"] = "직접 설정 성별은 1~20자 이내여야 해요."
			} else {
				genderCustom = &gc
			}
		}
	default:
		fields["gender"] = "지원하지 않는 성별 값이에요."
	}

	app, appErrs := s.ValidateAppearance(in.Appearance)
	for k, v := range appErrs {
		fields[k] = v
	}
	if len(fields) > 0 {
		return Character{}, false, apperr.Validation("캐릭터 입력값을 확인해 주세요.", fields)
	}

	type txOut struct {
		char     Character
		replayed bool
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (txOut, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return txOut{}, err
		}
		if rec != nil {
			var existing Character
			if err := json.Unmarshal(rec.ResponseJSON, &existing); err != nil {
				return txOut{}, err
			}
			return txOut{char: existing, replayed: true}, nil
		}

		appJSON, err := json.Marshal(app)
		if err != nil {
			return txOut{}, err
		}
		id := uuid.New()
		row := tx.QueryRow(ctx, `
			INSERT INTO characters (
				id, owner_id, kind, name, age, gender, gender_custom, appearance_json, appearance_version, version
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1)
			RETURNING id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
			          version, archived_at, created_at, updated_at
		`, id, ownerID, in.Kind, name, age, in.Gender, genderCustom, appJSON, s.catalog.CatalogVersion)

		created, err := scanCharacter(row)
		if err != nil {
			return txOut{}, err
		}
		respBytes, _ := json.Marshal(created)
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusCreated, respBytes, map[string]string{"character_id": created.ID.String()}, s.retention,
		); err != nil {
			return txOut{}, err
		}
		return txOut{char: created}, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Character{}, false, err
		}
		return Character{}, false, apperr.Internal(err)
	}
	return res.char, res.replayed, nil
}

func (s *Service) Update(ctx context.Context, ownerID, charID uuid.UUID, idem IdemMeta, in UpdateInput) (Character, error) {
	expVer, err := parseVersion(in.ExpectedVersion)
	if err != nil {
		return Character{}, apperr.Validation("expected_version 형식이 올바르지 않아요.", map[string]string{"expected_version": "정수 문자열이 필요해요."})
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (Character, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return Character{}, err
		}
		if rec != nil {
			var existing Character
			if err := json.Unmarshal(rec.ResponseJSON, &existing); err != nil {
				return Character{}, err
			}
			return existing, nil
		}

		row := tx.QueryRow(ctx, `
			SELECT id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
			       version, archived_at, created_at, updated_at
			FROM characters
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, charID, ownerID)
		cur, err := scanCharacter(row)
		if err == pgx.ErrNoRows {
			return Character{}, apperr.NotFound("캐릭터를 찾을 수 없어요.")
		}
		if err != nil {
			return Character{}, err
		}
		if cur.ArchivedAt != nil {
			return Character{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "보관된 캐릭터는 편집할 수 없어요.", false, nil)
		}
		if cur.Version != strconv.FormatInt(expVer, 10) {
			return Character{}, apperr.VersionConflict(cur.Version)
		}

		fields := map[string]string{}
		name := cur.Name
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if l := utf8.RuneCountInString(name); l < 1 || l > 20 {
				fields["name"] = "이름은 앞뒤 공백을 제외하고 1~20자로 입력해 주세요."
			}
		}
		age := cur.Age
		if in.Age != nil {
			age = *in.Age
			if age < 1 || age > 120 {
				fields["age"] = "나이는 1~120 사이의 정수여야 해요."
			}
		}
		gender := cur.Gender
		genderCustom := cur.GenderCustom
		if in.Gender != nil {
			gender = *in.Gender
			switch gender {
			case "female", "male", "unspecified":
				genderCustom = nil
			case "custom":
				if in.GenderCustom != nil {
					gc := strings.TrimSpace(*in.GenderCustom)
					if l := utf8.RuneCountInString(gc); l < 1 || l > 20 {
						fields["gender_custom"] = "직접 설정 성별은 1~20자 이내여야 해요."
					} else {
						genderCustom = &gc
					}
				} else if genderCustom == nil {
					fields["gender_custom"] = "직접 설정 성별을 입력해 주세요."
				}
			default:
				fields["gender"] = "지원하지 않는 성별 값이에요."
			}
		} else if in.GenderCustom != nil && gender == "custom" {
			gc := strings.TrimSpace(*in.GenderCustom)
			if l := utf8.RuneCountInString(gc); l < 1 || l > 20 {
				fields["gender_custom"] = "직접 설정 성별은 1~20자 이내여야 해요."
			} else {
				genderCustom = &gc
			}
		}

		app := cur.Appearance
		if in.Appearance != nil {
			validated, appErrs := s.ValidateAppearance(*in.Appearance)
			for k, v := range appErrs {
				fields[k] = v
			}
			app = validated
		}
		if len(fields) > 0 {
			return Character{}, apperr.Validation("캐릭터 수정 입력값을 확인해 주세요.", fields)
		}

		appJSON, err := json.Marshal(app)
		if err != nil {
			return Character{}, err
		}
		uRow := tx.QueryRow(ctx, `
			UPDATE characters
			SET name = $3,
			    age = $4,
			    gender = $5,
			    gender_custom = $6,
			    appearance_json = $7,
			    appearance_version = $8,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1 AND owner_id = $2
			RETURNING id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
			          version, archived_at, created_at, updated_at
		`, charID, ownerID, name, age, gender, genderCustom, appJSON, s.catalog.CatalogVersion)

		updated, err := scanCharacter(uRow)
		if err != nil {
			return Character{}, err
		}
		respBytes, _ := json.Marshal(updated)
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusOK, respBytes, map[string]string{"character_id": updated.ID.String()}, s.retention,
		); err != nil {
			return Character{}, err
		}
		return updated, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Character{}, err
		}
		return Character{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) Archive(ctx context.Context, ownerID, charID uuid.UUID, idem IdemMeta, in ArchiveInput) (Character, error) {
	expVer, err := parseVersion(in.ExpectedVersion)
	if err != nil {
		return Character{}, apperr.Validation("expected_version 형식이 올바르지 않아요.", map[string]string{"expected_version": "정수 문자열이 필요해요."})
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (Character, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return Character{}, err
		}
		if rec != nil {
			var existing Character
			if err := json.Unmarshal(rec.ResponseJSON, &existing); err != nil {
				return Character{}, err
			}
			return existing, nil
		}

		row := tx.QueryRow(ctx, `
			SELECT id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
			       version, archived_at, created_at, updated_at
			FROM characters
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, charID, ownerID)
		cur, err := scanCharacter(row)
		if err == pgx.ErrNoRows {
			return Character{}, apperr.NotFound("캐릭터를 찾을 수 없어요.")
		}
		if err != nil {
			return Character{}, err
		}
		if cur.Version != strconv.FormatInt(expVer, 10) {
			return Character{}, apperr.VersionConflict(cur.Version)
		}
		if cur.ArchivedAt != nil {
			respBytes, _ := json.Marshal(cur)
			_ = storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention)
			return cur, nil
		}

		uRow := tx.QueryRow(ctx, `
			UPDATE characters
			SET archived_at = now(),
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1 AND owner_id = $2
			RETURNING id, kind, name, age, gender, gender_custom, appearance_json, appearance_version,
			          version, archived_at, created_at, updated_at
		`, charID, ownerID)
		archived, err := scanCharacter(uRow)
		if err != nil {
			return Character{}, err
		}
		respBytes, _ := json.Marshal(archived)
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusOK, respBytes, map[string]string{"character_id": archived.ID.String()}, s.retention,
		); err != nil {
			return Character{}, err
		}
		return archived, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return Character{}, err
		}
		return Character{}, apperr.Internal(err)
	}
	return res, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCharacter(r rowScanner) (Character, error) {
	var ch Character
	var appRaw []byte
	var ver int64
	err := r.Scan(
		&ch.ID,
		&ch.Kind,
		&ch.Name,
		&ch.Age,
		&ch.Gender,
		&ch.GenderCustom,
		&appRaw,
		&ch.AppearanceVersion,
		&ver,
		&ch.ArchivedAt,
		&ch.CreatedAt,
		&ch.UpdatedAt,
	)
	if err != nil {
		return Character{}, err
	}
	ch.Version = strconv.FormatInt(ver, 10)
	if err := json.Unmarshal(appRaw, &ch.Appearance); err != nil {
		return Character{}, err
	}
	return ch, nil
}

func parseVersion(v string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation("유효한 버전 문자열이 아니에요.", nil)
	}
	return n, nil
}

func hasPreset(list []config.FacePreset, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

func hasSimpleOption(list []config.SimpleOption, id string) bool {
	for _, o := range list {
		if o.ID == id {
			return true
		}
	}
	return false
}

func hasSkinTone(list []config.SkinTone, id string) bool {
	for _, t := range list {
		if t.ID == id {
			return true
		}
	}
	return false
}
