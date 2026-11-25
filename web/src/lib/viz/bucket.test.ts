import { describe, it, expect } from 'vitest';
import { rebucket, quarterOf, bucketRange } from './bucket';
import type { VizBucket } from '$lib/api';

function mkBucket(
	month: string,
	byCategory: Record<string, number> = {},
	extra: Partial<VizBucket> = {}
): VizBucket {
	return {
		month,
		byCategory,
		bySubcategory: {},
		byAccount: {},
		uncategorized: 0,
		conflict: 0,
		transfer: 0,
		...extra
	};
}

describe('quarterOf', () => {
	it('maps months to quarters', () => {
		expect(quarterOf('2026-01')).toBe('2026-Q1');
		expect(quarterOf('2026-03')).toBe('2026-Q1');
		expect(quarterOf('2026-04')).toBe('2026-Q2');
		expect(quarterOf('2026-12')).toBe('2026-Q4');
	});
});

describe('rebucket', () => {
	const buckets = [
		mkBucket('2026-01', { income: 100, fixed: -50 }),
		mkBucket('2026-02', { income: 200, fixed: -60 }),
		mkBucket('2026-03', { income: 300, fixed: -70 }),
		mkBucket('2026-04', { income: 400, fixed: -80 })
	];

	it('month granularity passes through', () => {
		const out = rebucket(buckets, 'month');
		expect(out.length).toBe(4);
		expect(out[0].label).toBe('2026-01');
	});

	it('quarter granularity merges 3 months', () => {
		const out = rebucket(buckets, 'quarter');
		expect(out.length).toBe(2);
		expect(out[0].label).toBe('2026-Q1');
		expect(out[0].byCategory.income).toBe(600); // 100+200+300
		expect(out[0].byCategory.fixed).toBe(-180); // -50-60-70
		expect(out[1].label).toBe('2026-Q2');
		expect(out[1].byCategory.income).toBe(400);
	});

	it('year granularity merges all', () => {
		const out = rebucket(buckets, 'year');
		expect(out.length).toBe(1);
		expect(out[0].label).toBe('2026');
		expect(out[0].byCategory.income).toBe(1000);
	});

	it('sorts by label ascending', () => {
		const shuffled = [buckets[2], buckets[0], buckets[3], buckets[1]];
		const out = rebucket(shuffled, 'year');
		expect(out[0].label).toBe('2026');
	});

	it('handles empty input', () => {
		expect(rebucket([], 'month')).toEqual([]);
	});

	it('accumulates uncategorized/conflict/transfer', () => {
		const bs = [
			mkBucket('2026-01', {}, { uncategorized: -100, conflict: -50, transfer: 200 }),
			mkBucket('2026-02', {}, { uncategorized: -200, conflict: -10, transfer: -200 })
		];
		const out = rebucket(bs, 'quarter');
		expect(out[0].uncategorized).toBe(-300);
		expect(out[0].conflict).toBe(-60);
		expect(out[0].transfer).toBe(0);
	});
});

describe('bucketRange', () => {
	it('month range', () => {
		expect(bucketRange('2026-02', 'month')).toEqual({ from: '2026-02-01', to: '2026-02-28' });
	});
	it('month range leap year', () => {
		expect(bucketRange('2024-02', 'month')).toEqual({ from: '2024-02-01', to: '2024-02-29' });
	});
	it('quarter range', () => {
		expect(bucketRange('2026-Q1', 'quarter')).toEqual({ from: '2026-01-01', to: '2026-03-31' });
		expect(bucketRange('2026-Q4', 'quarter')).toEqual({ from: '2026-10-01', to: '2026-12-31' });
	});
	it('year range', () => {
		expect(bucketRange('2026', 'year')).toEqual({ from: '2026-01-01', to: '2026-12-31' });
	});
});
