import { expect, test } from 'playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// M2 e2e (plan step 7): against the native binary with a temp FA_DATA_DIR.
// Setup goes through the real API (create DB, import fixture CSV, hotkeys);
// the categorization flow itself is driven through the UI with the keyboard.

const BASE = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';
const CSV_PATH = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	'../../testdata/fixtures/csv/normal.csv'
);

interface LineJSON {
	id: number;
	source: string;
	subcategoryId: number | null;
	subcategoryName: string | null;
	spreadMonths: number;
}

async function api(path: string, init?: RequestInit) {
	const r = await fetch(`${BASE}/api${path}`, init);
	if (!r.ok) throw new Error(`API ${path}: ${r.status} ${await r.text()}`);
	return r;
}

async function setupData(): Promise<{ lineIds: number[]; groceriesId: number }> {
	// Create the database with one account and one subcategory
	await api('/database', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			accounts: [{ name: 'Main account', account_number: '9999 0000000001' }],
			subcategories: [{ category_key: 'variable', name: 'Groceries', sort_order: 0 }]
		})
	});

	// Add a second subcategory and assign hotkeys (CAT-10)
	const cats = (await (await api('/categories')).json()) as {
		id: number;
		key: string;
		subcategories: { id: number; name: string }[];
	}[];
	const variable = cats.find((c) => c.key === 'variable')!;
	const groceriesId = variable.subcategories.find((s) => s.name === 'Groceries')!.id;
	await api('/subcategories', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ category_id: variable.id, name: 'Dining', hotkey: 'd', sort_order: 1 })
	});
	await api(`/subcategories/${groceriesId}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ hotkey: 'g' })
	});

	// Import the fixture CSV (preview + commit)
	const csv = readFileSync(CSV_PATH);
	const previewBody = new FormData();
	previewBody.append('accountId', '1');
	previewBody.append('file', new Blob([csv], { type: 'text/csv' }), 'normal.csv');
	const preview = (await (
		await api('/imports/preview', { method: 'POST', body: previewBody })
	).json()) as {
		previewToken: string;
	};
	const commitBody = new FormData();
	commitBody.append('accountId', '1');
	commitBody.append('previewToken', preview.previewToken);
	commitBody.append('file', new Blob([csv], { type: 'text/csv' }), 'normal.csv');
	await api('/imports', { method: 'POST', body: commitBody });

	const lines = (await (await api('/lines?pageSize=100')).json()) as { lines: LineJSON[] };
	return { lineIds: lines.lines.map((l) => l.id), groceriesId };
}

async function listLines(query: string): Promise<LineJSON[]> {
	const r = await api(`/lines?${query}`);
	return ((await r.json()) as { lines: LineJSON[] }).lines;
}

test('M2 categorize flow: hotkey, picker, undo/redo, spread, bulk, deep link', async ({ page }) => {
	const { lineIds, groceriesId } = await setupData();
	expect(lineIds).toHaveLength(3);

	await page.goto('/categorize');
	const rows = page.locator('tbody tr');
	await expect(rows).toHaveCount(3);
	// newest first: first row is the focused one (2024-12-31 Løn)
	await expect(rows.first()).toHaveClass(/bg-purple-50/);
	await expect(rows.first()).toContainText('Løn');

	// 1. Hotkey assign: 'g' assigns Groceries, focus advances, row stays dimmed (CATZ-4)
	await page.keyboard.press('g');
	await expect(rows.first()).toHaveClass(/opacity-50/);
	await expect(rows.nth(1)).toHaveClass(/bg-purple-50/);
	let lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('manual');
	expect(lines.find((l) => l.id === lineIds[0])?.subcategoryId).toBe(groceriesId);

	// 2. Ctrl+Z reverts, focus returns to the affected line (CATZ-6)
	await page.keyboard.press('Control+z');
	await expect(rows.first()).toHaveClass(/bg-purple-50/);
	await expect(rows.first()).not.toHaveClass(/opacity-50/);
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('none');

	// 3. Ctrl+Shift+Z redoes
	await page.keyboard.press('Control+Shift+z');
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('manual');
	// default filter none,conflict: the categorized line dropped out of the list
	await expect(rows).toHaveCount(2);

	// 4. Type-ahead picker: Space, type "din", Enter assigns Dining (CATZ-5)
	await page.keyboard.press(' ');
	await page.getByPlaceholder(/Type to search/i).fill('dining');
	await page.keyboard.press('Enter');
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[1])?.subcategoryName).toBe('Dining');
	// CATZ-4: the assigned line stays visible (dimmed) until the next refresh
	await expect(rows).toHaveCount(2);
	await expect(rows.nth(0)).toHaveClass(/opacity-50/);

	// 5. Spread dialog: set spread to 12 on the remaining focused line (VIZ-7)
	await page.keyboard.press('s');
	await page.getByRole('spinbutton', { name: /Months/ }).fill('12');
	await page.getByRole('button', { name: 'Save' }).click();
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[2])?.spreadMonths).toBe(12);

	// 6. Multi-select with Shift+Down + bulk assign, then undo (CATZ-11)
	//    (reload to get all 3 lines back under the default filter)
	await page.goto('/categorize?source=none,manual');
	await expect(rows).toHaveCount(3);
	await page.keyboard.press('Shift+ArrowDown');
	await page.keyboard.press('Shift+ArrowDown');
	await expect(page.locator('text=/3 selected/i')).toBeVisible();
	await page.keyboard.press('g');
	await expect(rows.nth(0)).toHaveClass(/opacity-50/);
	await expect(rows.nth(1)).toHaveClass(/opacity-50/);
	await expect(rows.nth(2)).toHaveClass(/opacity-50/);
	lines = await listLines('source=manual&pageSize=100');
	expect(lines).toHaveLength(3);

	// one undo reverts the whole bulk action: back to the pre-bulk state
	// (lines 1 and 2 were already manual before the bulk, line 3 was not)
	await page.keyboard.press('Control+z');
	lines = await listLines('source=manual&pageSize=100');
	expect(lines).toHaveLength(2);
	lines = await listLines('source=none&pageSize=100');
	expect(lines).toHaveLength(1);
	expect(lines[0].id).toBe(lineIds[2]);
	expect(lines[0].spreadMonths).toBe(12);

	// 7. Deep link from the statements view (STMT-6)
	await page.goto('/statements');
	const firstLink = page.locator('[data-categorize-line]').first();
	const focusId = await firstLink.getAttribute('data-categorize-line');
	await firstLink.click();
	await expect(page).toHaveURL(new RegExp(`/categorize\\?.*focus=${focusId}`));
	await expect(page.locator(`tr[data-line-id="${focusId}"]`)).toHaveClass(/bg-purple-50/);

	// 8. Help overlay (CATZ-8)
	await page.keyboard.press('?');
	await expect(page.getByText(/Keyboard shortcuts/i)).toBeVisible();
	await page.keyboard.press('Escape');
	await expect(page.getByText(/Keyboard shortcuts/i)).toBeHidden();
});
