// Statement filter state, serialized to URL query params (STMT-5: bookmarkable, back/forward works).

export interface StatementFilters {
	from: string;
	to: string;
	accounts: number[];
	direction: '' | 'in' | 'out';
	amountMin: string; // minor units (øre), '' = unset
	amountMax: string;
	message: string;
	categories: number[];
	subcategories: number[];
	source: string;
	importId: string;
	spreadGt: string;
	sort: string;
	order: 'asc' | 'desc';
	page: number;
	pageSize: number;
}

export const DEFAULT_PAGE_SIZE = 50;

export function defaultFilters(): StatementFilters {
	return {
		from: '',
		to: '',
		accounts: [],
		direction: '',
		amountMin: '',
		amountMax: '',
		message: '',
		categories: [],
		subcategories: [],
		source: '',
		importId: '',
		spreadGt: '',
		sort: 'date',
		order: 'desc',
		page: 1,
		pageSize: DEFAULT_PAGE_SIZE
	};
}

const idList = (v: string): number[] =>
	v
		.split(',')
		.map((s) => s.trim())
		.filter((s) => s !== '')
		.map(Number)
		.filter((n) => Number.isInteger(n));

export function filtersFromParams(sp: URLSearchParams): StatementFilters {
	const f = defaultFilters();
	f.from = sp.get('from') ?? '';
	f.to = sp.get('to') ?? '';
	f.accounts = idList(sp.get('accounts') ?? '');
	const dir = sp.get('direction') ?? '';
	f.direction = dir === 'in' || dir === 'out' ? dir : '';
	f.amountMin = sp.get('amountMin') ?? '';
	f.amountMax = sp.get('amountMax') ?? '';
	f.message = sp.get('message') ?? '';
	f.categories = idList(sp.get('categories') ?? '');
	f.subcategories = idList(sp.get('subcategories') ?? '');
	f.source = sp.get('source') ?? '';
	f.importId = sp.get('importId') ?? '';
	f.spreadGt = sp.get('spreadGt') ?? '';
	f.sort = sp.get('sort') ?? 'date';
	const order = sp.get('order') ?? 'desc';
	f.order = order === 'asc' ? 'asc' : 'desc';
	f.page = Math.max(1, parseInt(sp.get('page') ?? '1', 10) || 1);
	f.pageSize = Math.max(
		1,
		parseInt(sp.get('pageSize') ?? String(DEFAULT_PAGE_SIZE), 10) || DEFAULT_PAGE_SIZE
	);
	return f;
}

// paramsFromFilters serializes the filters, omitting defaults and empty values.
export function paramsFromFilters(f: StatementFilters): URLSearchParams {
	const sp = new URLSearchParams();
	if (f.from) sp.set('from', f.from);
	if (f.to) sp.set('to', f.to);
	if (f.accounts.length) sp.set('accounts', f.accounts.join(','));
	if (f.direction) sp.set('direction', f.direction);
	if (f.amountMin) sp.set('amountMin', f.amountMin);
	if (f.amountMax) sp.set('amountMax', f.amountMax);
	if (f.message) sp.set('message', f.message);
	if (f.categories.length) sp.set('categories', f.categories.join(','));
	if (f.subcategories.length) sp.set('subcategories', f.subcategories.join(','));
	if (f.source) sp.set('source', f.source);
	if (f.importId) sp.set('importId', f.importId);
	if (f.spreadGt) sp.set('spreadGt', f.spreadGt);
	if (f.sort !== 'date') sp.set('sort', f.sort);
	if (f.order !== 'desc') sp.set('order', f.order);
	if (f.page !== 1) sp.set('page', String(f.page));
	if (f.pageSize !== DEFAULT_PAGE_SIZE) sp.set('pageSize', String(f.pageSize));
	return sp;
}

// toQueryString returns the serialized filters for API calls (all params, defaults included is fine).
export function filtersToQueryString(f: StatementFilters): string {
	return paramsFromFilters(f).toString();
}

// Date-range presets for the filter bar (STMT-2).
export type DatePreset = 'all' | 'last12m' | 'ytd' | 'prevYear' | 'custom';

export function presetToRange(
	preset: DatePreset,
	today = new Date()
): { from: string; to: string } {
	const iso = (d: Date) => d.toISOString().slice(0, 10);
	switch (preset) {
		case 'last12m': {
			const from = new Date(
				Date.UTC(today.getUTCFullYear() - 1, today.getUTCMonth(), today.getUTCDate() + 1)
			);
			return { from: iso(from), to: iso(today) };
		}
		case 'ytd':
			return { from: `${today.getUTCFullYear()}-01-01`, to: iso(today) };
		case 'prevYear': {
			const y = today.getUTCFullYear() - 1;
			return { from: `${y}-01-01`, to: `${y}-12-31` };
		}
		default:
			return { from: '', to: '' };
	}
}

export function rangeToPreset(from: string, to: string, today = new Date()): DatePreset {
	if (!from && !to) return 'all';
	const last12m = presetToRange('last12m', today);
	const ytd = presetToRange('ytd', today);
	const prevYear = presetToRange('prevYear', today);
	if (from === last12m.from && to === last12m.to) return 'last12m';
	if (from === ytd.from && to === ytd.to) return 'ytd';
	if (from === prevYear.from && to === prevYear.to) return 'prevYear';
	return 'custom';
}
