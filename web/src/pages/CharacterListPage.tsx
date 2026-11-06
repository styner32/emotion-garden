import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiClientError, api } from '../api/client';
import { Character } from '../api/types';
import { PortraitRenderer } from '../characters/PortraitRenderer';
import styles from './CharacterListPage.module.css';

type FilterTab = 'all' | 'release' | 'grow' | 'archived';

function genderLabel(ch: Character): string {
  switch (ch.gender) {
    case 'female':
      return '여성';
    case 'male':
      return '남성';
    case 'custom':
      return ch.gender_custom || '직접 입력';
    default:
      return '성별 미지정';
  }
}

export const CharacterListPage: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<FilterTab>('all');
  const [archivingId, setArchivingId] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['characters', true],
    queryFn: () => api.listCharacters(true),
  });

  const allCharacters = data?.characters ?? [];
  const filtered = allCharacters.filter((ch) => {
    const isArch = Boolean(ch.archived_at);
    if (tab === 'archived') return isArch;
    if (isArch) return false;
    if (tab === 'release') return ch.kind === 'release';
    if (tab === 'grow') return ch.kind === 'grow';
    return true;
  });

  const handleArchive = async (ch: Character) => {
    if (archivingId) return;
    setArchivingId(ch.id);
    setErrorMsg(null);
    try {
      await api.archiveCharacter(ch.id, ch.version);
      await queryClient.invalidateQueries({ queryKey: ['characters'] });
    } catch (err) {
      if (err instanceof ApiClientError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg('인물을 보관하는 중 오류가 발생했어요.');
      }
    } finally {
      setArchivingId(null);
    }
  };

  return (
    <div className={styles.page}>
      <div className={styles.headerRow}>
        <div>
          <h1 className={styles.title}>등장인물 도감</h1>
          <p className={styles.subtitle}>
            비움(털어놓기) 상대와 피움(고민 나누기) 상대를 따로 만들고 다듬을 수 있어요. 이미 시작된 대화의
            초상은 대화가 열릴 때의 모습으로 보존됩니다.
          </p>
        </div>

        <div className={styles.createButtons}>
          <Link
            to="/characters/new?kind=release"
            className={styles.createReleaseBtn}
            data-testid="new-release-char-btn"
          >
            + 비움 상대 만들기
          </Link>
          <Link
            to="/characters/new?kind=grow"
            className={styles.createGrowBtn}
            data-testid="new-grow-char-btn"
          >
            + 피움 상대 만들기
          </Link>
        </div>
      </div>

      <div className={styles.tabs} role="tablist" aria-label="등장인물 분류">
        {(
          [
            ['all', '전체'],
            ['release', '비움 상대'],
            ['grow', '피움 상대'],
            ['archived', '보관함'],
          ] as const
        ).map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={`${styles.tabBtn} ${tab === key ? styles.tabActive : ''}`}
            onClick={() => setTab(key)}
          >
            {label}
          </button>
        ))}
      </div>

      {errorMsg && (
        <div className={styles.errorBanner} role="alert">
          {errorMsg}
        </div>
      )}

      {isLoading ? (
        <p className={styles.emptyState}>인물 목록을 불러오고 있어요…</p>
      ) : filtered.length === 0 ? (
        <div className={styles.emptyCard}>
          <p>이 분류에 해당하는 인물이 아직 없어요.</p>
          {tab !== 'archived' && (
            <Link to="/characters/new?kind=grow" className={styles.createGrowBtn}>
              첫 등장인물 만들기
            </Link>
          )}
        </div>
      ) : (
        <div className={styles.grid}>
          {filtered.map((ch) => {
            const isArchived = Boolean(ch.archived_at);
            return (
              <article
                key={ch.id}
                className={styles.card}
                data-testid={`character-card-${ch.id}`}
              >
                <div className={styles.cardBody}>
                  <PortraitRenderer
                    appearance={ch.appearance}
                    expression={ch.kind === 'release' ? 'challenging' : 'listening'}
                    name={ch.name}
                    size={84}
                  />
                  <div className={styles.info}>
                    <div className={styles.badges}>
                      <span
                        className={
                          ch.kind === 'release' ? styles.badgeRelease : styles.badgeGrow
                        }
                      >
                        {ch.kind === 'release' ? '비움 상대' : '피움 상대'}
                      </span>
                      {isArchived && <span className={styles.badgeArchived}>보관됨</span>}
                    </div>
                    <h2 className={styles.charName}>{ch.name}</h2>
                    <p className={styles.metaText}>
                      {ch.age ? `${ch.age}세 · ` : ''}
                      {genderLabel(ch)}
                    </p>
                  </div>
                </div>

                <div className={styles.cardFooter}>
                  {!isArchived && (
                    <>
                      <button
                        type="button"
                        className={styles.startBtn}
                        onClick={() =>
                          navigate(`/?launch=${ch.kind}&characterId=${encodeURIComponent(ch.id)}`)
                        }
                      >
                        대화 열기
                      </button>
                      <Link
                        to={`/characters/${ch.id}/edit`}
                        className={styles.actionLink}
                        data-testid={`edit-char-${ch.id}`}
                      >
                        편집
                      </Link>
                      <Link
                        to={`/characters/new?cloneFrom=${encodeURIComponent(ch.id)}`}
                        className={styles.actionLink}
                      >
                        반대 역할 복제
                      </Link>
                      <button
                        type="button"
                        className={styles.archiveBtn}
                        disabled={archivingId === ch.id}
                        onClick={() => void handleArchive(ch)}
                        data-testid={`archive-char-${ch.id}`}
                      >
                        보관
                      </button>
                    </>
                  )}
                  {isArchived && (
                    <Link
                      to={`/characters/new?cloneFrom=${encodeURIComponent(ch.id)}`}
                      className={styles.actionLink}
                    >
                      새 인물로 복제 생성
                    </Link>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      )}
    </div>
  );
};
