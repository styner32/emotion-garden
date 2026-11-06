import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from './App';
import { installStandaloneBackend } from './standalone/inMemoryBackend';
import './styles/global.css';

if (
  typeof window !== 'undefined' &&
  Boolean((window as unknown as { __EG_STANDALONE__?: boolean }).__EG_STANDALONE__)
) {
  installStandaloneBackend();
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
