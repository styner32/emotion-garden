import React, { useCallback, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiClientError, api } from '../api/client';
import { FlowerSummaryDTO, RitualRoute, RitualView } from '../api/types';
import { SceneCanvas } from '../scenes/SceneCanvas';
import { FlowerBadge } from './FlowerBadge';
import styles from './RitualController.module.css';

export interface RitualControllerProps {
  initialRitual: RitualView;
  flower?: FlowerSummaryDTO;
  reducedMotion?: boolean;
  onRitualUpdated: (next: RitualView) => void;
  onReturnedToChat: () => void;
  onCompleted: (summary: { kind: 'release' | 'grow'; flower?: FlowerSummaryDTO }) => void;
}

const ROUTE_CHOICES: Array<{
  route: Extract<RitualRoute, 'sea' | 'incinerate' | 'bury'>;
  title: string;
  desc: string;
  badge: string;
}> = [
  {
    route: 'sea',
    title: '바다로 던지기',
    desc: '잔잔한 수평선 너머 파도 위로 봉투를 띄워 멀리 떠나보냅니다.',
    badge: '물결',
  },
  {
    route: 'incinerate',
    title: '화로에 소각하기',
    desc: '작은 화로의 온기 속에서 종이를 태워 가벼운 재와 연기로 흩어 보냅니다.',
    badge: '불꽃',
  },
  {
    route: 'bury',
    title: '땅에 묻기',
    desc: '조용한 흙을 파고 봉투를 묻은 뒤 단단하게 다져 침묵 속에 재웁니다.',
    badge: '대지',
  },
];

export const RitualController: React.FC<RitualControllerProps> = ({
  initialRitual,
  flower,
  reducedMotion = false,
  onRitualUpdated,
  onReturnedToChat,
  onCompleted,
}) => {
  const [ritual, setRitual] = useState<RitualView>(initialRitual);
  const [busy, setBusy] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // Sync when parent refetches a newer ritual version
  React.useEffect(() => {
    setRitual(initialRitual);
  }, [initialRitual]);

  const applyUpdate = useCallback(
    (next: RitualView) => {
      setRitual(next);
      onRitualUpdated(next);
    },
    [onRitualUpdated]
  );

  const refreshRitual = useCallback(async () => {
    try {
      const latest = await api.getRitual(ritual.id);
      applyUpdate(latest);
      // If client timer fired a few ms ahead of server clock, poll once more shortly
      if (latest.active_action) {
        window.setTimeout(async () => {
          try {
            const retrySnap = await api.getRitual(ritual.id);
            applyUpdate(retrySnap);
          } catch {
            // ignore transient poll error
          }
        }, 180);
      }
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      }
    }
  }, [ritual.id, applyUpdate]);

  const handleActionError = async (err: unknown) => {
    if (err instanceof ApiClientError) {
      if (err.code === 'VERSION_CONFLICT' || err.code === 'INVALID_TRANSITION') {
        await refreshRitual();
        return;
      }
      setErrorMsg(err.message);
    } else {
      setErrorMsg('단계 진행 중 잠시 문제가 생겼어요. 다시 시도해 주세요.');
    }
  };

  const handleSkip = async () => {
    if (!ritual.active_action || busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const next = await api.skipRitualAction(ritual.id, ritual.active_action.id, ritual.version);
      applyUpdate(next);
    } catch (err) {
      await handleActionError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleStartNext = async () => {
    if (!ritual.next_action_key || busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const next = await api.startRitualAction(ritual.id, ritual.version, ritual.next_action_key);
      applyUpdate(next);
    } catch (err) {
      await handleActionError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleReturnToChat = async () => {
    if (busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      await api.returnToChat(ritual.id, ritual.version);
      onReturnedToChat();
    } catch (err) {
      await handleActionError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleSelectRoute = async (route: Extract<RitualRoute, 'sea' | 'incinerate' | 'bury'>) => {
    if (busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const next = await api.selectRitualRoute(ritual.id, ritual.version, route);
      applyUpdate(next);
    } catch (err) {
      await handleActionError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleComplete = async () => {
    if (busy) return;
    setBusy(true);
    setErrorMsg(null);
    try {
      const res = await api.completeRitual(ritual.id, ritual.version, ritual.conversation_id);
      onCompleted({
        kind: ritual.kind,
        flower: res.flower ?? flower ?? ritual.flower,
      });
    } catch (err) {
      await handleActionError(err);
    } finally {
      setBusy(false);
    }
  };

  const cmds = new Set(ritual.allowed_commands);
  const canSkip = cmds.has('skip_current_action') && Boolean(ritual.active_action);
  const canNext = cmds.has('start_next_action') && Boolean(ritual.next_action_key);
  const canReturn = cmds.has('return_to_chat');
  const canChooseRoute =
    cmds.has('select_route:sea') ||
    cmds.has('select_route:incinerate') ||
    cmds.has('select_route:bury');
  const canComplete = cmds.has('complete_ritual');

  const activeFlower = flower ?? ritual.flower;

  return (
    <section className={styles.container} data-testid="ritual-controller">
      <div className={styles.topBar}>
        <div className={styles.metaGroup}>
          <span
            className={`${styles.kindPill} ${
              ritual.kind === 'release' ? styles.kindRelease : styles.kindGrow
            }`}
          >
            {ritual.route_label}
          </span>
          <span className={styles.stepCounter}>
            단계 {Math.min(ritual.cursor, ritual.total_actions)} / {ritual.total_actions}
          </span>
        </div>

        {canReturn && (
          <button
            type="button"
            className={styles.returnBtn}
            disabled={busy}
            onClick={() => void handleReturnToChat()}
            data-testid="return-to-chat-btn"
          >
            봉투를 열고 대화로 돌아가기
          </button>
        )}
      </div>

      <SceneCanvas
        ritual={ritual}
        flower={activeFlower}
        reducedMotion={reducedMotion}
        onActionTimerElapsed={() => void refreshRitual()}
      />

      <div className={styles.captionCard}>
        <div className={styles.captionHeader}>
          <div>
            <h2 className={styles.stepTitle}>{ritual.current_step_label}</h2>
            <p className={styles.stepDesc}>{ritual.current_description}</p>
          </div>
          {activeFlower && ritual.route === 'garden' && (
            <div className={styles.flowerMini}>
              <FlowerBadge
                visual={activeFlower.visual_snapshot}
                styleSeed={activeFlower.style_seed}
                speciesName={activeFlower.species_name}
                size={52}
              />
              <span>{activeFlower.species_name}</span>
            </div>
          )}
        </div>

        {errorMsg && (
          <div className={styles.errorBanner} role="alert">
            {errorMsg}
          </div>
        )}

        <div className={styles.controlsRow}>
          {canSkip && (
            <button
              type="button"
              className={styles.skipBtn}
              disabled={busy}
              onClick={() => void handleSkip()}
              data-testid="ritual-skip-btn"
            >
              현재 연출 건너뛰기
            </button>
          )}

          {canNext && (
            <button
              type="button"
              className={
                ritual.kind === 'release' ? styles.primaryReleaseBtn : styles.primaryGrowBtn
              }
              disabled={busy}
              onClick={() => void handleStartNext()}
              data-testid="ritual-next-btn"
            >
              {ritual.next_action_label ?? '다음 단계 진행하기'}
            </button>
          )}

          {canComplete && (
            <button
              type="button"
              className={
                ritual.kind === 'release' ? styles.primaryReleaseBtn : styles.primaryGrowBtn
              }
              disabled={busy}
              onClick={() => void handleComplete()}
              data-testid="ritual-complete-btn"
            >
              {ritual.kind === 'release'
                ? '비움 마무리 확정하기 (대화 원문 즉시 삭제)'
                : '꽃 심기 완료하기 (대화 원문 정리)'}
            </button>
          )}
        </div>

        {canChooseRoute && (
          <div className={styles.routeSection} data-testid="route-selector">
            <div className={styles.routeNotice}>
              <strong>어디로 떠나보낼까요?</strong>
              <span>보낼 곳을 선택하면 더 이상 대화로 돌아갈 수 없어요.</span>
            </div>
            <div className={styles.routeGrid}>
              {ROUTE_CHOICES.map((rc) => (
                <button
                  key={rc.route}
                  type="button"
                  className={styles.routeCard}
                  disabled={busy}
                  onClick={() => void handleSelectRoute(rc.route)}
                  data-testid={`select-route-${rc.route}`}
                >
                  <span className={styles.routeBadge}>{rc.badge}</span>
                  <strong className={styles.routeTitle}>{rc.title}</strong>
                  <span className={styles.routeDesc}>{rc.desc}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {ritual.route === 'garden' && (
        <div className={styles.gardenHint}>
          <span>예약된 자리: 세로 {activeFlower?.pending_row}행 · 가로 {activeFlower?.pending_col}열</span>
          <Link to="/garden" className={styles.inlineLink}>
            꽃밭 전체 보기 →
          </Link>
        </div>
      )}
    </section>
  );
};
