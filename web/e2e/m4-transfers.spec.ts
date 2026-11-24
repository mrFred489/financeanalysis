import { expect, test } from 'playwright/test';

// M4 e2e (plan docs/plans/M4-transfers.md step 10): against the native binary
// with a temp FA_DATA_DIR. Flow: import the two account CSVs through the UI ->
// result screen shows the transfer counts -> /transfers lists the auto-linked
// pair and the ambiguous review groups -> statements shows the transfer column
// with a jump link -> categorize page has "Jump to counterpart" -> unlink from
// the categorize page -> both lines back to none -> Ctrl+Z undoes the unlink ->
// link restored -> pick one review candidate -> manual link -> dismiss the
// other group -> detection does not re-suggest it.
//
// The CSVs are generated with run-unique amounts, because detection scans ALL
// data and a reused e2e data dir would otherwise pair this run's lines with a
// previous run's leftovers.

const BASE = process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080';

async function api(path: string, init?: RequestInit) {
	const r = await fetch(`${BASE}/api${path}`, init);
	if (!r.ok) throw new Error(`API ${path}: ${r.status} ${await r.text()}`);
	return r;
}

interface AccountJSON {
	id: number;
	name: string;
}
interface LineJSON {
	id: number;
	accountId: number;
	source: string;
	message: string;
	amountMinor: number;
	transferLinkId: number | null;
	transferCounterpartId: number | null;
}
interface LinkJSON {
	id: number;
	outLineId: number;
	inLineId: number;
	method: string;
	out: { id: number; accountId: number };
	in: { id: number; accountId: number };
}

// fmtMinor formats øre as the Danish CSV amount, e.g. -432100 -> "-4.321,00".
function fmtMinor(minor: number): string {
	const sign = minor < 0 ? '-' : '';
	const abs = Math.abs(minor);
	const whole = Math.floor(abs / 100);
	const dec = String(abs % 100).padStart(2, '0');
	const wholeDa = whole.toString().replace(/\B(?=(\d{3})+(?!\d))/g, '.');
	return `${sign}${wholeDa},${dec}`;
}

// row builds one v2 CSV row: message, from, to, amountMinor, dd-mm-yyyy, bankRef.
function row(
	message: string,
	from: string,
	to: string,
	amountMinor: number,
	date: string,
	ref: string
): string {
	const [y, m, d] = date.split('-');
	return `;${message};${from};${to};${fmtMinor(amountMinor)};;;${d}-${m}-${y};${d}-${m}-${y};${d}-${m}-${y};${ref};;;;;`;
}

interface CsvSet {
	main: { name: string; buffer: Buffer };
	savings: { name: string; buffer: Buffer };
	amounts: { pair: number; amb1: number; amb2: number };
}

function makeCsvs(stamp: number): CsvSet {
	// run-unique amounts keep this run's transfers from matching leftovers
	const pair = 400000 + (stamp % 97) * 1000;
	const amb1 = 100000 + (stamp % 89) * 1000 + 7;
	const amb2 = 200000 + (stamp % 83) * 1000 + 13;
	const mainAcc = '9999 0000000001';
	const savAcc = '9999 0000000002';
	const main = [
		row('Transfer to savings', mainAcc, savAcc, -pair, '2026-01-15', '2000000001'),
		row('Coffee shop', mainAcc, '', -4500, '2026-01-16', '2000000002'),
		row('Transfer ambiguous', mainAcc, savAcc, -amb1, '2026-01-20', '2000000003'),
		row('Transfer ambiguous 2', mainAcc, savAcc, -amb2, '2026-01-25', '2000000004')
	].join('\n');
	const savings = [
		row('Transfer from main', mainAcc, savAcc, pair, '2026-01-15', '3000000001'),
		row('Transfer ambiguous A', mainAcc, savAcc, amb1, '2026-01-20', '3000000002'),
		row('Transfer ambiguous B', mainAcc, savAcc, amb1, '2026-01-20', '3000000003'),
		row('Transfer ambiguous 2 A', mainAcc, savAcc, amb2, '2026-01-25', '3000000004'),
		row('Transfer ambiguous 2 B', mainAcc, savAcc, amb2, '2026-01-25', '3000000005')
	].join('\n');
	return {
		main: { name: 'transfer_main.csv', buffer: Buffer.from(main) },
		savings: { name: 'transfer_savings.csv', buffer: Buffer.from(savings) },
		amounts: { pair, amb1, amb2 }
	};
}

async function setupAccounts(stamp: number): Promise<{ main: AccountJSON; savings: AccountJSON }> {
	// Create the DB when missing (409 when another spec already made it).
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
	const mk = async (name: string, number: string): Promise<AccountJSON> =>
		(await (
			await api('/accounts', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name, account_number: number })
			})
		).json()) as AccountJSON;
	return {
		main: await mk(`M4 main ${stamp}`, '9999 0000000001'),
		savings: await mk(`M4 savings ${stamp}`, '9999 0000000002')
	};
}

// importViaUI drives the real import page: pick account, choose file, preview, commit.
async function importViaUI(
	page: import('playwright/test').Page,
	accountName: string,
	file: { name: string; buffer: Buffer }
) {
	await page.goto('/import');
	await page.locator('select').first().selectOption({ label: accountName });
	await page
		.locator('input[type="file"]')
		.setInputFiles({ name: file.name, mimeType: 'text/csv', buffer: file.buffer });
	await page.getByRole('button', { name: 'Preview', exact: true }).click();
	await page.getByRole('button', { name: 'Import', exact: true }).click();
	await expect(page.locator('[data-transfer-matched]')).toBeVisible();
}

async function myLinks(accountIds: number[]): Promise<LinkJSON[]> {
	const links = (await (await api('/transfers')).json()) as LinkJSON[];
	return links.filter(
		(l) => accountIds.includes(l.out.accountId) && accountIds.includes(l.in.accountId)
	);
}

async function myReview(
	accountId: number
): Promise<{ outLine: LineJSON; candidates: LineJSON[] }[]> {
	const groups = (await (await api('/transfers/review')).json()) as {
		outLine: LineJSON;
		candidates: LineJSON[];
	}[];
	return groups.filter((g) => g.outLine.accountId === accountId);
}

async function myLine(accountId: number, message: string): Promise<LineJSON> {
	const res = (await (await api(`/lines?pageSize=100&accounts=${accountId}`)).json()) as {
		lines: LineJSON[];
	};
	const line = res.lines.find((l) => l.message === message);
	if (!line) throw new Error(`line ${message} not found on account ${accountId}`);
	return line;
}

test('M4 transfers flow: import detects, review, link, dismiss, unlink, undo', async ({ page }) => {
	const stamp = Date.now();
	const { main, savings } = await setupAccounts(stamp);
	const csvs = makeCsvs(stamp);
	const accountIds = [main.id, savings.id];

	// 1. Import the main account file: no counterpart exists yet for this run.
	await importViaUI(page, main.name, csvs.main);
	await expect(page.locator('[data-transfer-matched]')).toHaveText(/^\d+$/);
	expect((await myLinks(accountIds)).length).toBe(0);

	// 2. Import the savings account file: the unique pair is auto-linked
	// (IMP-11/12) and the two ambiguous groups are reported on the result screen.
	await page.getByRole('button', { name: 'Import another file' }).click();
	await importViaUI(page, savings.name, csvs.savings);
	await expect(page.locator('[data-transfer-matched]')).toHaveText(/^[1-9]\d*$/);
	await expect(page.locator('[data-transfer-ambiguous]')).toHaveText(/^[1-9]\d*$/);
	const autoLinks = await myLinks(accountIds);
	expect(autoLinks.length).toBe(1);
	expect(autoLinks[0].method).toBe('auto');
	expect(Math.abs(autoLinks[0].outLineId) > 0).toBe(true);

	// 3. Result screen links to the transfer review page; the auto link and both
	// review groups are listed there.
	await page.locator('[data-view-transfers]').click();
	await expect(page).toHaveURL(/\/transfers$/);
	await expect(page.locator(`[data-transfer-link="${autoLinks[0].id}"]`)).toBeVisible();
	const myGroupIds = (await myReview(main.id)).map((g) => g.outLine.id);
	expect(myGroupIds.length).toBe(2);
	for (const id of myGroupIds) {
		await expect(page.locator(`[data-review-group="${id}"]`)).toBeVisible();
	}

	// 4. Statements view shows the transfer column with a jump link (TRF-9).
	await page.goto('/statements?source=transfer');
	const transferCell = page.locator('[data-transfer-link]').first();
	await expect(transferCell).toBeVisible();
	await transferCell.click();
	await expect(page).toHaveURL(/\/categorize\?.*focus=\d+/);

	// 5. Categorize page: "Jump to counterpart" navigates to the counterpart (CATZ-7).
	const jump = page.locator('[data-jump-counterpart]');
	await expect(jump).toBeVisible();
	const focusBefore = new URL(page.url()).searchParams.get('focus');
	await jump.click();
	const focusAfter = new URL(page.url()).searchParams.get('focus');
	expect(focusAfter).not.toBeNull();
	expect(focusAfter).not.toBe(focusBefore);

	// 6. Unlink from the categorize page (TRF-6): both lines go back to none.
	const outLine = await myLine(main.id, 'Transfer to savings');
	const inLine = await myLine(savings.id, 'Transfer from main');
	expect(Number(focusAfter)).toBe(inLine.id === Number(focusBefore) ? outLine.id : inLine.id);
	await Promise.all([
		page.waitForResponse(
			(r) => r.url().includes('/api/transfers/') && r.request().method() === 'POST'
		),
		page.locator('[data-unlink-transfer]').click()
	]);
	await expect(page.locator('[data-unlink-transfer]')).toHaveCount(0);
	for (const id of [outLine.id, inLine.id]) {
		const l = (await (await api(`/lines/${id}`)).json()) as LineJSON;
		expect(l.source).toBe('none');
		expect(l.transferLinkId).toBeNull();
	}
	expect((await myLinks(accountIds)).length).toBe(0);

	// 7. Ctrl+Z undoes the unlink: the link is restored (UNDO-1).
	await Promise.all([
		page.waitForResponse(
			(r) => r.url().includes('/api/actions/undo') && r.request().method() === 'POST'
		),
		page.keyboard.press('Control+z')
	]);
	const restored = await myLinks(accountIds);
	expect(restored.length).toBe(1);
	expect(restored[0].method).toBe('auto');
	const outAfter = (await (await api(`/lines/${outLine.id}`)).json()) as LineJSON;
	expect(outAfter.source).toBe('transfer');
	expect(outAfter.transferLinkId).toBe(restored[0].id);

	// 8. Review: pick one candidate of the first ambiguous group -> manual link (TRF-7).
	await page.goto('/transfers');
	const groups = await myReview(main.id);
	const g1 = groups.find((g) => g.outLine.message === 'Transfer ambiguous');
	if (!g1) throw new Error('first review group not found');
	await Promise.all([
		page.waitForResponse(
			(r) => r.url().includes('/api/transfers/link') && r.request().method() === 'POST'
		),
		page
			.locator(`[data-review-group="${g1.outLine.id}"]`)
			.locator('[data-candidate]')
			.first()
			.click()
	]);
	const linksAfterPick = await myLinks(accountIds);
	expect(linksAfterPick.length).toBe(2);
	expect(linksAfterPick.some((l) => l.method === 'manual')).toBe(true);

	// 9. Dismiss the remaining group (TRF-3); detection must not re-suggest it.
	const groupsLeft = await myReview(main.id);
	expect(groupsLeft.length).toBe(1); // only the second ambiguous group is left
	await Promise.all([
		page.waitForResponse(
			(r) => r.url().includes('/api/transfers/review/dismiss') && r.request().method() === 'POST'
		),
		page
			.locator(`[data-review-group="${groupsLeft[0].outLine.id}"]`)
			.locator('[data-dismiss]')
			.click()
	]);
	expect((await myReview(main.id)).length).toBe(0);
	await Promise.all([
		page.waitForResponse(
			(r) => r.url().includes('/api/transfers/detect') && r.request().method() === 'POST'
		),
		page.locator('[data-detect]').click()
	]);
	expect((await myReview(main.id)).length).toBe(0);
	expect((await myLinks(accountIds)).length).toBe(2);
});
