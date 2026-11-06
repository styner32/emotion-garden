import React, { useState } from 'react';
import { ConversationKind, RunDTO, TurnSnapshotDTO } from '../api/types';
import styles from './CandidateComparisonPanel.module.css';

export interface CandidateComparisonPanelProps {
  kind: ConversationKind;
  turn: TurnSnapshotDTO;
  streamedTextByRunId: Record<string, string>;
  detailedEvalEnabled: boolean;
  isBusy: boolean;
  onSelectRun: (runId: string) => void;
  onSkipTurn: () => void;
  onCancelRun: (runId: string) => void;
  onRetryRun: (runId: string) => void;
  onSubmitFeedback: (scores: Record<string, Record<string, number>>) => Promise<void>;
}

const RUBRIC_ITEMS: Array<{ key: string; label: string; releaseOnly?: boolean }> = [
  { key: 'empathy', label: '공감 진정성' },
  { key: 'context_reflection', label: '맥락 반영도' },
  { key: 'apology_specificity', label: '사과·인정 구체성', releaseOnly: true },
  { key: 'unnecessary_advice', label: '훈계·과잉조언 절제' },
  { key: 'repetition', label: '상투적 반복 회피' },
  { key: 'korean_naturalness', label: '한국어 자연스러움' },
];

function statusLabel(status: RunDTO['status']): string {
  switch (status) {
    case 'queued':
      return '대기 중…';
    case 'running':
      return '생각을 고르는 중…';
    case 'streaming':
      return '답변을 적는 중…';
    case 'succeeded':
      return '생성 완료';
    case 'failed':
      return '생성에 실패했어요';
    case 'incomplete':
      return '응답이 중간에 멈췄어요';
    case 'blocked':
      return '안전 정책으로 답변이 제한되었어요';
    case 'cancelled':
      return '중지됨';
    case 'interrupted':
      return '연결이 끊겨 중단되었어요';
    default:
      return status;
  }
}

export const CandidateComparisonPanel: React.FC<CandidateComparisonPanelProps> = ({
  kind,
  turn,
  streamedTextByRunId,
  detailedEvalEnabled,
  isBusy,
  onSelectRun,
  onSkipTurn,
  onCancelRun,
  onRetryRun,
  onSubmitFeedback,
}) => {
  const [scores, setScores] = useState<Record<string, Record<string, number>>>({});
  const [feedbackSaved, setFeedbackSaved] = useState(turn.has_feedback);
  const [savingFeedback, setSavingFeedback] = useState(false);

  if (turn.mode === 'quiet' || (turn.state === 'resolved' && turn.mode !== 'compare')) {
    return null;
  }

  // If compare turn is resolved and detailed eval is not open/needed, keep UI uncluttered unless user wants rubric
  if (turn.state === 'resolved' && !detailedEvalEnabled) {
    return null;
  }

  const isCompare = turn.mode === 'compare';
  const rubricMetrics = RUBRIC_ITEMS.filter((m) => !m.releaseOnly || kind === 'release');

  const handleScoreChange = (slot: string, metric: string, value: number) => {
    setScores((prev) => ({
      ...prev,
      [slot]: {
        ...(prev[slot] ?? {}),
        [metric]: value,
      },
    }));
  };

  const handleSaveFeedback = async () => {
    if (Object.keys(scores).length === 0) return;
    setSavingFeedback(true);
    try {
      await onSubmitFeedback(scores);
      setFeedbackSaved(true);
    } finally {
      setSavingFeedback(false);
    }
  };

  return (
    <section
      className={styles.panel}
      aria-label={isCompare ? 'AI 답변 후보 비교' : 'AI 답변 생성 중'}
      data-testid="candidate-panel"
    >
      <div className={styles.headerRow}>
        <div>
          <h3 className={styles.title}>
            {turn.state === 'resolved'
              ? '비교 답변 세부 평가'
              : isCompare
                ? '마음에 더 와닿는 답변을 골라 이어가세요'
                : '상대가 답변을 적고 있어요'}
          </h3>
          {isCompare && turn.state !== 'resolved' && (
            <p className={styles.subtitle}>
              {turn.blind_mode
                ? '블라인드 비교 중이에요. 선택 후 모델 정보가 공개됩니다.'
                : '같은 대화 맥락으로 생성된 두 모델의 답변 후보예요.'}
            </p>
          )}
        </div>

        {turn.state !== 'resolved' && (
          <button
            type="button"
            className={styles.skipBtn}
            disabled={isBusy}
            onClick={onSkipTurn}
            data-testid="skip-turn-btn"
          >
            이번엔 답변 없이 넘어가기
          </button>
        )}
      </div>

      <div className={`${styles.cardsGrid} ${isCompare ? styles.cardsCompare : styles.cardsSingle}`}>
        {turn.runs.map((run) => {
          const streamed = streamedTextByRunId[run.id] ?? '';
          const snapshotText = run.output_text ?? '';
          const displayText =
            run.status === 'succeeded' && snapshotText
              ? snapshotText
              : streamed.length >= snapshotText.length
                ? streamed
                : snapshotText;
          const isSelected = turn.selected_run_id === run.id;

          return (
            <article
              key={run.id}
              className={`${styles.card} ${isSelected ? styles.cardSelected : ''}`}
              data-testid={`candidate-card-${run.slot}`}
            >
              <div className={styles.cardTop}>
                <div className={styles.badgeGroup}>
                  <span className={styles.slotLabel}>{run.display_label}</span>
                  {run.is_mock && <span className={styles.mockBadge}>Mock</span>}
                  {!run.blind_hidden && run.requested_model && (
                    <span className={styles.modelTag}>{run.requested_model}</span>
                  )}
                </div>
                <span className={`${styles.statusTag} ${styles[`status_${run.status}`] ?? ''}`}>
                  {statusLabel(run.status)}
                </span>
              </div>

              <div className={styles.bodyText}>
                {displayText ? (
                  <p className={styles.outputParagraph}>{displayText}</p>
                ) : (
                  <p className={styles.placeholderText}>{statusLabel(run.status)}</p>
                )}
              </div>

              {run.total_ms !== undefined && run.status === 'succeeded' && (
                <div className={styles.metricsLine}>
                  <span>응답 {(run.total_ms / 1000).toFixed(1)}초</span>
                  {run.estimated_cost_usd ? (
                    <span>예상 ${run.estimated_cost_usd}</span>
                  ) : run.is_mock ? (
                    <span>무료 체험 응답</span>
                  ) : null}
                </div>
              )}

              {turn.state !== 'resolved' && (
                <div className={styles.actionsRow}>
                  {run.can_accept && (
                    <button
                      type="button"
                      className={styles.acceptBtn}
                      disabled={isBusy}
                      onClick={() => onSelectRun(run.id)}
                      data-testid={`accept-run-${run.slot}`}
                    >
                      이 답변으로 이어가기
                    </button>
                  )}
                  {run.can_cancel && (
                    <button
                      type="button"
                      className={styles.secondaryBtn}
                      disabled={isBusy}
                      onClick={() => onCancelRun(run.id)}
                    >
                      생성 중지
                    </button>
                  )}
                  {run.can_retry && (
                    <button
                      type="button"
                      className={styles.secondaryBtn}
                      disabled={isBusy}
                      onClick={() => onRetryRun(run.id)}
                    >
                      다시 생성
                    </button>
                  )}
                </div>
              )}

              {detailedEvalEnabled && run.status === 'succeeded' && (
                <div className={styles.rubricBox}>
                  <p className={styles.rubricTitle}>세부 인상 기록 (1~5점, 선택)</p>
                  <div className={styles.rubricGrid}>
                    {rubricMetrics.map((m) => {
                      const currentVal = scores[run.slot]?.[m.key] ?? 0;
                      return (
                        <div key={m.key} className={styles.rubricRow}>
                          <span className={styles.rubricLabel}>{m.label}</span>
                          <div className={styles.scoreButtons}>
                            {[1, 2, 3, 4, 5].map((num) => (
                              <button
                                key={num}
                                type="button"
                                className={`${styles.scoreBtn} ${
                                  currentVal === num ? styles.scoreBtnActive : ''
                                }`}
                                onClick={() => handleScoreChange(run.slot, m.key, num)}
                                aria-label={`${run.display_label} ${m.label} ${num}점`}
                              >
                                {num}
                              </button>
                            ))}
                          </div>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </article>
          );
        })}
      </div>

      {detailedEvalEnabled && (
        <div className={styles.feedbackFooter}>
          {feedbackSaved ? (
            <span className={styles.savedNote}>세부 평가 점수가 저장되었어요 (원문 미포함 메타데이터).</span>
          ) : (
            <button
              type="button"
              className={styles.secondaryBtn}
              disabled={savingFeedback || Object.keys(scores).length === 0}
              onClick={() => void handleSaveFeedback()}
            >
              {savingFeedback ? '저장 중…' : '세부 평가 점수 저장'}
            </button>
          )}
        </div>
      )}
    </section>
  );
};
