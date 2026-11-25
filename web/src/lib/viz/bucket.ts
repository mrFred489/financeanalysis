// Rebucket monthly viz buckets to quarter or year granularity (M5 D4).

import type { VizBucket } from '$lib/api';

export type Granularity = 'month' | 'quarter' | 'year';

export interface Bucket {
	label: string; // YYYY-MM, YYYY-Qn, or YYYY
	byCategory: Record<string, number>;
	bySubcategory: Record<string, number>;
	byAccount: Record<string, number>;
	uncategorized: number;
	conflict: number;
	transfer: number;
}

export function quarterOf(month: string): string {
	// month is YYYY-MM
	const m = parseInt(month.slice(5, 7), 10);
	const q = Math.ceil(m / 3);
	return `${month.slice(0, 4)}-Q${q}`;
}

export function rebucket(buckets: VizBucket[], granularity: Granularity): Bucket[] {
	if (granularity === 'month') {
		return buckets.map((b) => ({
			label: b.month,
			byCategory: { ...b.byCategory },
			bySubcategory: { ...b.bySubcategory },
			byAccount: { ...b.byAccount },
			uncategorized: b.uncategorized,
			conflict: b.conflict,
			transfer: b.transfer
		}));
	}
	const map = new Map<string, Bucket>();
	for (const b of buckets) {
		const label = granularity === 'quarter' ? quarterOf(b.month) : b.month.slice(0, 4);
		let target = map.get(label);
		if (!target) {
			target = {
				label,
				byCategory: {},
				bySubcategory: {},
				byAccount: {},
				uncategorized: 0,
				conflict: 0,
				transfer: 0
			};
			map.set(label, target);
		}
		mergeInto(target.byCategory, b.byCategory);
		mergeInto(target.bySubcategory, b.bySubcategory);
		mergeInto(target.byAccount, b.byAccount);
		target.uncategorized += b.uncategorized;
		target.conflict += b.conflict;
		target.transfer += b.transfer;
	}
	const out = [...map.values()];
	out.sort((a, b) => a.label.localeCompare(b.label));
	return out;
}

function mergeInto(target: Record<string, number>, src: Record<string, number>): void {
	for (const [k, v] of Object.entries(src)) {
		target[k] = (target[k] ?? 0) + v;
	}
}

// monthRange returns the {from, to} date range covering a bucket label,
// used for drill-down (VIZ-4).
export function bucketRange(label: string, granularity: Granularity): { from: string; to: string } {
	if (granularity === 'month') {
		// YYYY-MM
		const y = parseInt(label.slice(0, 4), 10);
		const m = parseInt(label.slice(5, 7), 10);
		const lastDay = new Date(Date.UTC(y, m, 0)).getUTCDate();
		return { from: `${label}-01`, to: `${label}-${String(lastDay).padStart(2, '0')}` };
	}
	if (granularity === 'quarter') {
		// YYYY-Qn
		const y = parseInt(label.slice(0, 4), 10);
		const q = parseInt(label.slice(6), 10);
		const startMonth = (q - 1) * 3 + 1;
		const endMonth = startMonth + 2;
		const from = `${y}-${String(startMonth).padStart(2, '0')}-01`;
		const lastDay = new Date(Date.UTC(y, endMonth, 0)).getUTCDate();
		const to = `${y}-${String(endMonth).padStart(2, '0')}-${String(lastDay).padStart(2, '0')}`;
		return { from, to };
	}
	// year
	const y = label;
	return { from: `${y}-01-01`, to: `${y}-12-31` };
}
