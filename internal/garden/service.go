package garden

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"emotion-garden/internal/apperr"
	"emotion-garden/internal/character"
	"emotion-garden/internal/config"
	"emotion-garden/internal/ritual"
	"emotion-garden/internal/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OccupancyDTO struct {
	Row            int16           `json:"row"`
	Col            int16           `json:"col"`
	State          string          `json:"state"` // reserved, planted
	FlowerID       uuid.UUID       `json:"flower_id"`
	RitualID       uuid.UUID       `json:"ritual_id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	SpeciesID      string          `json:"species_id"`
	SpeciesName    string          `json:"species_name"`
	StyleSeed      string          `json:"style_seed"`
	VisualSnapshot json.RawMessage `json:"visual_snapshot"`
	KeptLine       string          `json:"kept_line,omitempty"`
	ReservedAt     time.Time       `json:"reserved_at"`
	PlantedAt      *time.Time      `json:"planted_at,omitempty"`
	PlantedDateLocal string        `json:"planted_date_local,omitempty"`
}

type GardenView struct {
	ID          uuid.UUID      `json:"id"`
	Cols        int16          `json:"cols"`
	Rows        int16          `json:"rows"`
	MaxCols     int16          `json:"max_cols"`
	MaxRows     int16          `json:"max_rows"`
	Version     string         `json:"version"`
	Timezone    string         `json:"timezone"`
	HasReserved bool           `json:"has_reserved"`
	Occupancies []OccupancyDTO `json:"occupancies"`
}

type ResizeInput struct {
	ExpectedVersion string `json:"expected_version"`
	Cols            int16  `json:"cols"`
	Rows            int16  `json:"rows"`
}

type UpdateFlowerInput struct {
	ExpectedVersion string  `json:"expected_version"`
	KeptLine        *string `json:"kept_line,omitempty"`
	PendingRow      *int16  `json:"pending_row,omitempty"`
	PendingCol      *int16  `json:"pending_col,omitempty"`
	ClearPending    bool    `json:"clear_pending,omitempty"`
}

type BeginPlantingInput struct {
	Row                         int16  `json:"row"`
	Col                         int16  `json:"col"`
	ExpectedGardenVersion       string `json:"expected_garden_version"`
	ExpectedConversationVersion string `json:"expected_conversation_version"`
	ExpectedFlowerVersion       string `json:"expected_flower_version"`
}

type BeginPlantingResult struct {
	GardenVersion       string                  `json:"garden_version"`
	ConversationID      uuid.UUID               `json:"conversation_id"`
	ConversationStage   string                  `json:"conversation_stage"`
	ConversationVersion string                  `json:"conversation_version"`
	Flower              ritual.FlowerSummaryDTO `json:"flower"`
	Ritual              ritual.RitualView       `json:"ritual"`
}

type DiscardResult struct {
	ConversationID      uuid.UUID               `json:"conversation_id"`
	ConversationStage   string                  `json:"conversation_stage"`
	ConversationVersion string                  `json:"conversation_version"`
	CloseReason         string                  `json:"close_reason"`
	Purged              bool                    `json:"purged"`
	Flower              ritual.FlowerSummaryDTO `json:"flower"`
}

type Service struct {
	pool      *pgxpool.Pool
	rituals   *config.RitualCatalog
	ritualSvc *ritual.Service
	maxCols   int16
	maxRows   int16
	retention time.Duration
}

func NewService(
	pool *pgxpool.Pool,
	rituals *config.RitualCatalog,
	ritualSvc *ritual.Service,
	maxCols, maxRows int16,
	retention time.Duration,
) *Service {
	return &Service{
		pool:      pool,
		rituals:   rituals,
		ritualSvc: ritualSvc,
		maxCols:   maxCols,
		maxRows:   maxRows,
		retention: retention,
	}
}

func (s *Service) GetGarden(ctx context.Context, ownerID uuid.UUID) (GardenView, error) {
	var gID uuid.UUID
	var cols, rows int16
	var ver int64
	var tz string
	err := s.pool.QueryRow(ctx, `
		SELECT g.id, g.cols, g.rows, g.version, u.timezone
		FROM gardens g
		JOIN app_users u ON u.id = g.owner_id
		WHERE g.owner_id = $1
	`, ownerID).Scan(&gID, &cols, &rows, &ver, &tz)
	if err == pgx.ErrNoRows {
		return GardenView{}, apperr.NotFound("꽃밭 정보를 찾을 수 없어요.")
	}
	if err != nil {
		return GardenView{}, apperr.Internal(err)
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone("KST", 9*3600)
	}

	qRows, err := s.pool.Query(ctx, `
		SELECT o.row_no, o.col_no, o.state, o.flower_id, o.ritual_id,
		       f.source_conversation_id, f.species_id, f.species_name, f.style_seed,
		       f.visual_snapshot, f.kept_line, o.reserved_at, o.planted_at
		FROM garden_occupancies o
		JOIN flower_gifts f ON f.id = o.flower_id AND f.owner_id = o.owner_id
		WHERE o.garden_id = $1 AND o.owner_id = $2
		ORDER BY o.row_no ASC, o.col_no ASC
	`, gID, ownerID)
	if err != nil {
		return GardenView{}, apperr.Internal(err)
	}
	defer qRows.Close()

	occs := []OccupancyDTO{}
	hasReserved := false
	for qRows.Next() {
		var oc OccupancyDTO
		var seed int64
		var visRaw []byte
		if err := qRows.Scan(
			&oc.Row, &oc.Col, &oc.State, &oc.FlowerID, &oc.RitualID,
			&oc.ConversationID, &oc.SpeciesID, &oc.SpeciesName, &seed,
			&visRaw, &oc.KeptLine, &oc.ReservedAt, &oc.PlantedAt,
		); err != nil {
			return GardenView{}, apperr.Internal(err)
		}
		oc.StyleSeed = strconv.FormatInt(seed, 10)
		oc.VisualSnapshot = json.RawMessage(visRaw)
		if oc.State == "reserved" {
			hasReserved = true
			oc.KeptLine = "" // Only show kept_line on planted flowers in garden view
		}
		if oc.PlantedAt != nil {
			oc.PlantedDateLocal = oc.PlantedAt.In(loc).Format("2006. 01. 02")
		}
		occs = append(occs, oc)
	}
	if err := qRows.Err(); err != nil {
		return GardenView{}, apperr.Internal(err)
	}

	return GardenView{
		ID:          gID,
		Cols:        cols,
		Rows:        rows,
		MaxCols:     s.maxCols,
		MaxRows:     s.maxRows,
		Version:     strconv.FormatInt(ver, 10),
		Timezone:    tz,
		HasReserved: hasReserved,
		Occupancies: occs,
	}, nil
}

func (s *Service) ResizeGarden(ctx context.Context, ownerID uuid.UUID, idem character.IdemMeta, in ResizeInput) (GardenView, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return GardenView{}, err
	}
	fields := map[string]string{}
	if in.Cols < 1 || in.Cols > s.maxCols {
		fields["cols"] = "가로는 1~" + strconv.Itoa(int(s.maxCols)) + "칸 사이여야 해요."
	}
	if in.Rows < 1 || in.Rows > s.maxRows {
		fields["rows"] = "세로는 1~" + strconv.Itoa(int(s.maxRows)) + "칸 사이여야 해요."
	}
	if len(fields) > 0 {
		return GardenView{}, apperr.Validation("꽃밭 크기 범위를 확인해 주세요.", fields)
	}

	_, err = storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (bool, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return false, err
		}
		if rec != nil {
			return true, nil
		}

		var gID uuid.UUID
		var curCols, curRows int16
		var curVer int64
		if err := tx.QueryRow(ctx, `
			SELECT id, cols, rows, version FROM gardens WHERE owner_id = $1 FOR UPDATE
		`, ownerID).Scan(&gID, &curCols, &curRows, &curVer); err != nil {
			return false, err
		}
		if curVer != expVer {
			return false, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}

		var reservedCount int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM garden_occupancies WHERE garden_id = $1 AND state = 'reserved'
		`, gID).Scan(&reservedCount); err != nil {
			return false, err
		}
		if reservedCount > 0 {
			return false, apperr.New(
				apperr.CodeGardenBusy,
				http.StatusConflict,
				"지금 심고 있는 꽃을 마무리한 뒤에 크기를 바꿀 수 있어요.",
				false,
				nil,
			)
		}

		var maxCol, maxRow int16
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(MAX(col_no), 0), COALESCE(MAX(row_no), 0)
			FROM garden_occupancies
			WHERE garden_id = $1 AND state = 'planted'
		`, gID).Scan(&maxCol, &maxRow); err != nil {
			return false, err
		}
		if in.Cols < maxCol || in.Rows < maxRow {
			minC := maxCol
			if minC < 1 {
				minC = 1
			}
			minR := maxRow
			if minR < 1 {
				minR = 1
			}
			return false, apperr.New(
				apperr.CodeGardenShrinkConflict,
				http.StatusConflict,
				"꽃이 있는 자리는 줄일 수 없어요.",
				false,
				map[string]any{
					"minimum_cols": minC,
					"minimum_rows": minR,
				},
			)
		}

		newVer := curVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE gardens
			SET cols = $2, rows = $3, version = $4, updated_at = now()
			WHERE id = $1
		`, gID, in.Cols, in.Rows, newVer); err != nil {
			return false, err
		}

		// Clear unreserved pending selection if outside new bounds (Spec Section 21.6)
		if _, err := tx.Exec(ctx, `
			UPDATE flower_gifts
			SET pending_row = NULL,
			    pending_col = NULL,
			    version = version + 1,
			    updated_at = now()
			WHERE owner_id = $1
			  AND status = 'available'
			  AND (pending_col > $2 OR pending_row > $3)
		`, ownerID, in.Cols, in.Rows); err != nil {
			return false, err
		}

		respBytes, _ := json.Marshal(map[string]any{"version": strconv.FormatInt(newVer, 10)})
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return false, err
		}
		return false, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return GardenView{}, err
		}
		return GardenView{}, apperr.Internal(err)
	}
	return s.GetGarden(ctx, ownerID)
}

func (s *Service) UpdateFlowerGift(ctx context.Context, ownerID, flowerID uuid.UUID, idem character.IdemMeta, in UpdateFlowerInput) (ritual.FlowerSummaryDTO, error) {
	expVer, err := parsePositiveInt64(in.ExpectedVersion, "expected_version")
	if err != nil {
		return ritual.FlowerSummaryDTO{}, err
	}

	if in.KeptLine != nil {
		trimmed := strings.TrimSpace(*in.KeptLine)
		if utf8.RuneCountInString(trimmed) > 80 {
			return ritual.FlowerSummaryDTO{}, apperr.Validation("남길 한 문장은 80자 이내로 입력해 주세요.", map[string]string{"kept_line": "80자 이하로 입력해 주세요."})
		}
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (ritual.FlowerSummaryDTO, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}
		if rec != nil {
			var replay ritual.FlowerSummaryDTO
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return ritual.FlowerSummaryDTO{}, err
			}
			return replay, nil
		}

		var cols, rows int16
		if err := tx.QueryRow(ctx, `SELECT cols, rows FROM gardens WHERE owner_id = $1`, ownerID).Scan(&cols, &rows); err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}

		var convID uuid.UUID
		var status, curKept string
		var pRow, pCol *int16
		var curVer int64
		err = tx.QueryRow(ctx, `
			SELECT source_conversation_id, status, kept_line, pending_row, pending_col, version
			FROM flower_gifts
			WHERE id = $1 AND owner_id = $2
			FOR UPDATE
		`, flowerID, ownerID).Scan(&convID, &status, &curKept, &pRow, &pCol, &curVer)
		if err == pgx.ErrNoRows {
			return ritual.FlowerSummaryDTO{}, apperr.NotFound("꽃 선물을 찾을 수 없어요.")
		}
		if err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}
		if curVer != expVer {
			return ritual.FlowerSummaryDTO{}, apperr.VersionConflict(strconv.FormatInt(curVer, 10))
		}
		if status != "available" {
			return ritual.FlowerSummaryDTO{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "심기가 시작된 뒤에는 문장이나 자리를 바꿀 수 없어요.", false, nil)
		}

		nextKept := curKept
		if in.KeptLine != nil {
			nextKept = strings.TrimSpace(*in.KeptLine)
		}
		nextRow := pRow
		nextCol := pCol
		if in.ClearPending {
			nextRow = nil
			nextCol = nil
		} else if in.PendingRow != nil || in.PendingCol != nil {
			if in.PendingRow == nil || in.PendingCol == nil || *in.PendingRow < 1 || *in.PendingRow > rows || *in.PendingCol < 1 || *in.PendingCol > cols {
				return ritual.FlowerSummaryDTO{}, apperr.Validation("선택한 좌표가 현재 꽃밭 범위를 벗어났어요.", nil)
			}
			nextRow = in.PendingRow
			nextCol = in.PendingCol
		}

		if _, err := tx.Exec(ctx, `
			UPDATE flower_gifts
			SET kept_line = $2,
			    pending_row = $3,
			    pending_col = $4,
			    version = version + 1,
			    updated_at = now()
			WHERE id = $1
		`, flowerID, nextKept, nextRow, nextCol); err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}

		var out ritual.FlowerSummaryDTO
		var seed, ver int64
		var visRaw []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, species_id, species_name, style_seed, visual_snapshot,
			       status, kept_line, pending_row, pending_col, planted_row, planted_col, planted_at, version
			FROM flower_gifts WHERE id = $1
		`, flowerID).Scan(
			&out.ID, &out.SpeciesID, &out.SpeciesName, &seed, &visRaw,
			&out.Status, &out.KeptLine, &out.PendingRow, &out.PendingCol,
			&out.PlantedRow, &out.PlantedCol, &out.PlantedAt, &ver,
		); err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}
		out.StyleSeed = strconv.FormatInt(seed, 10)
		out.Version = strconv.FormatInt(ver, 10)
		out.VisualSnapshot = json.RawMessage(visRaw)

		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return ritual.FlowerSummaryDTO{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return ritual.FlowerSummaryDTO{}, err
		}
		return ritual.FlowerSummaryDTO{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) BeginPlanting(ctx context.Context, ownerID, flowerID uuid.UUID, idem character.IdemMeta, in BeginPlantingInput) (BeginPlantingResult, error) {
	expGardenVer, err := parsePositiveInt64(in.ExpectedGardenVersion, "expected_garden_version")
	if err != nil {
		return BeginPlantingResult{}, err
	}
	expConvVer, err := parsePositiveInt64(in.ExpectedConversationVersion, "expected_conversation_version")
	if err != nil {
		return BeginPlantingResult{}, err
	}
	expFlowerVer, err := parsePositiveInt64(in.ExpectedFlowerVersion, "expected_flower_version")
	if err != nil {
		return BeginPlantingResult{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (BeginPlantingResult, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return BeginPlantingResult{}, err
		}
		if rec != nil {
			var replay BeginPlantingResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return BeginPlantingResult{}, err
			}
			return replay, nil
		}

		// Lookup source_conversation_id first, then acquire locks strictly in canonical order:
		// gardens -> conversations -> flower_gifts -> garden_occupancies (Spec Section 21.1 & 21.5)
		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT source_conversation_id FROM flower_gifts WHERE id = $1 AND owner_id = $2
		`, flowerID, ownerID).Scan(&convID); err != nil {
			if err == pgx.ErrNoRows {
				return BeginPlantingResult{}, apperr.NotFound("꽃 선물을 찾을 수 없어요.")
			}
			return BeginPlantingResult{}, err
		}

		var gardenID uuid.UUID
		var gCols, gRows int16
		var gVer int64
		if err := tx.QueryRow(ctx, `
			SELECT id, cols, rows, version FROM gardens WHERE owner_id = $1 FOR UPDATE
		`, ownerID).Scan(&gardenID, &gCols, &gRows, &gVer); err != nil {
			return BeginPlantingResult{}, err
		}
		if gVer != expGardenVer {
			return BeginPlantingResult{}, apperr.VersionConflict(strconv.FormatInt(gVer, 10))
		}

		if in.Row < 1 || in.Row > gRows || in.Col < 1 || in.Col > gCols {
			return BeginPlantingResult{}, apperr.Validation("선택한 자리가 꽃밭 범위를 벗어났어요.", map[string]string{"cell": "유효한 칸을 선택해 주세요."})
		}

		var convStage string
		var convVer int64
		if err := tx.QueryRow(ctx, `
			SELECT stage, version FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, convID, ownerID).Scan(&convStage, &convVer); err != nil {
			return BeginPlantingResult{}, err
		}
		if convVer != expConvVer {
			return BeginPlantingResult{}, apperr.VersionConflict(strconv.FormatInt(convVer, 10))
		}
		if convStage != "gift_ready" {
			return BeginPlantingResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "꽃을 심을 수 있는 대화 단계가 아니에요.", false, nil)
		}

		var fStatus string
		var fVer int64
		if err := tx.QueryRow(ctx, `
			SELECT status, version FROM flower_gifts WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, flowerID, ownerID).Scan(&fStatus, &fVer); err != nil {
			return BeginPlantingResult{}, err
		}
		if fVer != expFlowerVer {
			return BeginPlantingResult{}, apperr.VersionConflict(strconv.FormatInt(fVer, 10))
		}
		if fStatus != "available" {
			return BeginPlantingResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "이미 심기 중이거나 처리된 꽃이에요.", false, nil)
		}

		var cellTaken bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM garden_occupancies
				WHERE garden_id = $1 AND row_no = $2 AND col_no = $3
			)
		`, gardenID, in.Row, in.Col).Scan(&cellTaken); err != nil {
			return BeginPlantingResult{}, err
		}
		if cellTaken {
			return BeginPlantingResult{}, apperr.New(
				apperr.CodeCellOccupied,
				http.StatusConflict,
				"이미 꽃이 있거나 예약된 자리예요. 다른 빈칸을 선택해 주세요.",
				false,
				nil,
			)
		}

		now := time.Now().UTC()
		ritualID := uuid.New()
		actionID := uuid.New()
		gardenRoute := s.rituals.Routes["garden"]
		arriveDef := gardenRoute.Actions[0]
		plannedEnd := now.Add(time.Duration(arriveDef.DurationMs) * time.Millisecond)

		if _, err := tx.Exec(ctx, `
			INSERT INTO rituals (
				id, owner_id, conversation_id, kind, definition_version, route,
				cursor, next_ordinal, active_action_id, status, version
			) VALUES ($1, $2, $3, 'planting', $4, 'garden', 0, 2, $5, 'active', 1)
		`, ritualID, ownerID, convID, s.rituals.DefinitionVersion, actionID); err != nil {
			return BeginPlantingResult{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO ritual_actions (
				id, owner_id, ritual_id, ordinal, route, action_key, started_at, planned_end_at
			) VALUES ($1, $2, $3, 1, 'garden', $4, $5, $6)
		`, actionID, ownerID, ritualID, arriveDef.Key, now, plannedEnd); err != nil {
			return BeginPlantingResult{}, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO garden_occupancies (
				garden_id, owner_id, row_no, col_no, flower_id, ritual_id, state, reserved_at
			) VALUES ($1, $2, $3, $4, $5, $6, 'reserved', $7)
		`, gardenID, ownerID, in.Row, in.Col, flowerID, ritualID, now); err != nil {
			if storage.IsUniqueConstraint(err, "") {
				return BeginPlantingResult{}, apperr.New(apperr.CodeCellOccupied, http.StatusConflict, "이미 다른 꽃이 선택한 자리예요.", false, nil)
			}
			return BeginPlantingResult{}, err
		}

		newGardenVer := gVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE gardens SET version = $2, updated_at = now() WHERE id = $1
		`, gardenID, newGardenVer); err != nil {
			return BeginPlantingResult{}, err
		}

		newConvVer := convVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE conversations SET stage = 'planting', version = $2, updated_at = now() WHERE id = $1
		`, convID, newConvVer); err != nil {
			return BeginPlantingResult{}, err
		}

		newFlowerVer := fVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE flower_gifts
			SET status = 'planting',
			    pending_row = $2,
			    pending_col = $3,
			    version = $4,
			    updated_at = now()
			WHERE id = $1
		`, flowerID, in.Row, in.Col, newFlowerVer); err != nil {
			return BeginPlantingResult{}, err
		}

		var flw ritual.FlowerSummaryDTO
		var seed int64
		var visRaw []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, species_id, species_name, style_seed, visual_snapshot,
			       status, kept_line, pending_row, pending_col, planted_row, planted_col, planted_at
			FROM flower_gifts WHERE id = $1
		`, flowerID).Scan(
			&flw.ID, &flw.SpeciesID, &flw.SpeciesName, &seed, &visRaw,
			&flw.Status, &flw.KeptLine, &flw.PendingRow, &flw.PendingCol,
			&flw.PlantedRow, &flw.PlantedCol, &flw.PlantedAt,
		); err != nil {
			return BeginPlantingResult{}, err
		}
		flw.StyleSeed = strconv.FormatInt(seed, 10)
		flw.Version = strconv.FormatInt(newFlowerVer, 10)
		flw.VisualSnapshot = json.RawMessage(visRaw)

		rv := ritual.RitualView{
			ID:                ritualID,
			ConversationID:    convID,
			Kind:              "planting",
			DefinitionVersion: s.rituals.DefinitionVersion,
			Route:             "garden",
			RouteLabel:        gardenRoute.Label,
			Cursor:            0,
			TotalActions:      len(gardenRoute.Actions),
			VisualStage:       arriveDef.VisualStage,
			Status:            "active",
			Version:           "1",
			CurrentStepLabel:  arriveDef.Label,
			CurrentDesc:       arriveDef.Description,
			ActiveAction: &ritual.ActiveActionDTO{
				ID:           actionID,
				Key:          arriveDef.Key,
				Label:        arriveDef.Label,
				Description:  arriveDef.Description,
				StartedAt:    now,
				PlannedEndAt: plannedEnd,
				DurationMs:   arriveDef.DurationMs,
			},
			AllowedCommands: []string{"skip_current_action"},
			Flower:          &flw,
		}

		out := BeginPlantingResult{
			GardenVersion:       strconv.FormatInt(newGardenVer, 10),
			ConversationID:      convID,
			ConversationStage:   "planting",
			ConversationVersion: strconv.FormatInt(newConvVer, 10),
			Flower:              flw,
			Ritual:              rv,
		}

		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(
			ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint,
			http.StatusOK, respBytes, map[string]string{"ritual_id": ritualID.String(), "flower_id": flowerID.String()}, s.retention,
		); err != nil {
			return BeginPlantingResult{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return BeginPlantingResult{}, err
		}
		return BeginPlantingResult{}, apperr.Internal(err)
	}
	return res, nil
}

func (s *Service) DiscardFlower(ctx context.Context, ownerID, flowerID uuid.UUID, idem character.IdemMeta, expectedVersion string) (DiscardResult, error) {
	expVer, err := parsePositiveInt64(expectedVersion, "expected_version")
	if err != nil {
		return DiscardResult{}, err
	}

	res, err := storage.WithTx(ctx, s.pool, func(tx pgx.Tx) (DiscardResult, error) {
		rec, err := storage.LockAndCheckIdempotency(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint)
		if err != nil {
			return DiscardResult{}, err
		}
		if rec != nil {
			var replay DiscardResult
			if err := json.Unmarshal(rec.ResponseJSON, &replay); err != nil {
				return DiscardResult{}, err
			}
			return replay, nil
		}

		var convID uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT source_conversation_id FROM flower_gifts WHERE id = $1 AND owner_id = $2
		`, flowerID, ownerID).Scan(&convID); err != nil {
			if err == pgx.ErrNoRows {
				return DiscardResult{}, apperr.NotFound("꽃 선물을 찾을 수 없어요.")
			}
			return DiscardResult{}, err
		}

		var convStage string
		var convVer int64
		if err := tx.QueryRow(ctx, `
			SELECT stage, version FROM conversations WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, convID, ownerID).Scan(&convStage, &convVer); err != nil {
			return DiscardResult{}, err
		}

		var fStatus string
		var fVer int64
		if err := tx.QueryRow(ctx, `
			SELECT status, version FROM flower_gifts WHERE id = $1 AND owner_id = $2 FOR UPDATE
		`, flowerID, ownerID).Scan(&fStatus, &fVer); err != nil {
			return DiscardResult{}, err
		}
		if fVer != expVer {
			return DiscardResult{}, apperr.VersionConflict(strconv.FormatInt(fVer, 10))
		}
		if fStatus != "available" || convStage != "gift_ready" {
			return DiscardResult{}, apperr.New(apperr.CodeInvalidTransition, http.StatusConflict, "심기가 시작된 뒤에는 꽃을 그냥 보낼 수 없어요.", false, nil)
		}

		newFVer := fVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE flower_gifts
			SET status = 'discarded',
			    kept_line = '',
			    pending_row = NULL,
			    pending_col = NULL,
			    discarded_at = now(),
			    version = $2,
			    updated_at = now()
			WHERE id = $1
		`, flowerID, newFVer); err != nil {
			return DiscardResult{}, err
		}

		newConvVer := convVer + 1
		if _, err := tx.Exec(ctx, `
			UPDATE conversations
			SET stage = 'completed',
			    closed_at = now(),
			    close_reason = 'flower_discarded',
			    version = $2,
			    updated_at = now()
			WHERE id = $1
		`, convID, newConvVer); err != nil {
			return DiscardResult{}, err
		}

		if err := ritual.PurgeConversationTx(ctx, tx, ownerID, convID); err != nil {
			return DiscardResult{}, err
		}

		var flw ritual.FlowerSummaryDTO
		var seed int64
		var visRaw []byte
		if err := tx.QueryRow(ctx, `
			SELECT id, species_id, species_name, style_seed, visual_snapshot, status, kept_line
			FROM flower_gifts WHERE id = $1
		`, flowerID).Scan(&flw.ID, &flw.SpeciesID, &flw.SpeciesName, &seed, &visRaw, &flw.Status, &flw.KeptLine); err != nil {
			return DiscardResult{}, err
		}
		flw.StyleSeed = strconv.FormatInt(seed, 10)
		flw.Version = strconv.FormatInt(newFVer, 10)
		flw.VisualSnapshot = json.RawMessage(visRaw)

		out := DiscardResult{
			ConversationID:      convID,
			ConversationStage:   "completed",
			ConversationVersion: strconv.FormatInt(newConvVer, 10),
			CloseReason:         "flower_discarded",
			Purged:              true,
			Flower:              flw,
		}
		respBytes, _ := json.Marshal(out)
		if err := storage.SaveIdempotencyRecord(ctx, tx, ownerID, idem.Scope, idem.Key, idem.Fingerprint, http.StatusOK, respBytes, nil, s.retention); err != nil {
			return DiscardResult{}, err
		}
		return out, nil
	})
	if err != nil {
		if _, ok := apperr.As(err); ok {
			return DiscardResult{}, err
		}
		return DiscardResult{}, apperr.Internal(err)
	}
	return res, nil
}

func parsePositiveInt64(v, field string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 1 {
		return 0, apperr.Validation(field+" 형식이 올바르지 않아요.", map[string]string{field: "양의 정수 문자열이 필요해요."})
	}
	return n, nil
}
