import { defineConfig } from 'playwright/test';

// E2E tests run against the native binary started by `task e2e`
// (FA_DATA_DIR=testdata/e2e/finance bin/financeanalysis serve, default
// 127.0.0.1:8080).
export default defineConfig({
	testDir: './e2e',
	timeout: 30_000,
	retries: 0,
	workers: 1,
	reporter: [['list']],
	use: {
		baseURL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080'
	},
	projects: [{ name: 'chromium', use: { browserName: 'chromium' } }]
});
