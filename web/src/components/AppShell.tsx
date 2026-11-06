import React, { useEffect } from 'react';
import { NavLink } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, onConversationPurged } from '../api/client';
import styles from './AppShell.module.css';

export interface AppShellProps {
  children: React.ReactNode;
}

export const AppShell: React.FC<AppShellProps> = ({ children }) => {
  const queryClient = useQueryClient();

  const { data: bootstrap } = useQuery({
    queryKey: ['bootstrap'],
    queryFn: () => api.getBootstrap(),
  });

  // Evict purged conversation caches across same-tab and BroadcastChannel events (DATA04)
  useEffect(() => {
    return onConversationPurged((conversationId) => {
      if (conversationId) {
        queryClient.removeQueries({ queryKey: ['conversation', conversationId] });
      }
      void queryClient.invalidateQueries({ queryKey: ['bootstrap'] });
      void queryClient.invalidateQueries({ queryKey: ['active-conversations'] });
      void queryClient.invalidateQueries({ queryKey: ['garden'] });
      void queryClient.invalidateQueries({ queryKey: ['comparisons'] });
    });
  }, [queryClient]);

  const caps = bootstrap?.capabilities;
  const hasLiveKeys = Boolean(caps?.openai_key_configured || caps?.gemini_key_configured);

  return (
    <div
      className={styles.shell}
      data-reduced-motion={bootstrap?.settings.reduced_motion ? 'true' : 'false'}
    >
      <header className={styles.header}>
        <div className={styles.headerInner}>
          <div className={styles.brandRow}>
            <NavLink to="/" className={styles.brand} end>
              <span className={styles.brandSeal} aria-hidden="true">
                ✿
              </span>
              <span className={styles.brandText}>
                <strong className={styles.brandTitle}>마음 한 칸</strong>
                <span className={styles.brandSub}>비움과 피움의 조용한 기록장</span>
              </span>
            </NavLink>

            {caps && (
              <NavLink
                to="/settings/ai"
                className={`${styles.modePill} ${hasLiveKeys ? styles.modeLive : styles.modeMock}`}
                title="AI 연결 상태 및 모델 설정 보기"
              >
                {hasLiveKeys ? '실시간 AI 연결됨' : '체험용 Mock 응답 모드'}
              </NavLink>
            )}
          </div>

          <nav className={styles.nav} aria-label="주요 메뉴">
            <NavLink
              to="/"
              end
              className={({ isActive }) =>
                `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
              }
            >
              홈
            </NavLink>
            <NavLink
              to="/characters"
              className={({ isActive }) =>
                `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
              }
            >
              등장인물
            </NavLink>
            <NavLink
              to="/garden"
              className={({ isActive }) =>
                `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
              }
            >
              꽃밭
              {bootstrap && bootstrap.garden_summary.planted_count > 0 && (
                <span className={styles.countBadge}>{bootstrap.garden_summary.planted_count}</span>
              )}
            </NavLink>
            <NavLink
              to="/settings/ai"
              className={({ isActive }) =>
                `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
              }
            >
              AI 설정
            </NavLink>
            <NavLink
              to="/settings/comparisons"
              className={({ isActive }) =>
                `${styles.navLink} ${isActive ? styles.navLinkActive : ''}`
              }
            >
              비교 기록
            </NavLink>
          </nav>
        </div>
      </header>

      <main className={styles.main}>{children}</main>

      <footer className={styles.footer}>
        <p>
          마무리된 대화의 원문은 서버에서 즉시 삭제되며, 피움의 꽃밭에는 오직 꽃과 직접 남긴 짧은 한 줄만 남습니다.
        </p>
      </footer>
    </div>
  );
};
