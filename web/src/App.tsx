import React from 'react';
import { BrowserRouter, MemoryRouter, Navigate, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ApiClientError } from './api/client';
import { AppShell } from './components/AppShell';
import { AISettingsPage } from './pages/AISettingsPage';
import { CharacterFormPage } from './pages/CharacterFormPage';
import { CharacterListPage } from './pages/CharacterListPage';
import { ComparisonsPage } from './pages/ComparisonsPage';
import { ConversationPage } from './pages/ConversationPage';
import { GardenPage } from './pages/GardenPage';
import { HomePage } from './pages/HomePage';

function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 8_000,
        refetchOnWindowFocus: false,
        retry: (failureCount, error) => {
          if (error instanceof ApiClientError && error.status >= 400 && error.status < 500) {
            return false;
          }
          return failureCount < 1;
        },
      },
    },
  });
}

const queryClient = createAppQueryClient();

const isStandaloneArtifact =
  typeof window !== 'undefined' &&
  Boolean(
    (window as unknown as { __EG_STANDALONE__?: boolean }).__EG_STANDALONE__ ||
      window.location.protocol === 'file:' ||
      window.location.protocol === 'blob:' ||
      window.location.origin === 'null'
  );

export const App: React.FC = () => {
  const RouterComponent = isStandaloneArtifact ? MemoryRouter : BrowserRouter;

  return (
    <QueryClientProvider client={queryClient}>
      <RouterComponent>
        <AppShell>
          <Routes>
            <Route path="/" element={<HomePage />} />
            <Route path="/characters" element={<CharacterListPage />} />
            <Route path="/characters/new" element={<CharacterFormPage />} />
            <Route path="/characters/:id/edit" element={<CharacterFormPage />} />
            <Route path="/conversations/:id" element={<ConversationPage />} />
            <Route path="/garden" element={<GardenPage />} />
            <Route path="/settings/ai" element={<AISettingsPage />} />
            <Route path="/settings/comparisons" element={<ComparisonsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </AppShell>
      </RouterComponent>
    </QueryClientProvider>
  );
};
