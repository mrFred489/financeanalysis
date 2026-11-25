import { expect, test } from 'playwright/test';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// M6 (docs/plans/categorize-inline-rules.md): against the native binary with a
// temp FA_DATA_DIR. Flow: controls + inline rule builder on top of /categorize,
// prefill follows focus while pristine, edits survive focus changes, 'r'
// re-prefills, Ctrl+Enter creates the rule in place, Ctrl+Z undoes it, filters
// collapse unless active.

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
	message: string;
}

async function api(path: string, init?: RequestInit) {
	const r = await fetch(`${BASE}/api${path}`, init);
	if (!r.ok) throw new Error(`API ${path}: ${r.status} ${await r.text()}`);
	return r;
}

async function setupData(): Promise<{
	lineIds: number[];
	groceriesId: number;
	accountId: number;
	paletteCols: number;
	fixedName: string;
}> {
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
			body: JSON.stringify({ name: `M6 account ${Date.now()}`, account_number: '9999 0000000001' })
		})
	).json()) as { id: number };
	const accountId = acc.id;

	const cats = (await (await api('/categories')).json()) as {
		id: number;
		key: string;
		name: string;
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
	// a second subcategory in the same category, so chips must flow horizontally
	if (!variable.subcategories.some((s) => s.name === 'Dining')) {
		await api('/subcategories', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ category_id: variable.id, name: 'Dining', sort_order: 1 })
		});
	}
	// a second category with a subcategory, so the palette has two columns
	const fixed = cats.find((c) => c.key === 'fixed')!;
	if (!fixed.subcategories.some((s) => s.name === 'Rent')) {
		await api('/subcategories', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ category_id: fixed.id, name: 'Rent', sort_order: 0 })
		});
	}
	const catsAfter = (await (await api('/categories')).json()) as typeof cats;
	const paletteCols = catsAfter.filter((c) => c.subcategories.length > 0).length;

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

	const lines = (await (await api(`/lines?pageSize=100&accounts=${accountId}`)).json()) as {
		lines: LineJSON[];
	};
	return {
		lineIds: lines.lines.map((l) => l.id),
		groceriesId,
		accountId,
		paletteCols,
		fixedName: fixed.name
	};
}

let currentAccountId = 0;
async function listLines(query: string): Promise<LineJSON[]> {
	const sep = query.includes('?') ? '&' : '?';
	const r = await api(`/lines?${query}${sep}accounts=${currentAccountId}`);
	return ((await r.json()) as { lines: LineJSON[] }).lines;
}

test('M6 categorize page: controls on top, inline rule builder, Ctrl+Enter creates rule', async ({
	page
}) => {
	const { lineIds, groceriesId, accountId, paletteCols, fixedName } = await setupData();
	currentAccountId = accountId;
	expect(lineIds).toHaveLength(3);
	// newest first: [0]=Løn december (in), [1]=Køb i Netto (out), [2]=Kontanthævning (out)

	await page.goto('/categorize');

	// 1. Layout: controls, then rule builder, then the line list. Filters collapsed.
	const yOf = async (sel: string) => (await page.locator(sel).first().boundingBox())!.y;
	expect(await yOf('[data-controls]')).toBeLessThan(await yOf('[data-rule-builder]'));
	expect(await yOf('[data-rule-builder]')).toBeLessThan(await yOf('tbody'));
	const builder = page.locator('[data-rule-builder]');
	await expect(builder).toBeVisible();
	await expect(page.locator('[data-filters-summary]')).toHaveText('Filters');
	await expect(page.getByLabel(/Period/i)).toBeHidden();

	// 1b. Palette: one column per category (with its headline); columns sit side
	//     by side and automatically fill the available horizontal space
	const palette = page.locator('[data-palette-grid]');
	await expect(palette).toBeVisible();
	expect(await palette.evaluate((el) => getComputedStyle(el).display)).toBe('grid');
	const trackCount = await palette.evaluate(
		(el) => getComputedStyle(el).gridTemplateColumns.split(' ').filter(Boolean).length
	);
	expect(trackCount).toBe(paletteCols);
	await expect(palette.getByText(fixedName, { exact: true })).toBeVisible(); // category headline
	const colBoxes = (await palette.locator(':scope > div').evaluateAll((els) =>
		els.map((e) => {
			const r = e.getBoundingClientRect();
			return { x: r.x, y: r.y, width: r.width, height: r.height };
		})
	)) as { x: number; y: number; width: number; height: number }[];
	expect(colBoxes).toHaveLength(paletteCols);
	const paletteBox = await palette.boundingBox();
	const controlsBox = await page.locator('[data-controls]').boundingBox();
	expect(paletteBox!.width).toBeGreaterThan(controlsBox!.width - 40); // fills width minus padding
	if (colBoxes.length >= 2) {
		expect(Math.abs(colBoxes[0].y - colBoxes[1].y)).toBeLessThan(2); // side by side
		expect(colBoxes[1].x).toBeGreaterThan(colBoxes[0].x);
	}
	// chips inside a column flow horizontally and fill its width (wrap only if needed)
	const varCol = palette.locator(':scope > div').filter({ hasText: 'Groceries' });
	const chipBoxes = (await varCol.locator('[data-subcategory-id]').evaluateAll((els) =>
		els.map((e) => {
			const r = e.getBoundingClientRect();
			return { x: r.x, y: r.y, width: r.width };
		})
	)) as { x: number; y: number; width: number }[];
	expect(chipBoxes.length).toBeGreaterThanOrEqual(2); // Groceries + Dining
	expect(Math.abs(chipBoxes[0].y - chipBoxes[1].y)).toBeLessThan(2); // same row: fills width first
	expect(chipBoxes[1].x).toBeGreaterThan(chipBoxes[0].x);

	// Scope the list to this spec's account (other specs share the data dir)
	await page.goto(`/categorize?accounts=${accountId}&source=none,conflict`);
	const rows = page.locator('tbody tr');
	await expect(rows).toHaveCount(3);

	// 2. Rule builder prefilled from the focused line (Løn december, incoming)
	const patternInput = builder.locator('input[spellcheck="false"]');
	await expect(patternInput).toHaveValue('Løn december');
	await expect(builder.getByLabel(/^Name/i)).toHaveValue('Løn december');
	await expect(builder.getByLabel(/Direction/i)).toHaveValue('in');
	// the focused line is summarized next to the builder title
	await expect(page.locator('[data-rule-builder-line]')).toContainText('Løn december');
	// subcategory select groups options by category (optgroup has no box -> attached, not visible)
	await expect(builder.locator('select optgroup').first()).toBeAttached();
	await expect(builder.locator('select optgroup')).not.toHaveCount(0);
	// no target subcategory preselected: save stays disabled until one is chosen
	const subSelect = builder.getByLabel(/Target subcategory/i);
	await expect(subSelect.locator('option').first()).toHaveText(/Select subcategory/i);
	await expect(builder.locator('[data-save-rule]')).toBeDisabled();

	// 3. Pristine form follows focus: ArrowDown -> Køb i Netto (outgoing)
	await page.keyboard.press('ArrowDown');
	await expect(patternInput).toHaveValue('Køb i Netto');
	await expect(builder.getByLabel(/Direction/i)).toHaveValue('out');

	// 4. Editing the pattern makes the form dirty: moving line focus no longer
	//    clobbers it (click a row to move line focus; keys go to the input otherwise)
	await patternInput.fill('NETTO');
	await rows.nth(2).click(); // focus Kontanthævning
	await expect(patternInput).toHaveValue('NETTO');
	await page.keyboard.press('ArrowUp'); // focus Køb i Netto
	await expect(patternInput).toHaveValue('NETTO');

	// 5. 'r' forces a re-prefill from the focused line (back to Køb i Netto)
	await page.keyboard.press('r');
	await expect(patternInput).toHaveValue('Køb i Netto');

	// Focus the first line (Løn december) and re-prefill. No rule from the other
	// specs matches it, so the preview shows it as willCategorize.
	await rows.first().click();
	await page.keyboard.press('r');
	await expect(patternInput).toHaveValue('Løn december');

	// 6. Live server-side preview shows the match before saving, in the right
	//    column next to the form fields (laptop viewport = lg breakpoint)
	await expect(builder.locator('[data-preview-panel]')).toBeVisible();
	await expect(builder.locator('[data-preview-group="willCategorize"]')).toHaveText('1');
	const patBox = await patternInput.boundingBox();
	const prevBox = await builder.locator('[data-preview-panel]').boundingBox();
	expect(prevBox!.x).toBeGreaterThan(patBox!.x);

	// 7. Pick the target subcategory (required), Ctrl+Enter creates the rule
	await builder.getByLabel(/Target subcategory/i).selectOption(String(groceriesId));
	await expect(builder.locator('[data-save-rule]')).toBeEnabled();
	await page.keyboard.press('Control+Enter');
	await expect(page).toHaveURL(/\/categorize/);
	await expect(page.locator('[role="status"]')).toContainText('Løn december');
	let lines = await listLines('pageSize=100');
	const lonLine = lines.find((l) => l.id === lineIds[0]);
	expect(lonLine?.source).toBe('rule');
	expect(lonLine?.subcategoryId).toBe(groceriesId);
	// the categorized line dropped out of the default none,conflict view
	await expect(rows).toHaveCount(2);

	// 8. Ctrl+Z undoes the rule creation (one undoable action, M3 UNDO-1).
	//    Move focus out of the form first: keys in fields don't reach the page handler.
	await page.getByRole('heading', { name: 'Categorize', level: 1 }).click();
	await page.keyboard.press('Control+z');
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('none');

	// 9. Ctrl+Enter with focus inside the pattern input also creates the rule
	//    (a target subcategory is required first)
	await builder.getByLabel(/Target subcategory/i).selectOption(String(groceriesId));
	await patternInput.click();
	await page.keyboard.press('Control+Enter');
	await expect(page.locator('[role="status"]')).toBeVisible();
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('rule');
	await page.getByRole('heading', { name: 'Categorize', level: 1 }).click();
	await page.keyboard.press('Control+z');
	lines = await listLines('pageSize=100');
	expect(lines.find((l) => l.id === lineIds[0])?.source).toBe('none');

	// 10. Filters auto-expand when non-default filters are active
	await page.goto('/categorize?source=rule');
	await expect(page.locator('[data-filters-summary]')).toContainText('1 active');
	await expect(page.getByLabel(/Period/i)).toBeVisible();
});
