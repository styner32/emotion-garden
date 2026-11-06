import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { GenerationMode } from '../api/types';
import styles from './AISettingsPage.module.css';

export const AISettingsPage: React.FC = () => {
  const queryClient = useQueryClient();

  const { data: settings, isLoading } = useQuery({
    queryKey: ['ai-settings'],
    queryFn: () => api.getAISettings(),
  });

  const [releaseMode, setReleaseMode] = useState<GenerationMode>('openai');
  const [growMode, setGrowMode] = useState<GenerationMode>('openai');
  const [openaiAlias, setOpenaiAlias] = useState('');
  const [geminiAlias, setGeminiAlias] = useState('');
  const [blindCompare, setBlindCompare] = useState(true);
  const [detailedEval, setDetailedEval] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  const [dailyBudget, setDailyBudget] = useState('');
  const [saving, setSaving] = useState(false);
  const [savedBanner, setSavedBanner] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  useEffect(() => {
    if (!settings) return;
    setReleaseMode(settings.release_mode);
    setGrowMode(settings.grow_mode);
    setOpenaiAlias(settings.model_aliases.openai ?? '');
    setGeminiAlias(settings.model_aliases.gemini ?? '');
    setBlindCompare(settings.blind_compare);
    setDetailedEval(settings.detailed_eval_enabled);
    setReducedMotion(settings.reduced_motion);
    setDailyBudget(settings.daily_budget_usd ?? '');
  }, [settings]);

  if (isLoading || !settings) {
    return <p className={styles.loading}>AI 설정을 불러오는 중…</p>;
  }

  const openaiModels = settings.models.filter((m) => m.provider === 'openai' && m.enabled);
  const geminiModels = settings.models.filter((m) => m.provider === 'gemini' && m.enabled);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setErrorMsg(null);
    setSavedBanner(false);

    try {
      const updated = await api.updateAISettings({
        expected_version: settings.version,
        release_mode: releaseMode,
        grow_mode: growMode,
        model_aliases: {
          openai: openaiAlias,
          gemini: geminiAlias,
        },
        blind_compare: blindCompare,
        detailed_eval_enabled: detailedEval,
        reduced_motion: reducedMotion,
        daily_budget_usd: dailyBudget.trim() || undefined,
      });
      queryClient.setQueryData(['ai-settings'], updated);
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      setSavedBanner(true);
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg('설정을 저장하는 중 문제가 생겼어요.');
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className={styles.page}>
      <div className={styles.headerRow}>
        <div>
          <h1 className={styles.title}>AI 응답 및 접근성 설정</h1>
          <p className={styles.subtitle}>
            새 대화를 시작할 때 사용할 기본 AI 모드, 모델 별칭, 블라인드 비교 방식과 화면 움직임 설정을 관리합니다.
          </p>
        </div>
        <Link to="/settings/comparisons" className={styles.compareLink}>
          모델 비교 통계 보기 →
        </Link>
      </div>

      <section className={styles.statusCard} data-testid="provider-status-card">
        <h2 className={styles.cardTitle}>공급자 연결 상태</h2>
        <div className={styles.statusGrid}>
          <div className={styles.statusItem}>
            <strong>OpenAI Responses API</strong>
            <span className={settings.openai_key_configured ? styles.liveTag : styles.mockTag}>
              {settings.openai_key_configured
                ? '실제 API 키 연결됨'
                : settings.mock_mode_enabled
                  ? '체험용 한국어 Mock 어댑터 활성'
                  : '미설정'}
            </span>
          </div>
          <div className={styles.statusItem}>
            <strong>Google Gemini Interactions API</strong>
            <span className={settings.gemini_key_configured ? styles.liveTag : styles.mockTag}>
              {settings.gemini_key_configured
                ? '실제 API 키 연결됨'
                : settings.mock_mode_enabled
                  ? '체험용 한국어 Mock 어댑터 활성'
                  : '미설정'}
            </span>
          </div>
        </div>
        <p className={styles.privacyNote}>
          실제 API 키 사용 시에도 모든 요청은 공급자 측 대화 저장 비활성화(<code>store=false</code>)로 전송됩니다.
        </p>
      </section>

      <form className={styles.formCard} onSubmit={(e) => void handleSave(e)}>
        {errorMsg && (
          <div className={styles.errorBanner} role="alert">
            {errorMsg}
          </div>
        )}
        {savedBanner && (
          <div className={styles.savedBanner} role="status">
            설정이 저장되었어요.
          </div>
        )}

        <div className={styles.twoCol}>
          <label className={styles.field}>
            <span className={styles.label}>비움(털어놓기) 기본 AI 모드</span>
            <select
              className={styles.select}
              value={releaseMode}
              onChange={(e) => setReleaseMode(e.target.value as GenerationMode)}
            >
              <option value="openai">OpenAI 단독</option>
              <option value="gemini">Gemini 단독</option>
              <option value="compare">두 모델 비교 (OpenAI + Gemini)</option>
            </select>
          </label>

          <label className={styles.field}>
            <span className={styles.label}>피움(고민 나누기) 기본 AI 모드</span>
            <select
              className={styles.select}
              value={growMode}
              onChange={(e) => setGrowMode(e.target.value as GenerationMode)}
            >
              <option value="openai">OpenAI 단독</option>
              <option value="gemini">Gemini 단독</option>
              <option value="compare">두 모델 비교 (OpenAI + Gemini)</option>
            </select>
          </label>
        </div>

        <div className={styles.twoCol}>
          <label className={styles.field}>
            <span className={styles.label}>OpenAI 모델 선택</span>
            <select
              className={styles.select}
              value={openaiAlias}
              onChange={(e) => setOpenaiAlias(e.target.value)}
            >
              {openaiModels.map((m) => (
                <option key={m.alias} value={m.alias}>
                  {m.display_name} ({m.model_id})
                </option>
              ))}
            </select>
          </label>

          <label className={styles.field}>
            <span className={styles.label}>Gemini 모델 선택</span>
            <select
              className={styles.select}
              value={geminiAlias}
              onChange={(e) => setGeminiAlias(e.target.value)}
            >
              {geminiModels.map((m) => (
                <option key={m.alias} value={m.alias}>
                  {m.display_name} ({m.model_id})
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className={styles.toggleList}>
          <label className={styles.toggleRow}>
            <div>
              <strong>블라인드 비교 모드</strong>
              <p>두 모델 비교 시 선택 전까지 모델 이름을 가리고 후보 A·B 순서를 무작위로 섞어 보여줍니다.</p>
            </div>
            <input
              type="checkbox"
              checked={blindCompare}
              onChange={(e) => setBlindCompare(e.target.checked)}
            />
          </label>

          <label className={styles.toggleRow}>
            <div>
              <strong>세부 평가 루브릭 표시</strong>
              <p>답변 후보 아래에 공감도·맥락 반영·한국어 자연스러움 등 1~5점 평가표를 표시합니다.</p>
            </div>
            <input
              type="checkbox"
              checked={detailedEval}
              onChange={(e) => setDetailedEval(e.target.checked)}
            />
          </label>

          <label className={styles.toggleRow}>
            <div>
              <strong>움직임 줄이기 (Reduced Motion)</strong>
              <p>봉투 떠나보내기와 꽃 심기 연출의 흔들림·파티클 움직임을 차분한 정적 전환으로 줄입니다.</p>
            </div>
            <input
              type="checkbox"
              checked={reducedMotion}
              onChange={(e) => setReducedMotion(e.target.checked)}
            />
          </label>
        </div>

        <label className={styles.field}>
          <span className={styles.label}>일일 예상 비용 상한 (USD, 비워두면 제한 없음)</span>
          <input
            type="text"
            className={styles.input}
            value={dailyBudget}
            onChange={(e) => setDailyBudget(e.target.value)}
            placeholder="예: 2.50"
          />
        </label>

        <div className={styles.submitRow}>
          <button type="submit" disabled={saving} className={styles.saveBtn}>
            {saving ? '저장 중…' : '설정 저장하기'}
          </button>
        </div>
      </form>
    </div>
  );
};
