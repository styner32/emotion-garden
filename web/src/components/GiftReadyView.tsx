import React, { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { FlowerSummaryDTO, OccupancyDTO, RitualView } from '../api/types';
import { FlowerBadge } from './FlowerBadge';
import styles from './GiftReadyView.module.css';

export interface GiftReadyViewProps {
  conversationId: string;
  conversationVersion: string;
  initialFlower: FlowerSummaryDTO;
  maxKeptLineChars?: number;
  onPlantingStarted: (payload: {
    ritual: RitualView;
    flower: FlowerSummaryDTO;
    conversationVersion: string;
  }) => void;
  onDiscarded: (flower: FlowerSummaryDTO) => void;
}

function countCodePoints(str: string): number {
  return Array.from(str).length;
}

export const GiftReadyView: React.FC<GiftReadyViewProps> = ({
  conversationId,
  conversationVersion,
  initialFlower,
  maxKeptLineChars = 80,
  onPlantingStarted,
  onDiscarded,
}) => {
  const queryClient = useQueryClient();
  const [flower, setFlower] = useState<FlowerSummaryDTO>(initialFlower);
  const [keptLine, setKeptLine] = useState(initialFlower.kept_line ?? '');
  const [selectedCell, setSelectedCell] = useState<{ row: number; col: number } | null>(
    initialFlower.pending_row && initialFlower.pending_col
      ? { row: initialFlower.pending_row, col: initialFlower.pending_col }
      : null
  );
  const [savingLine, setSavingLine] = useState(false);
  const [lineSavedNote, setLineSavedNote] = useState(false);
  const [busy, setBusy] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const { data: garden, refetch: refetchGarden } = useQuery({
    queryKey: ['garden'],
    queryFn: () => api.getGarden(),
  });

  const occMap = useMemo(() => {
    const m = new Map<string, OccupancyDTO>();
    for (const o of garden?.occupancies ?? []) {
      m.set(`${o.row}:${o.col}`, o);
    }
    return m;
  }, [garden]);

  const codePointLen = countCodePoints(keptLine.trim());
  const isLineTooLong = codePointLen > maxKeptLineChars;

  const totalCells = garden ? garden.rows * garden.cols : 0;
  const occupiedCount = garden?.occupancies.length ?? 0;
  const isGardenFull = garden ? occupiedCount >= totalCells : false;
  const canExpandGarden = garden
    ? garden.cols < garden.max_cols || garden.rows < garden.max_rows
    : false;

  const persistKeptLineIfDirty = async (currentFlower: FlowerSummaryDTO): Promise<FlowerSummaryDTO> => {
    const trimmed = keptLine.trim();
    if (trimmed === (currentFlower.kept_line ?? '')) {
      return currentFlower;
    }
    setSavingLine(true);
    try {
      const updated = await api.updateFlower(currentFlower.id, {
        expected_version: currentFlower.version,
        kept_line: trimmed,
      });
      setFlower(updated);
      setLineSavedNote(true);
      return updated;
    } finally {
      setSavingLine(false);
    }
  };

  const handleSaveKeptLine = async () => {
    if (isLineTooLong || savingLine) return;
    setErrorMsg(null);
    try {
      await persistKeptLineIfDirty(flower);
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      }
    }
  };

  const handleBeginPlanting = async () => {
    if (!garden || !selectedCell || busy || isLineTooLong) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const syncedFlower = await persistKeptLineIfDirty(flower);
      const res = await api.beginPlanting(syncedFlower.id, {
        row: selectedCell.row,
        col: selectedCell.col,
        expected_garden_version: garden.version,
        expected_conversation_version: conversationVersion,
        expected_flower_version: syncedFlower.version,
      });
      void queryClient.invalidateQueries({ queryKey: ['garden'] });
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      onPlantingStarted({
        ritual: res.ritual,
        flower: res.flower,
        conversationVersion: res.conversation_version,
      });
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
        if (
          err.code === 'CELL_OCCUPIED' ||
          err.code === 'GARDEN_BUSY' ||
          err.code === 'VERSION_CONFLICT'
        ) {
          void refetchGarden();
        }
      } else {
        setErrorMsg('심기를 시작하는 중 문제가 생겼어요.');
      }
    } finally {
      setBusy(false);
    }
  };

  const handleDiscard = async () => {
    if (busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const res = await api.discardFlower(flower.id, flower.version, conversationId);
      onDiscarded(res.flower);
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg('꽃을 보내는 중 문제가 생겼어요.');
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className={styles.wrapper} data-testid="gift-ready-view">
      <div className={styles.giftCard}>
        <span className={styles.eyebrow}>피움의 선물</span>
        <div className={styles.flowerShowcase}>
          <FlowerBadge
            visual={flower.visual_snapshot}
            styleSeed={flower.style_seed}
            speciesName={flower.species_name}
            size={112}
            showRootBall
          />
          <div className={styles.flowerInfo}>
            <h2 className={styles.speciesTitle}>{flower.species_name}</h2>
            <p className={styles.speciesDesc}>
              나눈 이야기 끝에 작은 꽃 한 송이가 건네졌어요. 꽃밭에 심거나 바람에 조용히 흘려보낼 수 있어요.
              어느 쪽을 택하든 대화 원문은 남지 않아요.
            </p>
          </div>
        </div>

        <div className={styles.keptLineSection}>
          <div className={styles.labelRow}>
            <label htmlFor="kept-line-input" className={styles.label}>
              꽃에 곁들일 짧은 한 줄 (선택)
            </label>
            <span className={isLineTooLong ? styles.counterOver : styles.counter}>
              {codePointLen} / {maxKeptLineChars}자
            </span>
          </div>
          <div className={styles.inputRow}>
            <input
              id="kept-line-input"
              type="text"
              className={styles.input}
              value={keptLine}
              onChange={(e) => {
                setKeptLine(e.target.value);
                setLineSavedNote(false);
              }}
              onBlur={() => void handleSaveKeptLine()}
              placeholder="예: 오늘의 다짐이나 기억하고 싶은 마음 한 줄"
              data-testid="kept-line-input"
            />
            <button
              type="button"
              className={styles.saveLineBtn}
              disabled={savingLine || isLineTooLong || keptLine.trim() === (flower.kept_line ?? '')}
              onClick={() => void handleSaveKeptLine()}
            >
              {savingLine ? '저장…' : '문구 저장'}
            </button>
          </div>
          {lineSavedNote && <p className={styles.savedHint}>문구가 꽃에 기록되었어요.</p>}
          {isLineTooLong && (
            <p className={styles.errorHint}>남길 한 줄은 {maxKeptLineChars}자 이하로 적어 주세요.</p>
          )}
        </div>

        <div className={styles.gardenPickerSection}>
          <div className={styles.pickerHeader}>
            <div>
              <h3 className={styles.pickerTitle}>심을 자리 고르기</h3>
              <p className={styles.pickerSub}>
                비어 있는 흙 칸을 눌러 자리를 정한 뒤 직접 땅을 파고 물을 주어 심어요.
              </p>
            </div>
            <Link to="/garden" className={styles.gardenLink}>
              꽃밭 관리 / 크기 조절 →
            </Link>
          </div>

          {garden?.has_reserved && (
            <div className={styles.warnBox}>
              현재 꽃밭에 이미 심는 중인 자리가 있어요. 진행 중인 심기를 마친 뒤 새 자리에 심을 수 있어요.
            </div>
          )}

          {isGardenFull && (
            <div className={styles.warnBox} data-testid="garden-full-banner">
              꽃밭의 모든 칸이 채워져 있어요.{' '}
              {canExpandGarden ? (
                <Link to="/garden" className={styles.gardenLink}>
                  꽃밭 크기를 넓히고 돌아오거나
                </Link>
              ) : (
                <span>최대 크기에 도달했어요. </span>
              )}{' '}
              심지 않고 바람에 보내줄 수도 있어요.
            </div>
          )}

          {garden && (
            <div
              className={styles.grid}
              style={{ gridTemplateColumns: `repeat(${garden.cols}, minmax(0, 1fr))` }}
              role="grid"
              aria-label="심을 꽃밭 자리 선택"
            >
              {Array.from({ length: garden.rows }, (_, rIdx) => {
                const row = rIdx + 1;
                return Array.from({ length: garden.cols }, (__, cIdx) => {
                  const col = cIdx + 1;
                  const occ = occMap.get(`${row}:${col}`);
                  const isSelected = selectedCell?.row === row && selectedCell?.col === col;
                  const disabled = Boolean(occ) || garden.has_reserved;

                  return (
                    <button
                      key={`${row}:${col}`}
                      type="button"
                      disabled={disabled}
                      onClick={() => setSelectedCell({ row, col })}
                      className={`${styles.cell} ${
                        occ
                          ? styles.cellOccupied
                          : isSelected
                            ? styles.cellSelected
                            : styles.cellEmpty
                      }`}
                      aria-label={`${row}행 ${col}열 ${
                        occ ? (occ.state === 'planted' ? '심어진 꽃 있음' : '심는 중') : '빈 땅'
                      }`}
                      data-testid={`pick-cell-${row}-${col}`}
                    >
                      {occ ? (
                        <>
                          <FlowerBadge
                            visual={occ.visual_snapshot}
                            styleSeed={occ.style_seed}
                            speciesName={occ.species_name}
                            size={34}
                          />
                          <span className={styles.cellTag}>
                            {occ.state === 'planted' ? '심어짐' : '심는 중'}
                          </span>
                        </>
                      ) : (
                        <>
                          <span className={styles.soilDot} aria-hidden="true">
                            {isSelected ? '✦' : '·'}
                          </span>
                          <span className={styles.cellTag}>
                            {isSelected ? `${row}·${col} 선택됨` : '빈 땅'}
                          </span>
                        </>
                      )}
                    </button>
                  );
                });
              })}
            </div>
          )}
        </div>

        {errorMsg && (
          <div className={styles.errorBanner} role="alert">
            {errorMsg}
          </div>
        )}

        <div className={styles.footerActions}>
          <button
            type="button"
            className={styles.discardBtn}
            disabled={busy}
            onClick={() => void handleDiscard()}
            data-testid="discard-flower-btn"
          >
            심지 않고 바람에 보내기
          </button>

          <button
            type="button"
            className={styles.plantBtn}
            disabled={
              busy || !selectedCell || isLineTooLong || Boolean(garden?.has_reserved)
            }
            onClick={() => void handleBeginPlanting()}
            data-testid="start-planting-btn"
          >
            {selectedCell
              ? `${selectedCell.row}행 ${selectedCell.col}열에 심기 시작`
              : '심을 빈 땅을 선택해 주세요'}
          </button>
        </div>
      </div>
    </section>
  );
};
