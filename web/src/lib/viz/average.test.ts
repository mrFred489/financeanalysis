import { describe, it, expect } from 'vitest';
import { rollingAverage } from './average';
import type { Bucket } from './bucket';

function mkBucket(label: string, income: number): Bucket {
	return {
		label,
		byCategory: { income },
		bySubcategory: {},
		byAccount: {},
		uncategorized: 0,
		conflict: 0,
		transfer: 0
	};
}

describe('rollingAverage', () => {
	const buckets = [
		mkBucket('2026-01', 100),
		mkBucket('2026-02', 200),
		mkBucket('2026-03', 300),
		mkBucket('2026-04', 400)
	];

	it('window=1 returns raw values', () => {
		const out = rollingAverage(buckets, 1, ['income']);
		expect(out.map((b) => b.values.income)).toEqual([100, 200, 300, 400]);
	});

	it('window=3 computes rolling mean with partial windows at start', () => {
		const out = rollingAverage(buckets, 3, ['income']);
		// i=0: [100] → 100; i=1: [100,200] → 150; i=2: [100,200,300] → 200; i=3: [200,300,400] → 300
		expect(out.map((b) => b.values.income)).toEqual([100, 150, 200, 300]);
	});

	it('window larger than data uses all available', () => {
		const out = rollingAverage(buckets, 10, ['income']);
		expect(out[3].values.income).toBe(250); // (100+200+300+400)/4
	});

	it('empty input', () => {
		expect(rollingAverage([], 3, ['income'])).toEqual([]);
	});

	it('preserves labels', () => {
		const out = rollingAverage(buckets, 2, ['income']);
		expect(out.map((b) => b.label)).toEqual(['2026-01', '2026-02', '2026-03', '2026-04']);
	});
});
