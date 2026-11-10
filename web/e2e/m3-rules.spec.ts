import { expect, test } from 'playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// M3 e2e (plan step 9): against the native binary with a temp FA_DATA_DIR.
// Flow: import fixture -> create rule with live preview -> lines categorized ->
// edit with diff -> conflict -> resolve -> undo -> create rule from line (r)
// -> import triggers rule evaluation -> delete + undo.

const BASE = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';
const CSV_PATH = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	'../../testdata/fixtures/csv/normal.csv'
);
const CSV2_PATH = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	'../../testdata/fixtures/csv/duplicates.csv'
);

interface LineJSON {
	id: number;
	source: string;
	subcategoryId: number | null;
	ruleId: number | null;
	message: string;
}

async function api(path: string, init?: RequestInit) {
	const r = await fetch(`${BASE}/api${path}`, init);
	if (!r.ok) throw new Error(`API ${path}: ${r.status} ${await r.text()}`);
	return r;
}

async function setupData(): Promise<{ lineIds: number[]; groceriesId: number; accountId: number }> {
	// Create the DB when missing (409 when another spec already made it), then a
	// fresh account so this spec's import gets its own lines (dedup is per account).
	const createRes = await fetch(`${BASE}/api/database`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({
			accounts: [{ name: 'Main account', account_number: '9999 0000000001' }],
			subcategories: [{ category_key: 'variable', name: 'Groceries', sort_order: 0 }]
		})
	});
	if (createRes.status !== 201 && createRes.status !== 409) {
		throw new Error(`create database: ${createRes.status} ${await createRes.text()}`);
	}
	const acc = (await (
		await api('/accounts', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ name: `M3 account ${Date.now()}`, account_number: '9999 0000000001' })
		})
	).json()) as { id: number };
	const accountId = acc.id;

	const cats = (await (await api('/categories')).json()) as {
		id: number;
		key: string;
		subcategories: { id: number; name: string }[];
	}[];
	const variable = cats.find((c) => c.key === 'variable')!;
	let groceriesId = variable.subcategories.find((s) => s.name === 'Groceries')?.id;
	if (groceriesId === undefined) {
		const sub = (await (
			await api('/subcategories', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ category_id: variable.id, name: 'Groceries', sort_order: 0 })
			})
		).json()) as { id: number };
		groceriesId = sub.id;
	}
	const csv = readFileSync(CSV_PATH);
	const previewBody = new FormData();
	previewBody.append('accountId', String(accountId));
	previewBody.append('file', new Blob([csv], { type: 'text/csv' }), 'normal.csv');
	const preview = (await (
		await api('/imports/preview', { method: 'POST', body: previewBody })
	).json()) as { previewToken: string };
	const commitBody = new FormData();
	commitBody.append('accountId', String(accountId));
	commitBody.append('previewToken', preview.previewToken);
	commitBody.append('file', new Blob([csv], { type: 'text/csv' }), 'normal.csv');
	await api('/imports', { method: 'POST', body: commitBody });

	// only this spec's account's lines (other specs may share the data dir)
	const lines = (await (await api(`/lines?pageSize=100&accounts=${accountId}`)).json()) as {
		lines: LineJSON[];
	};
	return { lineIds: lines.lines.map((l) => l.id), groceriesId, accountId };
}

let currentAccountId = 0;
async function listLines(query: string): Promise<LineJSON[]> {
	const sep = query.includes('?') ? '&' : '?';
	const r = await api(`/lines?${query}${sep}accounts=${currentAccountId}`);
	return ((await r.json()) as { lines: LineJSON[] }).lines;
}

test('M3 rules flow: preview, save, diff, conflict, resolve, undo, from-line, import, delete', async ({
	page
}) => {
	const { lineIds, groceriesId, accountId } = await setupData();
	currentAccountId = accountId;
	expect(lineIds).toHaveLength(3);
	// newest first: [0]=Løn (in), [1]=Køb i Netto (out), [2]=Kontanthævning (out)
	const nettoId = lineIds[1];

	// 1. Create a rule with live preview (RULE-1/3)
	await page.goto('/rules/new');
	await page.getByLabel(/Name/i).fill('Netto');
	const patternInput = page.locator('input[spellcheck="false"]');
	await patternInput.fill('Netto');
	// live preview appears (debounced) with willCategorize = 1
	await expect(page.locator('[data-preview-group="willCategorize"]')).toHaveText('1');
	await page.getByLabel(/Target subcategory/i).selectOption(String(groceriesId));
	await page.locator('[data-save-rule]').click();
	await expect(page).toHaveURL(/\/rules$/);
	// the list shows the rule with matchCount 1
	await expect(page.locator('[data-rule-id]')).toHaveCount(1);
	await expect(page.locator('[data-match-count]')).toHaveText('1');
	let lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('rule');
	expect(lines.find((l) => l.id === nettoId)?.ruleId).not.toBeNull();

	// 2. Edit with diff (RULE-4/6): narrow the pattern so the line is removed
	const ruleId = Number(await page.locator('[data-rule-id]').getAttribute('data-rule-id'));
	await page.goto(`/rules/${ruleId}/edit`);
	await page.locator('[data-diff-mode]').click();
	await patternInput.fill('^ZZZ-NOMATCH');
	await expect(page.locator('[data-diff-removed]')).toHaveText('1');
	await page.locator('[data-save-rule]').click();
	await expect(page).toHaveURL(/\/rules$/);
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('none');

	// undo the edit via the API stack, then redo state check via UI undo on categorize
	await api('/actions/undo', { method: 'POST' });
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('rule');

	// 3. Second overlapping rule -> conflict (RULE-7)
	await page.goto('/rules/new');
	await page.getByLabel(/Name/i).fill('K-rule');
	await patternInput.fill('Køb');
	await page.getByLabel(/Target subcategory/i).selectOption(String(groceriesId));
	await page.locator('[data-save-rule]').click();
	await expect(page).toHaveURL(/\/rules$/);
	lines = await listLines('pageSize=100');
	// "Køb i Netto" matches both `Netto` and `Køb` -> conflict
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('conflict');

	// conflicts tab lists it with both rules
	await page.locator('[data-tab-conflicts]').click();
	await expect(page.locator('[data-conflict-line]')).toHaveCount(1);
	const ruleButtons = page.locator('[data-resolve-rule]');
	await expect(ruleButtons).toHaveCount(2);
	// resolve with the first rule button
	await ruleButtons.first().click();
	await expect(page.locator('[data-conflict-line]')).toHaveCount(0);
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('rule');

	// 4. Ctrl+Z on the categorize page undoes the resolution
	await page.goto(`/categorize?focus=${nettoId}&source=conflict,rule,none`);
	await expect(page.locator('tbody tr').first()).toBeVisible(); // wait for hydration
	await page.keyboard.press('Control+z');
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('conflict');

	// 5. "Create rule from this line" (r hotkey, RULE-1/D9): focus Kontanthævning
	const kontId = lineIds[2];
	await page.goto(`/categorize?focus=${kontId}&source=none,conflict`);
	await expect(page.locator('tbody tr').first()).toBeVisible(); // wait for hydration
	await page.keyboard.press('r');
	await expect(page).toHaveURL(/\/rules\/new\?fromLine=/);
	// prefilled: escaped message pattern + account + direction out
	await expect(patternInput).toHaveValue('Kontanthævning');
	await page.getByLabel(/Name/i).fill('Cash withdrawal');
	await page.getByLabel(/Target subcategory/i).selectOption(String(groceriesId));
	await page.locator('[data-save-rule]').click();
	await expect(page).toHaveURL(/\/rules$/);
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === kontId)?.source).toBe('rule');

	// 6. Import another file -> rule evaluation runs, result screen shows counts (IMP-11/12)
	await page.goto('/import');
	const csv2 = readFileSync(CSV2_PATH);
	const fileInput = page.locator('input[type="file"]');
	await fileInput.setInputFiles({ name: 'duplicates.csv', mimeType: 'text/csv', buffer: csv2 });
	await page.getByRole('button', { name: /Preview/i }).click();
	await page.getByRole('button', { name: /^Import$/ }).click();
	// result screen shows the auto-categorized count (no rule matches "Same"/"Other" -> 0)
	await expect(page.locator('[data-rule-categorized]')).toHaveText('0');
	await expect(page.locator('[data-rule-conflicts]')).toBeVisible();

	// 7. Delete a rule -> its lines re-evaluated; undo restores (RULE-6, UNDO-1)
	await page.goto('/rules');
	const rows = page.locator('[data-rule-id]');
	const countBefore = await rows.count();
	expect(countBefore).toBe(3);
	// delete the K-rule (row containing pattern `K` but not Netto)... find by edit link href
	const kRow = page.locator('[data-rule-id]', { hasText: 'K-rule' });
	await kRow.locator('[data-delete-rule]').click();
	await kRow.locator('[data-confirm-delete]').click();
	await expect(page.locator('[data-rule-id]')).toHaveCount(2);
	lines = await listLines('pageSize=100');
	// Netto line loses the conflict -> back to rule via the Netto rule
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('rule');
	// undo the delete via categorize page Ctrl+Z
	await page.goto('/categorize');
	await expect(page.locator('tbody tr').first()).toBeVisible(); // wait for hydration
	await page.keyboard.press('Control+z');
	const rulesAfter = (await (await api('/rules')).json()) as unknown[];
	expect(rulesAfter).toHaveLength(3);
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === nettoId)?.source).toBe('conflict');
});
