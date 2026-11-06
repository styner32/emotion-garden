import { defineConfig, devices } from '@playwright/test';

const E2E_PORT = process.env.E2E_PORT || '18080';
const BASE_URL = process.env.E2E_BASE_URL || `http://127.0.0.1:${E2E_PORT}`;
const E2E_DB_URL =
  process.env.E2E_DATABASE_URL ||
  'postgres://app:change_me@127.0.0.1:5432/emotion_garden_e2e?sslmode=disable';

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: {
    timeout: 12_000,
  },
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    launchOptions: {
      executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH || '/usr/bin/chromium',
      args: ['--no-sandbox', '--disable-dev-shm-usage'],
    },
  },
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : {
        command: `bash -c 'cd .. && ./scripts/dev-db.sh start && psql -h 127.0.0.1 -p 5432 -U postgres -d postgres -c "DROP DATABASE IF EXISTS emotion_garden_e2e WITH (FORCE);" -c "CREATE DATABASE emotion_garden_e2e OWNER app;" && DATABASE_URL="${E2E_DB_URL}" ./bin/migrate up && DATABASE_URL="${E2E_DB_URL}" HTTP_ADDR=127.0.0.1:${E2E_PORT} PUBLIC_ORIGIN=${BASE_URL} EXTRA_ALLOWED_ORIGINS=http://localhost:${E2E_PORT} exec ./bin/server'`,
        url: `${BASE_URL}/readyz`,
        reuseExistingServer: false,
        timeout: 30_000,
      },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});
