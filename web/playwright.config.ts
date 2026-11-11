import { defineConfig } from '@playwright/test'

// Runs against a server started with KOWHAI_DEV_OIDC=1 (see `make e2e`).
// Set KOWHAI_E2E_URL to point somewhere else.
export default defineConfig({
  testDir: 'e2e',
  timeout: 30_000,
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: process.env.KOWHAI_E2E_URL ?? 'http://localhost:18080',
    channel: process.env.KOWHAI_E2E_CHANNEL ?? undefined,
    trace: 'retain-on-failure',
  },
})
