import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { OccupancyDTO } from '../api/types';
import { FlowerBadge } from '../components/FlowerBadge';
import styles from './GardenPage.module.css';

export const GardenPage: React.FC = () => {
  const queryClient = useQueryClient();

  const { data: garden, isLoading } = useQuery({
    queryKey: ['garden'],
    queryFn: () => api.getGarden(),
  });

  const [colsInput, setColsInput] = useState(4);
  const [rowsInput, setRowsInput] = useState(3);
  const [resizing, setResizing] = useState(false);
  const [resizeError, setResizeError] = useState<string | null>(null);
  const [resizeSuccess, setResizeSuccess] = useState<string | null>(null);
  const [selectedOcc, setSelectedOcc] = useState<OccupancyDTO | null>(null);

  useEffect(() => {
    if (garden) {
      setColsInput(garden.cols);
      setRowsInput(garden.rows);
    }
  }, [garden]);

  const occMap = useMemo(() => {
    const m = new Map<string, OccupancyDTO>();
    for (const o of garden?.occupancies ?? []) {
      m.set(`${o.row}:${o.col}`, o);
    }
    return m;
  }, [garden]);

  const handleResize = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!garden || resizing || garden.has_reserved) return;
    setResizing(true);
    setResizeError(null);
    setResizeSuccess(null);
    try {
      const updated = await api.resizeGarden(garden.version, colsInput, rowsInput);
      queryClient.setQueryData(['garden'], updated);
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      setResizeSuccess(`꽃밭 크기가 가로 ${updated.cols}칸 × 세로 ${updated.rows}칸으로 변경되었어요.`);
    } catch (err) {
      if (err instanceof ApiClientError) {
        if (err.code === 'GARDEN_SHRINK_CONFLICT' && err.details) {
          const minC = err.details.minimum_cols;
          const minR = err.details.minimum_rows;
          setResizeError(
            `${err.message} (최소 가로 ${String(minC ?? 1)}칸 × 세로 ${String(minR ?? 1)}칸 이상이어야 해요.)`
          );
        } else {
          setResizeError(err.message);
        }
      } else {
        setResizeError('꽃밭 크기를 조정하는 중 문제가 생겼어요.');
      }
    } finally {
      setResizing(false);
    }
  };

  if (isLoading || !garden) {
    return <p className={styles.loading}>꽃밭을 살피는 중…</p>;
  }

  const plantedCount = garden.occupancies.filter((o) => o.state === 'planted').length;

  return (
    <div className={styles.page}>
      <div className={styles.headerRow}>
        <div>
          <h1 className={styles.title}>나의 작은 꽃밭</h1>
          <p className={styles.subtitle}>
            피움 대화를 마치고 직접 심은 꽃들이 자라는 공간이에요. 대화 원문은 남지 않고 꽃의 모습과 심은
            날짜({garden.timezone} 기준), 직접 남긴 짧은 한 줄만 간직됩니다.
          </p>
        </div>

        <div className={styles.statsPill}>
          <span>
            가로 {garden.cols} × 세로 {garden.rows}칸
          </span>
          <strong>심어진 꽃 {plantedCount}송이</strong>
        </div>
      </div>

      <div className={styles.mainLayout}>
        {/* Garden Soil Plot */}
        <section className={styles.plotCard} aria-label="꽃밭 격자">
          <div className={styles.legendRow}>
            <span className={styles.legendItem}>
              <span className={styles.legendEmpty} aria-hidden="true">
                ·
              </span>{' '}
              빈 땅
            </span>
            <span className={styles.legendItem}>
              <span className={styles.legendReserved} aria-hidden="true">
                🌱
              </span>{' '}
              심는 중 (예약됨)
            </span>
            <span className={styles.legendItem}>
              <span className={styles.legendPlanted} aria-hidden="true">
                ✿
              </span>{' '}
              심어진 꽃 (눌러서 한 줄 보기)
            </span>
          </div>

          <div
            className={styles.grid}
            style={{ gridTemplateColumns: `repeat(${garden.cols}, minmax(0, 1fr))` }}
            data-testid="garden-grid"
          >
            {Array.from({ length: garden.rows }, (_, rIdx) => {
              const row = rIdx + 1;
              return Array.from({ length: garden.cols }, (__, cIdx) => {
                const col = cIdx + 1;
                const occ = occMap.get(`${row}:${col}`);
                const isSelected =
                  selectedOcc?.row === row && selectedOcc?.col === col;

                if (!occ) {
                  return (
                    <div
                      key={`${row}:${col}`}
                      className={`${styles.cell} ${styles.cellEmpty}`}
                      aria-label={`${row}행 ${col}열 빈 땅`}
                      data-testid={`garden-cell-${row}-${col}`}
                    >
                      <span className={styles.emptySoilIcon} aria-hidden="true">
                        ·
                      </span>
                      <span className={styles.cellCaption}>빈 땅</span>
                    </div>
                  );
                }

                if (occ.state === 'reserved') {
                  return (
                    <button
                      key={`${row}:${col}`}
                      type="button"
                      onClick={() => setSelectedOcc(occ)}
                      className={`${styles.cell} ${styles.cellReserved} ${
                        isSelected ? styles.cellSelected : ''
                      }`}
                      aria-label={`${row}행 ${col}열 ${occ.species_name} 심는 중`}
                      data-testid={`garden-cell-${row}-${col}`}
                    >
                      <span className={styles.seedlingBadge} aria-hidden="true">
                        🌱
                      </span>
                      <span className={styles.cellCaption}>심는 중</span>
                    </button>
                  );
                }

                return (
                  <button
                    key={`${row}:${col}`}
                    type="button"
                    onClick={() => setSelectedOcc(occ)}
                    className={`${styles.cell} ${styles.cellPlanted} ${
                      isSelected ? styles.cellSelected : ''
                    }`}
                    aria-label={`${row}행 ${col}열 심어진 꽃 ${occ.species_name}`}
                    data-testid={`garden-cell-${row}-${col}`}
                  >
                    <FlowerBadge
                      visual={occ.visual_snapshot}
                      styleSeed={occ.style_seed}
                      speciesName={occ.species_name}
                      size={garden.cols >= 5 ? 36 : 48}
                    />
                    <span className={styles.cellCaption}>{occ.species_name}</span>
                  </button>
                );
              });
            })}
          </div>
        </section>

        {/* Side / Bottom Controls & Detail Drawer */}
        <aside className={styles.sidePane}>
          {selectedOcc ? (
            <section className={styles.detailCard} data-testid="flower-detail-drawer">
              <div className={styles.detailHeader}>
                <span className={styles.detailCoord}>
                  세로 {selectedOcc.row}행 · 가로 {selectedOcc.col}열
                </span>
                <button
                  type="button"
                  className={styles.closeBtn}
                  onClick={() => setSelectedOcc(null)}
                >
                  닫기
                </button>
              </div>

              {selectedOcc.state === 'reserved' ? (
                <div className={styles.reservedDetail}>
                  <FlowerBadge
                    visual={selectedOcc.visual_snapshot}
                    styleSeed={selectedOcc.style_seed}
                    speciesName={selectedOcc.species_name}
                    size={96}
                    showRootBall
                  />
                  <h2>{selectedOcc.species_name} (심는 중)</h2>
                  <p>
                    아직 흙을 덮고 물을 주는 심기 과정이 남아 있어요. 이어서 마무리를 완료하면 이 자리에
                    꽃이 피어납니다.
                  </p>
                  <Link
                    to={`/conversations/${selectedOcc.conversation_id}`}
                    className={styles.resumePlantBtn}
                    data-testid="resume-planting-from-garden"
                  >
                    이어서 심기 마무리하기 →
                  </Link>
                </div>
              ) : (
                <div className={styles.plantedDetail}>
                  <FlowerBadge
                    visual={selectedOcc.visual_snapshot}
                    styleSeed={selectedOcc.style_seed}
                    speciesName={selectedOcc.species_name}
                    size={108}
                  />
                  <h2 className={styles.detailSpecies}>{selectedOcc.species_name}</h2>
                  <span className={styles.detailDate} data-testid="flower-planted-date">
                    심은 날: {selectedOcc.planted_date_local ?? '기록됨'}
                  </span>

                  <blockquote className={styles.keptQuote} data-testid="flower-kept-line">
                    {selectedOcc.kept_line ? (
                      `“${selectedOcc.kept_line}”`
                    ) : (
                      <span className={styles.noQuote}>남겨둔 문구 없이 조용히 심어진 꽃이에요.</span>
                    )}
                  </blockquote>

                  <p className={styles.privacyNote}>
                    이 꽃을 피우며 나누었던 대화 원문은 심기 완료와 함께 안전하게 삭제되었습니다.
                  </p>
                </div>
              )}
            </section>
          ) : (
            <section className={styles.hintCard}>
              <h2 className={styles.sideTitle}>꽃 살펴보기</h2>
              <p className={styles.sideDesc}>
                꽃밭에 피어난 꽃을 누르면 심은 날짜와 직접 남겨둔 짧은 한 줄을 다시 읽어볼 수 있어요.
              </p>
            </section>
          )}

          {/* Garden Resize Form */}
          <form className={styles.resizeCard} onSubmit={(e) => void handleResize(e)}>
            <h2 className={styles.sideTitle}>꽃밭 크기 조절</h2>
            <p className={styles.sideDesc}>
              가로는 1~{garden.max_cols}칸, 세로는 1~{garden.max_rows}칸 범위에서 넓히거나 줄일 수 있어요. 이미
              꽃이 심어진 구역보다 작게는 줄일 수 없습니다.
            </p>

            {garden.has_reserved && (
              <p className={styles.warnText}>
                심는 중인 자리가 있을 때는 꽃밭 크기를 변경할 수 없어요.
              </p>
            )}

            <div className={styles.sizeInputs}>
              <label className={styles.sizeField}>
                <span>가로 (열, 1~{garden.max_cols})</span>
                <input
                  type="number"
                  min={1}
                  max={garden.max_cols}
                  disabled={garden.has_reserved}
                  value={colsInput}
                  onChange={(e) => setColsInput(Number(e.target.value))}
                  className={styles.numInput}
                  data-testid="garden-cols-input"
                />
              </label>

              <label className={styles.sizeField}>
                <span>세로 (행, 1~{garden.max_rows})</span>
                <input
                  type="number"
                  min={1}
                  max={garden.max_rows}
                  disabled={garden.has_reserved}
                  value={rowsInput}
                  onChange={(e) => setRowsInput(Number(e.target.value))}
                  className={styles.numInput}
                  data-testid="garden-rows-input"
                />
              </label>
            </div>

            {resizeError && (
              <p className={styles.errorText} role="alert" data-testid="garden-resize-error">
                {resizeError}
              </p>
            )}
            {resizeSuccess && <p className={styles.successText}>{resizeSuccess}</p>}

            <button
              type="submit"
              disabled={
                resizing ||
                garden.has_reserved ||
                (colsInput === garden.cols && rowsInput === garden.rows)
              }
              className={styles.resizeBtn}
              data-testid="garden-resize-btn"
            >
              {resizing ? '조절 중…' : '꽃밭 크기 적용하기'}
            </button>
          </form>
        </aside>
      </div>
    </div>
  );
};
