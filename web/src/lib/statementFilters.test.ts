import { describe, expect, it } from 'vitest';
import {
	defaultFilters,
	filtersFromParams,
	filtersToQueryString,
	paramsFromFilters,
	presetToRange,
	rangeToPreset
} from './statementFilters';

describe('statementFilters URL serialization', () => {
	it('round-trips a full filter set', () => {
		const f = defaultFilters();
		f.from = '2024-01-01';
		f.to = '2024-12-31';
		f.accounts = [1, 2];
		f.direction = 'out';
		f.amountMin = '-50000';
		f.amountMax = '100000';
		f.message = 'Netto';
		f.categories = [3];
		f.subcategories = [7, 8];
		f.source = 'none';
		f.importId = '5';
		f.spreadGt = '1';
		f.sort = 'amount';
		f.order = 'asc';
		f.page = 3;
		f.pageSize = 100;

		const sp = paramsFromFilters(f);
		const back = filtersFromParams(sp);
		expect(back).toEqual(f);
	});

	it('omits defaults from the query string', () => {
		const sp = paramsFromFilters(defaultFilters());
		expect(sp.toString()).toBe('');
	});

	it('parses empty params to defaults', () => {
		expect(filtersFromParams(new URLSearchParams())).toEqual(defaultFilters());
	});

	it('ignores invalid list values', () => {
		const sp = new URLSearchParams('accounts=1,,x,3&direction=sideways&order=up&page=abc');
		const f = filtersFromParams(sp);
		expect(f.accounts).toEqual([1, 3]);
		expect(f.direction).toBe('');
		expect(f.order).toBe('desc');
		expect(f.page).toBe(1);
	});

	it('filtersToQueryString produces a usable query string', () => {
		const f = defaultFilters();
		f.message = 'Køb; a';
		f.page = 2;
		expect(filtersToQueryString(f)).toBe('message=K%C3%B8b%3B+a&page=2');
	});
});

describe('date presets', () => {
	const today = new Date(Date.UTC(2025, 5, 15)); // 2025-06-15

	it('all is empty range', () => {
		expect(presetToRange('all', today)).toEqual({ from: '', to: '' });
		expect(rangeToPreset('', '', today)).toBe('all');
	});

	it('ytd', () => {
		expect(presetToRange('ytd', today)).toEqual({ from: '2025-01-01', to: '2025-06-15' });
		expect(rangeToPreset('2025-01-01', '2025-06-15', today)).toBe('ytd');
	});

	it('prevYear', () => {
		expect(presetToRange('prevYear', today)).toEqual({ from: '2024-01-01', to: '2024-12-31' });
		expect(rangeToPreset('2024-01-01', '2024-12-31', today)).toBe('prevYear');
	});

	it('last12m', () => {
		expect(presetToRange('last12m', today)).toEqual({ from: '2024-06-16', to: '2025-06-15' });
		expect(rangeToPreset('2024-06-16', '2025-06-15', today)).toBe('last12m');
	});

	it('custom falls back for anything else', () => {
		expect(rangeToPreset('2020-01-01', '2020-02-01', today)).toBe('custom');
	});
});
