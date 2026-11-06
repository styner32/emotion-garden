import React, { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { ConversationKind, ReplyPolicy, SlotSummary } from '../api/types';
import { PortraitRenderer } from '../characters/PortraitRenderer';
import styles from './HomePage.module.css';

export const HomePage: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const queryClient = useQueryClient();

  const { data: bootstrap, isLoading: bootLoading } = useQuery({
    queryKey: ['bootstrap'],
    queryFn: () => api.getBootstrap(),
  });

  const { data: catalog } = useQuery({
    queryKey: ['avatar-catalog'],
    queryFn: () => api.getAvatarCatalog(),
  });

  const { data: charData } = useQuery({
    queryKey: ['characters', false],
    queryFn: () => api.listCharacters(false),
  });

  const [openLauncher, setOpenLauncher] = useState<ConversationKind | null>(null);
  const [selectedCharId, setSelectedCharId] = useState<Record<ConversationKind, string>>({
    release: '',
    grow: '',
  });
  const [releaseGreetingKey, setReleaseGreetingKey] = useState<string>('');
  const [releasePolicy, setReleasePolicy] = useState<ReplyPolicy>('apology');
  const [creatingKind, setCreatingKind] = useState<ConversationKind | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const characters = charData?.characters ?? [];
  const releaseChars = useMemo(
    () => characters.filter((c) => c.kind === 'release' && !c.archived_at),
    [characters]
  );
  const growChars = useMemo(
    () => characters.filter((c) => c.kind === 'grow' && !c.archived_at),
    [characters]
  );

  // Initialize default selections when catalog/characters load or query params request launch
  useEffect(() => {
    if (catalog && !releaseGreetingKey) {
      setReleaseGreetingKey(catalog.greetings.release.default_key);
    }
  }, [catalog, releaseGreetingKey]);

  useEffect(() => {
    setSelectedCharId((prev) => ({
      release: prev.release || releaseChars[0]?.id || '',
      grow: prev.grow || growChars[0]?.id || '',
    }));
  }, [releaseChars, growChars]);

  useEffect(() => {
    const launchKind = searchParams.get('launch');
    const charId = searchParams.get('characterId');
    if (launchKind === 'release' || launchKind === 'grow') {
      setOpenLauncher(launchKind);
      if (charId) {
        setSelectedCharId((prev) => ({ ...prev, [launchKind]: charId }));
      }
      setSearchParams({}, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const handleStartConversation = async (kind: ConversationKind) => {
    const charId = selectedCharId[kind];
    if (!charId || creatingKind) return;
    setCreatingKind(kind);
    setErrorMsg(null);
    try {
      const created = await api.createConversation({
        kind,
        character_id: charId,
        greeting_key:
          kind === 'release'
            ? releaseGreetingKey || catalog?.greetings.release.default_key
            : catalog?.greetings.grow.default_key,
        reply_policy: kind === 'release' ? releasePolicy : 'supportive',
      });
      await queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      navigate(`/conversations/${created.id}`);
    } catch (err) {
      if (err instanceof ApiClientError) {
        if (err.code === 'ACTIVE_CONVERSATION_EXISTS') {
          const existingId = err.details?.conversation_id;
          await queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
          if (typeof existingId === 'string' && existingId) {
            navigate(`/conversations/${existingId}`);
            return;
          }
        }
        setErrorMsg(err.message);
      } else {
        setErrorMsg('대화를 여는 중 문제가 생겼어요.');
      }
    } finally {
      setCreatingKind(null);
    }
  };

  const renderActiveSlot = (slot: SlotSummary, kind: ConversationKind) => {
    const isRelease = kind === 'release';
    let ctaLabel = '대화 이어가기';
    if (slot.stage === 'release_ritual') ctaLabel = '비움 마무리 이어가기';
    else if (slot.stage === 'gift_ready') ctaLabel = '건네받은 꽃 확인하기';
    else if (slot.stage === 'planting') ctaLabel = '꽃 심기 이어가기';

    return (
      <div className={styles.activeBox} data-testid={`active-slot-${kind}`}>
        <div className={styles.activeRow}>
          <PortraitRenderer
            appearance={slot.character.appearance}
            expression={
              isRelease
                ? slot.reply_policy === 'apology'
                  ? 'apologetic'
                  : 'challenging'
                : 'listening'
            }
            name={slot.character.name}
            size={68}
          />
          <div className={styles.activeMeta}>
            <span className={styles.statusChip}>{slot.ui_status_label}</span>
            <strong className={styles.charName}>{slot.character.name}</strong>
            <span className={styles.modeNote}>
              {isRelease
                ? slot.reply_policy === 'quiet'
                  ? '조용히 들어주기(무응답)'
                  : `사과·인정 모드 · ${slot.generation_mode}`
                : `공감 대화 · ${slot.generation_mode}`}
            </span>
          </div>
        </div>
        <button
          type="button"
          className={isRelease ? styles.releasePrimaryBtn : styles.growPrimaryBtn}
          onClick={() => navigate(`/conversations/${slot.conversation_id}`)}
          data-testid={`resume-${kind}-btn`}
        >
          {ctaLabel} →
        </button>
      </div>
    );
  };

  const renderLauncher = (kind: ConversationKind) => {
    const isRelease = kind === 'release';
    const list = isRelease ? releaseChars : growChars;
    const chosenId = selectedCharId[kind];

    return (
      <div className={styles.launcherBox} data-testid={`launcher-${kind}`}>
        {list.length === 0 ? (
          <div className={styles.emptyCharsBox}>
            <p>
              아직 만든 {isRelease ? '비움(털어놓기)' : '피움(고민 나누기)'} 상대 인물이 없어요.
              먼저 대화할 인물의 얼굴과 인상을 만들어 주세요.
            </p>
            <Link
              to={`/characters/new?kind=${kind}`}
              className={isRelease ? styles.releasePrimaryBtn : styles.growPrimaryBtn}
              data-testid={`create-first-char-${kind}`}
            >
              + {isRelease ? '비움 인물 만들기' : '피움 인물 만들기'}
            </Link>
          </div>
        ) : (
          <>
            <div className={styles.fieldGroup}>
              <div className={styles.fieldLabelRow}>
                <span className={styles.fieldLabel}>대화 상대 고르기</span>
                <Link to={`/characters/new?kind=${kind}`} className={styles.smallLink}>
                  + 새 인물 추가
                </Link>
              </div>
              <div className={styles.charPickList}>
                {list.map((ch) => {
                  const selected = ch.id === chosenId;
                  return (
                    <button
                      key={ch.id}
                      type="button"
                      onClick={() =>
                        setSelectedCharId((prev) => ({ ...prev, [kind]: ch.id }))
                      }
                      className={`${styles.charPickItem} ${
                        selected ? styles.charPickSelected : ''
                      }`}
                      data-testid={`pick-char-${ch.id}`}
                    >
                      <PortraitRenderer
                        appearance={ch.appearance}
                        expression={isRelease ? 'challenging' : 'listening'}
                        name={ch.name}
                        size={48}
                      />
                      <span className={styles.charPickName}>{ch.name}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            {isRelease && catalog && (
              <>
                <div className={styles.fieldGroup}>
                  <label htmlFor="release-greeting-select" className={styles.fieldLabel}>
                    상대의 첫마디
                  </label>
                  <select
                    id="release-greeting-select"
                    className={styles.select}
                    value={releaseGreetingKey}
                    onChange={(e) => setReleaseGreetingKey(e.target.value)}
                  >
                    {catalog.greetings.release.options.map((opt) => (
                      <option key={opt.key} value={opt.key}>
                        “{opt.text}”
                      </option>
                    ))}
                  </select>
                </div>

                <div className={styles.fieldGroup}>
                  <span className={styles.fieldLabel}>응답 방식</span>
                  <div className={styles.policyToggle}>
                    <button
                      type="button"
                      className={`${styles.policyBtn} ${
                        releasePolicy === 'apology' ? styles.policyBtnActive : ''
                      }`}
                      onClick={() => setReleasePolicy('apology')}
                      data-testid="policy-apology-btn"
                    >
                      <strong>사과·인정 받기</strong>
                      <span>내 감정을 인정하고 진심 어린 사과를 건넵니다</span>
                    </button>
                    <button
                      type="button"
                      className={`${styles.policyBtn} ${
                        releasePolicy === 'quiet' ? styles.policyBtnActive : ''
                      }`}
                      onClick={() => setReleasePolicy('quiet')}
                      data-testid="policy-quiet-btn"
                    >
                      <strong>조용히 들어주기 (무응답)</strong>
                      <span>AI 호출 없이 오직 내 말만 쏟아내고 봉투에 담습니다</span>
                    </button>
                  </div>
                </div>
              </>
            )}

            <div className={styles.launcherActions}>
              <button
                type="button"
                className={styles.cancelLinkBtn}
                onClick={() => setOpenLauncher(null)}
              >
                닫기
              </button>
              <button
                type="button"
                className={isRelease ? styles.releasePrimaryBtn : styles.growPrimaryBtn}
                disabled={!chosenId || creatingKind !== null}
                onClick={() => void handleStartConversation(kind)}
                data-testid={`confirm-start-${kind}`}
              >
                {creatingKind === kind ? '대화 여는 중…' : '이 상대와 대화 시작하기'}
              </button>
            </div>
          </>
        )}
      </div>
    );
  };

  const releaseSlot = bootstrap?.active_conversations.release ?? null;
  const growSlot = bootstrap?.active_conversations.grow ?? null;

  return (
    <div className={styles.page}>
      <section className={styles.hero}>
        <h1 className={styles.heroTitle}>오늘 마음에 쌓인 말을 어디에 두시겠어요?</h1>
        <p className={styles.heroLead}>
          답답하고 서운했던 감정은 봉투에 담아 멀리 떠나보내고(비움), 조용히 곱씹고 싶은 고민은 따뜻한
          대화 끝에 한 송이 꽃으로 심어 두세요(피움).
        </p>
      </section>

      {errorMsg && (
        <div className={styles.errorBanner} role="alert">
          {errorMsg}
        </div>
      )}

      <section className={styles.dualGrid} aria-label="대화 공간 선택">
        {/* Release Card */}
        <article className={`${styles.modeCard} ${styles.releaseCard}`} data-testid="home-card-release">
          <div className={styles.cardHeader}>
            <span className={styles.releaseBadge}>털어놓기 · 비움</span>
            <h2 className={styles.cardTitle}>답답한 마음 쏟아내고 떠나보내기</h2>
            <p className={styles.cardDesc}>
              하고 싶었지만 삼켜야 했던 말을 쏟아내세요. 상대의 진심 어린 사과를 듣거나 아무 응답 없이
              털어놓은 뒤, 봉투에 담아 바다·불꽃·땅으로 떠나보내면 대화 원문이 남김없이 사라집니다.
            </p>
          </div>

          {bootLoading ? (
            <p className={styles.loadingText}>불러오는 중…</p>
          ) : releaseSlot ? (
            renderActiveSlot(releaseSlot, 'release')
          ) : openLauncher === 'release' ? (
            renderLauncher('release')
          ) : (
            <button
              type="button"
              className={styles.releasePrimaryBtn}
              onClick={() => setOpenLauncher('release')}
              data-testid="open-launcher-release"
            >
              새 비움 대화 시작하기
            </button>
          )}
        </article>

        {/* Grow Card */}
        <article className={`${styles.modeCard} ${styles.growCard}`} data-testid="home-card-grow">
          <div className={styles.cardHeader}>
            <span className={styles.growBadge}>고민 나누기 · 피움</span>
            <h2 className={styles.cardTitle}>차분히 마음 살피고 꽃 한 송이 심기</h2>
            <p className={styles.cardDesc}>
              섣부른 훈계 없이 내 이야기를 경청하는 상대와 마음을 정리해 보세요. 대화를 마치면 건네받은 꽃을
              꽃밭에 직접 심고, 원문 대신 내가 적은 짧은 한 줄만 남길 수 있어요.
            </p>
          </div>

          {bootLoading ? (
            <p className={styles.loadingText}>불러오는 중…</p>
          ) : growSlot ? (
            renderActiveSlot(growSlot, 'grow')
          ) : openLauncher === 'grow' ? (
            renderLauncher('grow')
          ) : (
            <button
              type="button"
              className={styles.growPrimaryBtn}
              onClick={() => setOpenLauncher('grow')}
              data-testid="open-launcher-grow"
            >
              새 피움 대화 시작하기
            </button>
          )}
        </article>
      </section>

      {bootstrap && (
        <section className={styles.gardenBanner}>
          <div className={styles.gardenBannerInfo}>
            <h2 className={styles.gardenBannerTitle}>나의 작은 꽃밭</h2>
            <p className={styles.gardenBannerSub}>
              현재 가로 {bootstrap.garden_summary.cols}칸 × 세로 {bootstrap.garden_summary.rows}칸 중{' '}
              <strong>{bootstrap.garden_summary.planted_count}송이</strong>의 꽃이 피어 있어요.
              {bootstrap.garden_summary.has_reserved ? ' (심는 중인 자리 1곳 있음)' : ''}
            </p>
          </div>
          <Link to="/garden" className={styles.gardenVisitBtn} data-testid="home-garden-link">
            꽃밭 둘러보기 →
          </Link>
        </section>
      )}
    </div>
  );
};
