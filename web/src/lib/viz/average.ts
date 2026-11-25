// Rolling X-month average over viz buckets (M5 VIZ-1e, D4: client-side).

import type { Bucket } from './bucket';

export interface AvgBucket {
	label: string;
	values: Record<string, number>; // series key → average
}

/**
 * Compute a rolling average of `window` buckets per series. The value at
 * position i is the mean of buckets [i-window+1 .. i] (only available
 * buckets count, so the first window-1 positions use partial windows).
 */
export function rollingAverage(
	buckets: Bucket[],
	window: number,
	seriesKeys: string[]
): AvgBucket[] {
	if (window < 1) window = 1;
	const out: AvgBucket[] = [];
	for (let i = 0; i < buckets.length; i++) {
		const start = Math.max(0, i - window + 1);
		const slice = buckets.slice(start, i + 1);
		const values: Record<string, number> = {};
		for (const key of seriesKeys) {
			let sum = 0;
			for (const b of slice) {
				sum += b.byCategory[key] ?? b.bySubcategory[key] ?? b.byAccount[key] ?? 0;
			}
			values[key] = slice.length > 0 ? sum / slice.length : 0;
		}
		out.push({ label: buckets[i].label, values });
	}
	return out;
}
