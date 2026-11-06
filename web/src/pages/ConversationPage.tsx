import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import {
  ApiClientError,
  api,
  createIdempotencyKey,
  onConversationPurged,
} from '../api/client';
import { connectTurnEventStream } from '../api/sse';
import {
  ConversationKind,
  FlowerSummaryDTO,
  GenerationMode,
  ReplyPolicy,
  RitualView,
} from '../api/types';
import { PortraitRenderer } from '../characters/PortraitRenderer';
import { CandidateComparisonPanel } from '../components/CandidateComparisonPanel';
import { FlowerBadge } from '../components/FlowerBadge';
import { GiftReadyView } from '../components/GiftReadyView';
import { RitualController } from '../components/RitualController';
import styles from './ConversationPage.module.css';

interface CompletionSummary {
  kind: ConversationKind;
  discarded?: boolean;
  flower?: FlowerSummaryDTO;
}

export const ConversationPage: React.FC = () => {
  const { id = '' } = useParams<{ id: string }>();
  const queryClient = useQueryClient();

  const [purgedState, setPurgedState] = useState(false);
  const [completionSummary, setCompletionSummary] = useState<CompletionSummary | null>(null);

  const [composerText, setComposerText] = useState('');
  const [draftStatus, setDraftStatus] = useState<'idle' | 'saving' | 'saved'>('idle');
  const draftVersionRef = useRef<string>('1');
  const lastSavedTextRef = useRef<string>('');
  const draftInitializedForIdRef = useRef<string>('');
  const draftTimerRef = useRef<number | null>(null);

  const [submittingTurn, setSubmittingTurn] = useState(false);
  const [turnActionBusy, setTurnActionBusy] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // Modals for first-time compare cost notice & unsent draft/pending turn closure confirmation
  const [pendingCompareAck, setPendingCompareAck] = useState(false);
  const [closureConfirmOpen, setClosureConfirmOpen] = useState(false);
  const [closingBusy, setClosingBusy] = useState(false);

  // Streaming text accumulated from seq=0 per run_id
  const [streamedByRunId, setStreamedByRunId] = useState<Record<string, string>>({});

  // Scroll tracking (pause auto-scroll if user scrolled up)
  const timelineRef = useRef<HTMLDivElement | null>(null);
  const isPinnedBottomRef = useRef<boolean>(true);
  const [hasNewBelow, setHasNewBelow] = useState(false);

  const { data: bootstrap } = useQuery({
    queryKey: ['bootstrap'],
    queryFn: () => api.getBootstrap(),
  });

  const {
    data: detail,
    error: queryError,
    isLoading,
    refetch: refetchConversation,
  } = useQuery({
    queryKey: ['conversation', id],
    queryFn: () => api.getConversation(id),
    enabled: Boolean(id) && !purgedState && !completionSummary,
    retry: false,
  });

  // Listen for cross-tab or same-tab purge notifications (DATA04)
  useEffect(() => {
    return onConversationPurged((purgedConvId) => {
      if (purgedConvId === id) {
        if (draftTimerRef.current) {
          window.clearTimeout(draftTimerRef.current);
          draftTimerRef.current = null;
        }
        setComposerText('');
        setStreamedByRunId({});
        setPurgedState(true);
      }
    });
  }, [id]);

  // Initialize composer text & draft version from loaded conversation
  useEffect(() => {
    if (!detail) return;
    draftVersionRef.current = detail.draft.draft_version;
    if (draftInitializedForIdRef.current !== detail.id) {
      draftInitializedForIdRef.current = detail.id;
      setComposerText(detail.draft.text ?? '');
      lastSavedTextRef.current = detail.draft.text ?? '';
    }
  }, [detail]);

  // Debounced monotonic draft autosave (700ms) while in chat stage
  useEffect(() => {
    if (!detail || detail.stage !== 'chat' || purgedState || completionSummary) return;
    if (composerText === lastSavedTextRef.current) return;

    if (draftTimerRef.current) {
      window.clearTimeout(draftTimerRef.current);
    }

    draftTimerRef.current = window.setTimeout(async () => {
      draftTimerRef.current = null;
      if (composerText === lastSavedTextRef.current) return;
      setDraftStatus('saving');
      try {
        const saved = await api.saveDraft(detail.id, {
          draft_version: draftVersionRef.current,
          captured_epoch: detail.content_epoch,
          text: composerText,
        });
        draftVersionRef.current = saved.draft_version;
        lastSavedTextRef.current = saved.text;
        setDraftStatus('saved');
      } catch (err) {
        if (err instanceof ApiClientError && err.code === 'CONTENT_PURGED') {
          setPurgedState(true);
          return;
        }
        setDraftStatus('idle');
      }
    }, 700);

    return () => {
      if (draftTimerRef.current) {
        window.clearTimeout(draftTimerRef.current);
        draftTimerRef.current = null;
      }
    };
  }, [composerText, detail, purgedState, completionSummary]);

  const scrollToBottom = useCallback(() => {
    const el = timelineRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isPinnedBottomRef.current = true;
    setHasNewBelow(false);
  }, []);

  const handleTimelineScroll = () => {
    const el = timelineRef.current;
    if (!el) return;
    const dist = el.scrollHeight - el.scrollTop - el.clientHeight;
    const pinned = dist < 96;
    isPinnedBottomRef.current = pinned;
    if (pinned) {
      setHasNewBelow(false);
    }
  };

  // Auto-scroll on new canonical messages or streaming chunks if pinned to bottom
  useEffect(() => {
    if (!detail || detail.stage !== 'chat') return;
    if (isPinnedBottomRef.current) {
      scrollToBottom();
    } else {
      setHasNewBelow(true);
    }
  }, [detail?.messages.length, streamedByRunId, detail?.latest_turn?.state, detail, scrollToBottom]);

  // Connect SSE stream for active unresolved non-quiet turns
  const activeTurnId =
    detail?.stage === 'chat' &&
    detail.latest_turn &&
    detail.latest_turn.mode !== 'quiet' &&
    detail.latest_turn.state !== 'resolved'
      ? detail.latest_turn.id
      : null;

  useEffect(() => {
    if (!activeTurnId) return;
    setStreamedByRunId({});

    const seenSeqByTurn = { max: 0n };

    const disconnect = connectTurnEventStream({
      turnId: activeTurnId,
      initialAfterSeq: '0',
      onFrame: (frame) => {
        if (frame.id && /^\d+$/.test(frame.id)) {
          const seq = BigInt(frame.id);
          if (seq <= seenSeqByTurn.max) {
            return;
          }
          seenSeqByTurn.max = seq;
        }

        if (frame.event === 'run.delta') {
          try {
            const parsed = JSON.parse(frame.data) as { run_id?: string; text?: string };
            if (parsed.run_id && typeof parsed.text === 'string') {
              const runId = parsed.run_id;
              const delta = parsed.text;
              setStreamedByRunId((prev) => ({
                ...prev,
                [runId]: (prev[runId] ?? '') + delta,
              }));
            }
          } catch {
            // ignore malformed frame
          }
          return;
        }

        if (
          frame.event === 'run.completed' ||
          frame.event === 'run.failed' ||
          frame.event === 'run.blocked' ||
          frame.event === 'run.cancelled' ||
          frame.event === 'run.retried' ||
          frame.event === 'turn.selection_required' ||
          frame.event === 'turn.resolved'
        ) {
          void refetchConversation();
          if (frame.event === 'turn.resolved') {
            void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
          }
        }
      },
      onCursorExpired: async () => {
        try {
          const snap = await api.getTurn(activeTurnId);
          const seeded: Record<string, string> = {};
          for (const r of snap.runs) {
            seeded[r.id] = r.output_text ?? '';
          }
          setStreamedByRunId(seeded);
          void refetchConversation();
          if (snap.state === 'resolved') return null;
          return snap.last_event_seq;
        } catch {
          return null;
        }
      },
      onPurged: () => {
        setPurgedState(true);
      },
    });

    return () => {
      disconnect();
    };
  }, [activeTurnId, refetchConversation, queryClient]);

  // Handle purged or completion view (never renders raw conversation history)
  const is410Purged =
    purgedState ||
    (queryError instanceof ApiClientError && queryError.code === 'CONTENT_PURGED');

  if (completionSummary || is410Purged) {
    const summaryFlower = completionSummary?.flower;
    return (
      <div className={styles.centeredBox} data-testid="purged-completion-view">
        <div className={styles.sereneCard}>
          {summaryFlower && !completionSummary?.discarded && (
            <FlowerBadge
              visual={summaryFlower.visual_snapshot}
              styleSeed={summaryFlower.style_seed}
              speciesName={summaryFlower.species_name}
              size={96}
            />
          )}
          <h1 className={styles.sereneTitle}>
            {completionSummary
              ? completionSummary.kind === 'release'
                ? '봉투 속 이야기를 멀리 떠나보냈어요'
                : completionSummary.discarded
                  ? '작은 꽃을 바람에 조용히 흘려보냈어요'
                  : '꽃밭에 새로운 꽃 한 송이를 심었어요'
              : '이미 마무리되어 원문이 정리된 이야기예요'}
          </h1>
          <p className={styles.sereneBody}>
            주고받았던 대화 원문과 초안, 스트리밍 기록은 모두 즉시 삭제되어 어디에도 남지 않습니다.
            {summaryFlower && !completionSummary?.discarded && summaryFlower.kept_line
              ? ` 남기신 짧은 한 줄(“${summaryFlower.kept_line}”)만 꽃과 함께 꽃밭에 간직되었어요.`
              : ''}
          </p>
          <div className={styles.sereneActions}>
            <Link to="/" className={styles.secondaryLinkBtn}>
              홈으로 돌아가기
            </Link>
            <Link to="/garden" className={styles.primaryGrowLinkBtn} data-testid="go-to-garden-btn">
              꽃밭 둘러보기 →
            </Link>
          </div>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return <p className={styles.loadingState}>대화를 불러오는 중…</p>;
  }

  if (queryError || !detail) {
    return (
      <div className={styles.centeredBox}>
        <div className={styles.sereneCard}>
          <h1 className={styles.sereneTitle}>대화를 찾을 수 없어요</h1>
          <p className={styles.sereneBody}>
            {queryError instanceof ApiClientError
              ? queryError.message
              : '요청하신 대화가 존재하지 않거나 접근할 수 없어요.'}
          </p>
          <Link to="/" className={styles.secondaryLinkBtn}>
            홈으로 돌아가기
          </Link>
        </div>
      </div>
    );
  }

  // Stage 2: Envelope Ritual or Flower Planting Ritual
  if ((detail.stage === 'release_ritual' || detail.stage === 'planting') && detail.active_ritual) {
    return (
      <RitualController
        initialRitual={detail.active_ritual}
        flower={detail.flower_gift}
        reducedMotion={bootstrap?.settings.reduced_motion}
        onRitualUpdated={(nextRitual: RitualView) => {
          queryClient.setQueryData(['conversation', id], {
            ...detail,
            active_ritual: nextRitual,
          });
        }}
        onReturnedToChat={() => {
          void refetchConversation();
          void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
        }}
        onCompleted={({ kind, flower }) => {
          setCompletionSummary({ kind, flower });
        }}
      />
    );
  }

  // Stage 3: Grow Flower Gift Ready
  if (detail.stage === 'gift_ready' && detail.flower_gift) {
    return (
      <GiftReadyView
        conversationId={detail.id}
        conversationVersion={detail.version}
        initialFlower={detail.flower_gift}
        maxKeptLineChars={bootstrap?.capabilities.kept_line_max_chars ?? 80}
        onPlantingStarted={() => {
          void refetchConversation();
        }}
        onDiscarded={(discardedFlower) => {
          setCompletionSummary({
            kind: 'grow',
            discarded: true,
            flower: discardedFlower,
          });
        }}
      />
    );
  }

  // Stage 1: Active Chat Workspace
  const isRelease = detail.kind === 'release';
  const latestTurn = detail.latest_turn;
  const turnBlockingComposer = Boolean(latestTurn && latestTurn.state !== 'resolved');
  const charCount = Array.from(composerText).length;
  const maxChars = bootstrap?.capabilities.message_max_chars ?? 4000;

  const applyModeChange = async (nextMode: GenerationMode) => {
    setErrorMsg(null);
    try {
      const updated = await api.updatePreferences(detail.id, {
        expected_version: detail.version,
        generation_mode: nextMode,
      });
      queryClient.setQueryData(['conversation', id], updated);
    } catch (err) {
      if (err instanceof ApiClientError) setErrorMsg(err.message);
    }
  };

  const handleSelectMode = (nextMode: GenerationMode) => {
    if (nextMode === 'compare' && bootstrap && !bootstrap.settings.compare_notice_ack) {
      setPendingCompareAck(true);
      return;
    }
    void applyModeChange(nextMode);
  };

  const handleAckCompareAndApply = async () => {
    setPendingCompareAck(false);
    if (bootstrap) {
      try {
        await api.updateAISettings({
          expected_version: bootstrap.settings.version,
          compare_notice_ack: true,
        });
        void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      } catch {
        // non-fatal
      }
    }
    await applyModeChange('compare');
  };

  const handleToggleReleasePolicy = async (policy: ReplyPolicy) => {
    setErrorMsg(null);
    try {
      const updated = await api.updatePreferences(detail.id, {
        expected_version: detail.version,
        reply_policy: policy,
      });
      queryClient.setQueryData(['conversation', id], updated);
    } catch (err) {
      if (err instanceof ApiClientError) setErrorMsg(err.message);
    }
  };

  const handleSubmitMessage = async () => {
    const trimmed = composerText.trim();
    if (!trimmed || submittingTurn || turnBlockingComposer || charCount > maxChars) return;

    if (draftTimerRef.current) {
      window.clearTimeout(draftTimerRef.current);
      draftTimerRef.current = null;
    }

    setSubmittingTurn(true);
    setErrorMsg(null);
    isPinnedBottomRef.current = true;

    try {
      const clientTurnId = createIdempotencyKey('turn');
      let res: Awaited<ReturnType<typeof api.submitTurn>>;
      try {
        res = await api.submitTurn(
          detail.id,
          {
            client_turn_id: clientTurnId,
            expected_version: detail.version,
            text: trimmed,
          },
          clientTurnId
        );
      } catch (firstErr) {
        if (firstErr instanceof ApiClientError && firstErr.code === 'VERSION_CONFLICT') {
          const refreshed = await refetchConversation();
          if (!refreshed.data) throw firstErr;
          res = await api.submitTurn(
            detail.id,
            {
              client_turn_id: clientTurnId,
              expected_version: refreshed.data.version,
              text: trimmed,
            },
            clientTurnId
          );
        } else {
          throw firstErr;
        }
      }
      draftVersionRef.current = res.draft_version;
      lastSavedTextRef.current = '';
      setComposerText('');
      setDraftStatus('idle');
      await refetchConversation();
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg('메시지를 보내는 중 오류가 발생했어요.');
      }
    } finally {
      setSubmittingTurn(false);
    }
  };

  // Korean IME-safe Enter handler (Spec Section 28 CHAT05)
  const handleComposerKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      if (e.nativeEvent.isComposing || e.keyCode === 229) {
        return;
      }
      e.preventDefault();
      void handleSubmitMessage();
    }
  };

  const executeClosure = async (forceDiscardAndCancel: boolean) => {
    if (closingBusy) return;
    setClosingBusy(true);
    setErrorMsg(null);
    if (draftTimerRef.current) {
      window.clearTimeout(draftTimerRef.current);
      draftTimerRef.current = null;
    }
    try {
      await api.beginClosure(detail.id, {
        expected_version: detail.version,
        cancel_pending: forceDiscardAndCancel,
        draft_disposition: forceDiscardAndCancel ? 'discard' : 'require_empty',
      });
      setClosureConfirmOpen(false);
      setComposerText('');
      lastSavedTextRef.current = '';
      await refetchConversation();
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
    } catch (err) {
      if (err instanceof ApiClientError) {
        if (
          !forceDiscardAndCancel &&
          (err.code === 'UNSENT_DRAFT' || err.code === 'TURN_UNRESOLVED')
        ) {
          setClosureConfirmOpen(true);
        } else {
          setErrorMsg(err.message);
        }
      } else {
        setErrorMsg('마무리 단계로 넘어가는 중 문제가 생겼어요.');
      }
    } finally {
      setClosingBusy(false);
    }
  };

  const handleClosureClick = () => {
    if (composerText.trim().length > 0 || turnBlockingComposer) {
      setClosureConfirmOpen(true);
      return;
    }
    void executeClosure(false);
  };

  return (
    <div className={styles.chatPage} data-testid="conversation-chat-view">
      {/* Sticky Character & Mode Header */}
      <section className={styles.chatHeader}>
        <div className={styles.personGroup}>
          <PortraitRenderer
            appearance={detail.character.appearance}
            expression={detail.expression_state}
            name={detail.character.name}
            size={68}
          />
          <div className={styles.personMeta}>
            <div className={styles.badgeLine}>
              <span className={isRelease ? styles.releaseBadge : styles.growBadge}>
                {isRelease ? '털어놓기 · 비움' : '고민 나누기 · 피움'}
              </span>
              <span className={styles.expChip}>
                {detail.expression_state === 'challenging'
                  ? '도전적인 표정'
                  : detail.expression_state === 'apologetic'
                    ? '미안해하는 표정'
                    : '귀 기울이는 표정'}
              </span>
            </div>
            <h1 className={styles.charTitle}>{detail.character.name}</h1>
          </div>
        </div>

        <div className={styles.headerControls}>
          {isRelease && (
            <div className={styles.segmented} role="group" aria-label="비움 응답 방식">
              <button
                type="button"
                aria-pressed={detail.reply_policy === 'apology'}
                disabled={turnBlockingComposer}
                onClick={() => void handleToggleReleasePolicy('apology')}
                className={`${styles.segBtn} ${
                  detail.reply_policy === 'apology' ? styles.segActive : ''
                }`}
              >
                사과·인정
              </button>
              <button
                type="button"
                aria-pressed={detail.reply_policy === 'quiet'}
                disabled={turnBlockingComposer}
                onClick={() => void handleToggleReleasePolicy('quiet')}
                className={`${styles.segBtn} ${
                  detail.reply_policy === 'quiet' ? styles.segActive : ''
                }`}
                data-testid="toggle-quiet-policy"
              >
                무응답(조용히)
              </button>
            </div>
          )}

          {detail.reply_policy !== 'quiet' && (
            <div className={styles.segmented} role="group" aria-label="AI 생성 모드">
              {(
                [
                  ['openai', 'OpenAI'],
                  ['gemini', 'Gemini'],
                  ['compare', '두 모델 비교'],
                ] as const
              ).map(([mVal, mLabel]) => (
                <button
                  key={mVal}
                  type="button"
                  aria-pressed={detail.generation_mode === mVal}
                  disabled={turnBlockingComposer}
                  onClick={() => handleSelectMode(mVal)}
                  className={`${styles.segBtn} ${
                    detail.generation_mode === mVal ? styles.segActive : ''
                  }`}
                  data-testid={`mode-${mVal}-btn`}
                >
                  {mLabel}
                </button>
              ))}
            </div>
          )}

          <button
            type="button"
            disabled={closingBusy}
            onClick={handleClosureClick}
            className={isRelease ? styles.closureReleaseBtn : styles.closureGrowBtn}
            data-testid="begin-closure-btn"
          >
            {isRelease ? '봉투에 담아 마무리하기' : '대화 마치고 꽃 선물 받기'}
          </button>
        </div>
      </section>

      {errorMsg && (
        <div className={styles.errorBanner} role="alert">
          {errorMsg}
        </div>
      )}

      {/* Canonical Message Timeline */}
      <div
        ref={timelineRef}
        onScroll={handleTimelineScroll}
        className={styles.timeline}
        data-testid="message-timeline"
      >
        {detail.messages.map((msg) => {
          const isUser = msg.role === 'user';
          return (
            <div
              key={msg.id}
              className={`${styles.msgRow} ${isUser ? styles.msgRowUser : styles.msgRowAssistant}`}
              data-testid={`message-${msg.role}-${msg.seq}`}
            >
              <div
                className={`${styles.bubble} ${
                  isUser ? styles.userBubble : styles.assistantBubble
                }`}
              >
                {!isUser && (
                  <span className={styles.speakerLabel}>
                    {detail.character.name}
                    {msg.origin === 'opening' ? ' · 첫마디' : ''}
                  </span>
                )}
                <p className={styles.msgText}>{msg.content}</p>
              </div>
            </div>
          );
        })}

        {latestTurn && (
          <CandidateComparisonPanel
            kind={detail.kind}
            turn={latestTurn}
            streamedTextByRunId={streamedByRunId}
            detailedEvalEnabled={Boolean(bootstrap?.settings.detailed_eval_enabled)}
            isBusy={turnActionBusy}
            onSelectRun={async (runId) => {
              setTurnActionBusy(true);
              setErrorMsg(null);
              try {
                await api.selectCandidate(latestTurn.id, latestTurn.version, runId);
                await refetchConversation();
                void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
              } catch (err) {
                if (err instanceof ApiClientError) setErrorMsg(err.message);
                void refetchConversation();
              } finally {
                setTurnActionBusy(false);
              }
            }}
            onSkipTurn={async () => {
              setTurnActionBusy(true);
              setErrorMsg(null);
              try {
                await api.skipTurn(latestTurn.id, latestTurn.version);
                await refetchConversation();
              } catch (err) {
                if (err instanceof ApiClientError) setErrorMsg(err.message);
                void refetchConversation();
              } finally {
                setTurnActionBusy(false);
              }
            }}
            onCancelRun={async (runId) => {
              setTurnActionBusy(true);
              setErrorMsg(null);
              try {
                await api.cancelRun(runId, latestTurn.version);
                await refetchConversation();
              } catch (err) {
                if (err instanceof ApiClientError) setErrorMsg(err.message);
              } finally {
                setTurnActionBusy(false);
              }
            }}
            onRetryRun={async (runId) => {
              setTurnActionBusy(true);
              setErrorMsg(null);
              try {
                await api.retryRun(runId, latestTurn.version);
                await refetchConversation();
              } catch (err) {
                if (err instanceof ApiClientError) setErrorMsg(err.message);
              } finally {
                setTurnActionBusy(false);
              }
            }}
            onSubmitFeedback={async (scores) => {
              await api.submitFeedback(latestTurn.id, scores);
              await refetchConversation();
            }}
          />
        )}
      </div>

      {hasNewBelow && (
        <button
          type="button"
          className={styles.newContentPill}
          onClick={scrollToBottom}
        >
          ↓ 새 내용 보기
        </button>
      )}

      {/* Composer */}
      <section className={styles.composerCard}>
        {turnBlockingComposer && (
          <p className={styles.blockingNotice}>
            위 답변이 마무리되거나 후보를 선택(또는 건너뛰기)하면 다음 말을 적을 수 있어요.
          </p>
        )}
        <textarea
          rows={3}
          className={styles.textarea}
          disabled={turnBlockingComposer || submittingTurn}
          value={composerText}
          onChange={(e) => setComposerText(e.target.value)}
          onKeyDown={handleComposerKeyDown}
          placeholder={
            isRelease
              ? '속에 담아두었던 말을 편하게 적어 보세요. (Enter 전송, Shift+Enter 줄바꿈)'
              : '요즘 마음에 걸리는 고민을 천천히 적어 보세요. (Enter 전송, Shift+Enter 줄바꿈)'
          }
          aria-label="대화 메시지 입력"
          data-testid="chat-composer"
        />
        <div className={styles.composerFooter}>
          <div className={styles.composerMeta}>
            <span className={charCount > maxChars ? styles.charOver : ''}>
              {charCount} / {maxChars}자
            </span>
            {draftStatus === 'saving' && <span>· 초안 저장 중…</span>}
            {draftStatus === 'saved' && <span>· 초안 저장됨</span>}
          </div>

          <button
            type="button"
            disabled={
              submittingTurn ||
              turnBlockingComposer ||
              composerText.trim().length === 0 ||
              charCount > maxChars
            }
            onClick={() => void handleSubmitMessage()}
            className={isRelease ? styles.sendReleaseBtn : styles.sendGrowBtn}
            data-testid="send-message-btn"
          >
            {submittingTurn
              ? '보내는 중…'
              : detail.reply_policy === 'quiet'
                ? '조용히 털어놓기'
                : '마음 건네기'}
          </button>
        </div>
      </section>

      {/* First-time Compare Mode Cost/Latency Notice Modal */}
      {pendingCompareAck && (
        <div className={styles.modalBackdrop} role="dialog" aria-modal="true">
          <div className={styles.modalCard} data-testid="compare-notice-modal">
            <h2 className={styles.modalTitle}>두 모델 비교 모드 안내</h2>
            <p className={styles.modalBody}>
              두 모델 비교 모드에서는 같은 대화 맥락으로 OpenAI와 Gemini 두 답변을 동시에 생성해요.
              단독 모드보다 응답 대기 시간과 토큰 사용량(실제 키 연결 시 예상 비용)이 약 2배로 늘어납니다.
            </p>
            <div className={styles.modalActions}>
              <button
                type="button"
                className={styles.modalCancelBtn}
                onClick={() => setPendingCompareAck(false)}
              >
                취소
              </button>
              <button
                type="button"
                className={styles.sendGrowBtn}
                onClick={() => void handleAckCompareAndApply()}
                data-testid="confirm-compare-notice-btn"
              >
                확인하고 비교 모드 켜기
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Unsent Draft / Pending Turn Closure Confirmation Modal */}
      {closureConfirmOpen && (
        <div className={styles.modalBackdrop} role="dialog" aria-modal="true">
          <div className={styles.modalCard} data-testid="closure-confirm-modal">
            <h2 className={styles.modalTitle}>대화를 마무리할까요?</h2>
            <p className={styles.modalBody}>
              아직 보내지 않은 초안이 있거나 답변 생성이 진행 중이에요. 지금 마무리 단계로 넘어가면 작성 중인
              초안을 비우고 진행 중인 생성을 멈춘 뒤 마무리를 시작합니다.
            </p>
            <div className={styles.modalActions}>
              <button
                type="button"
                className={styles.modalCancelBtn}
                onClick={() => setClosureConfirmOpen(false)}
              >
                대화로 돌아가기
              </button>
              <button
                type="button"
                disabled={closingBusy}
                className={isRelease ? styles.sendReleaseBtn : styles.sendGrowBtn}
                onClick={() => void executeClosure(true)}
                data-testid="confirm-closure-discard-btn"
              >
                초안 비우고 마무리 시작
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
