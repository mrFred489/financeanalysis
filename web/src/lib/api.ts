// API client for FinanceAnalysis

const API_BASE = '/api';

export interface APIErrorShape {
	code: string;
	message: string;
}

/**
 * Custom error class for API errors.
 */
export class APIError extends Error {
	constructor(
		public readonly code: string,
		message: string,
		public readonly status: number
	) {
		super(message);
		this.name = 'APIError';
	}
}

/**
 * Generic fetch wrapper that handles API errors consistently.
 */
export async function apiFetch<T>(path: string, options?: RequestInit): Promise<T> {
	const url = API_BASE + path;
	const response = await fetch(url, options);

	if (!response.ok) {
		try {
			const errorData = await response.json();
			if (errorData.error) {
				throw new APIError(
					errorData.error.code || 'unknown',
					errorData.error.message || response.statusText,
					response.status
				);
			}
		} catch (e) {
			if (e instanceof APIError) throw e;
		}
		throw new APIError('unknown', response.statusText, response.status);
	}

	return response.json() as Promise<T>;
}

// --- helpers for Go's sql.Null* JSON shape ({String|Int64, Valid}) ---

interface nullString {
	String: string;
	Valid: boolean;
}
interface nullInt {
	Int64: number;
	Valid: boolean;
}
const ns = (v: nullString | null): string | null => (v && v.Valid ? v.String : null);
const ni = (v: nullInt | null): number | null => (v && v.Valid ? v.Int64 : null);

// --- types ---

export interface StatusResponse {
	version: string;
	database: 'missing' | 'ready';
	schemaVersion?: number;
	accountsConfigured?: boolean;
}

export interface Account {
	id: number;
	name: string;
	account_number: string | null;
	sort_order: number;
	archived: number;
	created_at: string;
}

export interface Category {
	id: number;
	key: string;
	name: string;
	color: string;
}

export interface Subcategory {
	id: number;
	category_id: number;
	name: string;
	color: string;
	icon: string | null;
	hotkey: string | null;
	sort_order: number;
}

export interface CategoryWithSubcategories extends Category {
	subcategories: Subcategory[];
}

export interface ImportBatch {
	id: number;
	account_id: number;
	account_name: string;
	file_name: string;
	file_sha256: string;
	imported_at: string;
	line_count: number;
	duplicate_count: number;
	undone_at: nullString | null;
	date_from: string | null;
	date_to: string | null;
	actual_line_count: number;
}

export interface ParseError {
	row: number;
	reason: string;
}

export interface ImportWarning {
	type: string;
	message: string;
	row?: number;
}

export interface PreviewResult {
	fileName: string;
	fileSha256: string;
	previewToken: string;
	accountId: number;
	fileAccounts: string[];
	dateFrom?: string;
	dateTo?: string;
	newCount: number;
	duplicateCount: number;
	parseErrors: ParseError[];
	warnings: ImportWarning[];
}

export interface CommitResult {
	importId: number;
	fileName: string;
	linesImported: number;
	duplicatesSkipped: number;
	uncategorized: number;
	parseErrors: ParseError[];
}

export interface StatementLine {
	id: number;
	accountId: number;
	accountName: string;
	importId: number | null;
	bookingDate: string;
	transactionDate: string | null;
	valueDate: string | null;
	message: string;
	amountMinor: number;
	balanceMinor: number | null; // null when the bank format has no balance column
	currency: string;
	bankRef: string | null;
	counterpartyAccount: string | null;
	fileRow: number;
	seq: number;
	occurrence: number;
	subcategoryId: number | null;
	subcategoryName: string | null;
	subcategoryColor: string | null;
	categoryId: number | null;
	categoryName: string | null;
	source: string;
	ruleId: number | null;
	spreadMonths: number;
}

export interface LinesTotals {
	count: number;
	sumIn: number;
	sumOut: number;
	net: number;
}

export interface LinesResponse {
	lines: StatementLine[];
	total: number;
	page: number;
	pageSize: number;
	totals: LinesTotals;
}

export interface BackupInfo {
	name: string;
	size: number;
	modTime: string;
}

export interface CreateDatabasePayload {
	accounts?: { name: string; account_number?: string | null }[];
	subcategories?: {
		category_key: string;
		name: string;
		color?: string;
		hotkey?: string | null;
		sort_order?: number;
	}[];
}

// --- status / database ---

export async function getStatus(): Promise<StatusResponse> {
	return apiFetch<StatusResponse>('/status');
}

export async function createDatabase(payload?: CreateDatabasePayload): Promise<{ status: string }> {
	return apiFetch<{ status: string }>('/database', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: payload ? JSON.stringify(payload) : undefined
	});
}

export function uploadDatabase(file: File): Promise<{ status: string }> {
	const form = new FormData();
	form.append('file', file);
	return apiFetch<{ status: string }>('/database/upload', { method: 'POST', body: form });
}

export async function downloadDatabase(): Promise<Blob> {
	const response = await fetch(API_BASE + '/database/download');
	if (!response.ok) throw new APIError('download_failed', response.statusText, response.status);
	return response.blob();
}

export function replaceDatabase(file: File): Promise<{ status: string }> {
	const form = new FormData();
	form.append('file', file);
	return apiFetch<{ status: string }>('/database/replace', { method: 'POST', body: form });
}

export async function listBackups(): Promise<BackupInfo[]> {
	return apiFetch<BackupInfo[]>('/backups');
}

export async function restoreBackup(name: string): Promise<{ status: string }> {
	return apiFetch<{ status: string }>(`/backups/${encodeURIComponent(name)}/restore`, {
		method: 'POST'
	});
}

// --- accounts ---

function mapAccount(raw: Record<string, unknown>): Account {
	return {
		id: raw.id as number,
		name: raw.name as string,
		account_number: ns(raw.account_number as nullString | null),
		sort_order: raw.sort_order as number,
		archived: raw.archived as number,
		created_at: raw.created_at as string
	};
}

export async function listAccounts(): Promise<Account[]> {
	const raw = await apiFetch<Record<string, unknown>[]>('/accounts');
	return raw.map(mapAccount);
}

export async function createAccount(name: string, accountNumber?: string): Promise<Account> {
	const raw = await apiFetch<Record<string, unknown>>('/accounts', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name, account_number: accountNumber ?? null })
	});
	return mapAccount(raw);
}

export async function updateAccount(
	id: number,
	patch: { name?: string; account_number?: string | null; sort_order?: number; archived?: boolean }
): Promise<Account> {
	const raw = await apiFetch<Record<string, unknown>>(`/accounts/${id}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(patch)
	});
	return mapAccount(raw);
}

export async function deleteAccount(id: number): Promise<void> {
	await apiFetch(`/accounts/${id}`, { method: 'DELETE' });
}

// --- categories / subcategories ---

function mapSubcategory(raw: Record<string, unknown>): Subcategory {
	return {
		id: raw.id as number,
		category_id: raw.category_id as number,
		name: raw.name as string,
		color: raw.color as string,
		icon: ns(raw.icon as nullString | null),
		hotkey: ns(raw.hotkey as nullString | null),
		sort_order: raw.sort_order as number
	};
}

export async function listCategories(): Promise<CategoryWithSubcategories[]> {
	const raw = await apiFetch<
		{
			id: number;
			key: string;
			name: string;
			color: string;
			subcategories: Record<string, unknown>[];
		}[]
	>('/categories');
	return raw.map((c) => ({ ...c, subcategories: c.subcategories.map(mapSubcategory) }));
}

export async function listSubcategories(): Promise<Subcategory[]> {
	const raw = await apiFetch<Record<string, unknown>[]>('/subcategories');
	return raw.map(mapSubcategory);
}

export async function createSubcategory(input: {
	category_id: number;
	name: string;
	color?: string;
	hotkey?: string | null;
	sort_order?: number;
}): Promise<Subcategory> {
	const raw = await apiFetch<Record<string, unknown>>('/subcategories', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(input)
	});
	return mapSubcategory(raw);
}

export async function updateSubcategory(
	id: number,
	patch: {
		category_id?: number;
		name?: string;
		color?: string;
		icon?: string | null;
		hotkey?: string | null;
		sort_order?: number;
	}
): Promise<Subcategory> {
	const raw = await apiFetch<Record<string, unknown>>(`/subcategories/${id}`, {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(patch)
	});
	return mapSubcategory(raw);
}

export async function deleteSubcategory(id: number, mergeIntoId: number): Promise<void> {
	await apiFetch(`/subcategories/${id}?mergeIntoId=${mergeIntoId}`, { method: 'DELETE' });
}

// --- imports ---

export function previewImport(file: File, accountId: number): Promise<PreviewResult> {
	const form = new FormData();
	form.append('accountId', String(accountId));
	form.append('file', file);
	return apiFetch<PreviewResult>('/imports/preview', { method: 'POST', body: form });
}

export function commitImport(
	file: File,
	accountId: number,
	previewToken: string
): Promise<CommitResult> {
	const form = new FormData();
	form.append('accountId', String(accountId));
	form.append('previewToken', previewToken);
	form.append('file', file);
	return apiFetch<CommitResult>('/imports', { method: 'POST', body: form });
}

export async function undoImport(id: number): Promise<{ status: string; importId: number }> {
	return apiFetch(`/imports/${id}/undo`, { method: 'POST' });
}

function mapBatch(raw: Record<string, unknown>): ImportBatch {
	return {
		id: raw.id as number,
		account_id: raw.account_id as number,
		account_name: raw.account_name as string,
		file_name: raw.file_name as string,
		file_sha256: raw.file_sha256 as string,
		imported_at: raw.imported_at as string,
		line_count: raw.line_count as number,
		duplicate_count: raw.duplicate_count as number,
		undone_at: raw.undone_at as nullString | null,
		date_from: raw.date_from as string | null,
		date_to: raw.date_to as string | null,
		actual_line_count: raw.actual_line_count as number
	};
}

export async function listImports(): Promise<ImportBatch[]> {
	const raw = await apiFetch<Record<string, unknown>[]>('/imports');
	return raw.map(mapBatch);
}

// --- statement lines ---

export async function listLines(queryString: string): Promise<LinesResponse> {
	return apiFetch<LinesResponse>(`/lines${queryString ? '?' + queryString : ''}`);
}

export async function getLine(id: number): Promise<StatementLine> {
	return apiFetch<StatementLine>(`/lines/${id}`);
}

// --- settings ---

export async function getSettings(): Promise<Record<string, string>> {
	const raw = await apiFetch<{ settings: Record<string, string> }>('/settings');
	return raw.settings;
}

export async function updateSettings(settings: Record<string, string>): Promise<void> {
	await apiFetch('/settings', {
		method: 'PUT',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(settings)
	});
}

export { ni };
