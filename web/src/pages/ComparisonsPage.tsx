import React from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { api } from '../api/client';
import { ComparisonItemDTO } from '../api/types';
import styles from './ComparisonsPage.module.css';

function categoryLabel(cat: string): string {
  switch (cat) {
    case 'complete_comparison':
      return '정상 비교 완료';
    case 'incomplete_early_selection':
      return '조기 선택 (상대 생성 전 확정)';
    case 'both_skipped':
      return '답변 건너뛰기';
    case 'technical_failure':
      return '일부 생성 실패·중단';
    default:
      return cat;
  }
}

export const ComparisonsPage: React.FC = () => {
  const { data, isLoading } = useQuery({
    queryKey: ['comparisons'],
    queryFn: () => api.listComparisons(40),
  });

  if (isLoading || !data) {
    return <p className={styles.loading}>비교 기록을 불러오는 중…</p>;
  }

  return (
    <div className={styles.page}>
      <div className={styles.headerRow}>
        <div>
          <h1 className={styles.title}>모델 비교 기록 및 품질 통계</h1>
          <p className={styles.subtitle}>
            대화 원문과 후보 본문을 일절 저장하지 않고, 프롬프트 버전·모델 조합별 선택 빈도와 응답 지연, 토큰·예상
            비용 메타데이터만 안전하게 집계합니다.
          </p>
        </div>
        <Link to="/settings/ai" className={styles.backLink}>
          ← AI 설정으로
        </Link>
      </div>

      {data.groups.length === 0 ? (
        <div className={styles.emptyCard} data-testid="comparisons-empty">
          <p>아직 두 모델 비교 모드로 확정된 기록이 없어요.</p>
          <span>대화 화면 상단에서 ‘두 모델 비교’를 선택해 답변을 비교해 보세요.</span>
        </div>
      ) : (
        <>
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>프롬프트·모델 조합별 요약</h2>
            <div className={styles.groupGrid}>
              {data.groups.map((g) => {
                const winEntries = Object.entries(g.wins_by_provider);
                return (
                  <article key={g.group_key} className={styles.groupCard}>
                    <div className={styles.groupTop}>
                      <span className={styles.promptTag}>프롬프트 {g.prompt_version}</span>
                      <strong className={styles.pairTitle}>
                        {g.openai_model || 'OpenAI'} vs {g.gemini_model || 'Gemini'}
                      </strong>
                    </div>

                    <div className={styles.statsRow}>
                      <div className={styles.statBox}>
                        <span>정상 비교</span>
                        <strong>{g.complete_comparisons}</strong>
                      </div>
                      <div className={styles.statBox}>
                        <span>조기 선택</span>
                        <strong>{g.incomplete_early_selections}</strong>
                      </div>
                      <div className={styles.statBox}>
                        <span>건너뛰기</span>
                        <strong>{g.both_skipped}</strong>
                      </div>
                      <div className={styles.statBox}>
                        <span>기술 오류</span>
                        <strong>{g.technical_failures}</strong>
                      </div>
                    </div>

                    <div className={styles.winsLine}>
                      <span>공급자별 선택 횟수:</span>
                      {winEntries.length === 0 ? (
                        <span>선택 없음</span>
                      ) : (
                        winEntries.map(([prov, count]) => (
                          <span key={prov} className={styles.winBadge}>
                            {prov}: {count}회
                          </span>
                        ))
                      )}
                    </div>
                  </article>
                );
              })}
            </div>
          </section>

          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>최근 비교 메타데이터 내역</h2>
            <div className={styles.itemList}>
              {data.items.map((item: ComparisonItemDTO) => (
                <article key={item.turn_id} className={styles.itemCard} data-testid="comparison-item">
                  <div className={styles.itemTop}>
                    <div className={styles.itemBadges}>
                      <span
                        className={
                          item.kind === 'release' ? styles.kindRelease : styles.kindGrow
                        }
                      >
                        {item.kind === 'release' ? '비움' : '피움'}
                      </span>
                      <span className={styles.catBadge}>{categoryLabel(item.category)}</span>
                      {item.selected_provider && (
                        <span className={styles.winBadge}>선택: {item.selected_provider}</span>
                      )}
                    </div>
                    <span className={styles.hashText} title="동일 입력 검증용 컨텍스트 해시">
                      맥락 해시 {item.context_hash.slice(0, 10)}…
                    </span>
                  </div>

                  <div className={styles.runsRow}>
                    {item.runs.map((r) => (
                      <div key={r.id} className={styles.runMetaBox}>
                        <div className={styles.runTitleLine}>
                          <strong>
                            후보 {r.slot.toUpperCase()} · {r.requested_model}
                          </strong>
                          {r.is_mock && <span className={styles.mockPill}>Mock</span>}
                        </div>
                        <div className={styles.runMetrics}>
                          <span>상태: {r.status}</span>
                          {r.first_text_ms !== undefined && (
                            <span>첫 글자: {r.first_text_ms}ms</span>
                          )}
                          {r.total_ms !== undefined && <span>총 소요: {r.total_ms}ms</span>}
                          <span>
                            토큰:{' '}
                            {r.usage_status === 'unknown'
                              ? '산정 불가'
                              : `입력 ${r.usage?.input_tokens ?? 0} / 출력 ${
                                  r.usage?.output_tokens ?? r.usage?.public_output_tokens ?? 0
                                }`}
                          </span>
                          <span>
                            비용:{' '}
                            {r.is_mock
                              ? '$0 (Mock)'
                              : r.estimated_cost_usd
                                ? `$${r.estimated_cost_usd}`
                                : '산정 불가'}
                          </span>
                        </div>
                      </div>
                    ))}
                  </div>
                </article>
              ))}
            </div>
          </section>
        </>
      )}
    </div>
  );
};
