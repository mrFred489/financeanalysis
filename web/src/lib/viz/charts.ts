// ECharts option builders for the six M5 dashboard charts (VIZ-1a–f).
// Each function takes aggregated data + colors and returns an EChartsOption.
// Drill-down params are attached via the `dataZoom`/click handler in the page.

import type { EChartsOption } from 'echarts';
import type { Bucket, Granularity } from './bucket';
import type { AvgBucket } from './average';
import type { ColorMap } from './colors';
import {
	categoryColor,
	subcategoryColor,
	UNCATEGORIZED_COLOR,
	CONFLICT_COLOR,
	TRANSFER_COLOR
} from './colors';
import type { VizShare } from '$lib/api';

// minor units → display units (kr): divide by 100
export function toKr(minor: number): number {
	return minor / 100;
}

const CATEGORY_ORDER = ['fixed', 'variable', 'savings'];
const CATEGORY_LABELS: Record<string, string> = {
	income: 'Income',
	fixed: 'Fixed costs',
	variable: 'Variable spending',
	savings: 'Savings'
};

// Shared category x-axis. The bottom grid margin is enlarged (GRID_SLANTED)
// so labels have room and don't collide with content below the chart.
function categoryXAxis(labels: string[]): EChartsOption['xAxis'] {
	return {
		type: 'category',
		data: labels,
		axisLabel: { interval: 0 }
	};
}

const GRID_SLANTED = { left: 60, right: 20, top: 40, bottom: 60 };

/** VIZ-1a: monthly stacked bars of fixed/variable/savings + income as a line. */
export function monthlyStackedBarOptions(
	buckets: Bucket[],
	colors: ColorMap,
	includeTransfers: boolean
): EChartsOption {
	const labels = buckets.map((b) => b.label);
	const series: EChartsOption['series'] = CATEGORY_ORDER.map((key) => ({
		name: CATEGORY_LABELS[key],
		type: 'bar',
		stack: 'expenses',
		itemStyle: { color: categoryColor(colors, key) },
		data: buckets.map((b) => toKr(-(b.byCategory[key] ?? 0))) // expenses as positive
	}));
	series.push({
		name: CATEGORY_LABELS.income,
		type: 'line',
		itemStyle: { color: categoryColor(colors, 'income') },
		lineStyle: { width: 2 },
		data: buckets.map((b) => toKr(b.byCategory.income ?? 0))
	});
	if (buckets.some((b) => b.uncategorized !== 0)) {
		series.push({
			name: 'Uncategorized',
			type: 'bar',
			stack: 'expenses',
			itemStyle: { color: UNCATEGORIZED_COLOR },
			data: buckets.map((b) => toKr(-b.uncategorized))
		});
	}
	if (buckets.some((b) => b.conflict !== 0)) {
		series.push({
			name: 'Conflict',
			type: 'bar',
			stack: 'expenses',
			itemStyle: { color: CONFLICT_COLOR },
			data: buckets.map((b) => toKr(-b.conflict))
		});
	}
	if (includeTransfers && buckets.some((b) => b.transfer !== 0)) {
		series.push({
			name: 'Transfers',
			type: 'bar',
			itemStyle: { color: TRANSFER_COLOR },
			data: buckets.map((b) => toKr(b.transfer))
		});
	}
	return {
		tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
		legend: { data: series.map((s) => (s as { name: string }).name) },
		grid: GRID_SLANTED,
		xAxis: categoryXAxis(labels),
		yAxis: { type: 'value', name: 'kr' },
		series
	};
}

/** VIZ-1b: income vs expenses vs savings per month + savings rate line. */
export function incomeVsExpensesOptions(buckets: Bucket[], colors: ColorMap): EChartsOption {
	const labels = buckets.map((b) => b.label);
	const income = buckets.map((b) => toKr(b.byCategory.income ?? 0));
	const expenses = buckets.map((b) =>
		toKr(-((b.byCategory.fixed ?? 0) + (b.byCategory.variable ?? 0) + (b.byCategory.savings ?? 0)))
	);
	const savings = buckets.map((b) => toKr(-(b.byCategory.savings ?? 0)));
	const savingsRate = buckets.map((b) => {
		const inc = b.byCategory.income ?? 0;
		const sav = -(b.byCategory.savings ?? 0);
		return inc > 0 ? (sav / inc) * 100 : 0;
	});
	return {
		tooltip: { trigger: 'axis' },
		legend: { data: ['Income', 'Expenses', 'Savings', 'Savings rate'] },
		grid: { left: 60, right: 60, top: 40, bottom: 60 },
		xAxis: categoryXAxis(labels),
		yAxis: [
			{ type: 'value', name: 'kr' },
			{ type: 'value', name: '%', max: 100 }
		],
		series: [
			{
				name: 'Income',
				type: 'bar',
				itemStyle: { color: categoryColor(colors, 'income') },
				data: income
			},
			{
				name: 'Expenses',
				type: 'bar',
				itemStyle: { color: categoryColor(colors, 'fixed') },
				data: expenses
			},
			{
				name: 'Savings',
				type: 'bar',
				itemStyle: { color: categoryColor(colors, 'savings') },
				data: savings
			},
			{
				name: 'Savings rate',
				type: 'line',
				yAxisIndex: 1,
				itemStyle: { color: '#10b981' },
				data: savingsRate.map((v) => Math.round(v * 10) / 10)
			}
		]
	};
}

/** VIZ-1c: subcategory breakdown over time for a selected category. */
export function subcategoryBreakdownOptions(
	buckets: Bucket[],
	colors: ColorMap,
	categoryKey: string,
	subcategoryIds: number[]
): EChartsOption {
	const labels = buckets.map((b) => b.label);
	const series = subcategoryIds.map((id) => ({
		name: colors.subcategoryName[id] ?? `#${id}`,
		type: 'bar' as const,
		stack: 'total',
		itemStyle: { color: subcategoryColor(colors, id) },
		data: buckets.map((b) => toKr(-(b.bySubcategory[String(id)] ?? 0)))
	}));
	return {
		tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' } },
		legend: { data: series.map((s) => s.name) },
		grid: GRID_SLANTED,
		xAxis: categoryXAxis(labels),
		yAxis: { type: 'value', name: 'kr' },
		series
	};
}

/** VIZ-1d: subcategory share donut for the selected period. */
export function subcategoryShareOptions(shares: VizShare[], colors: ColorMap): EChartsOption {
	const data = shares
		.filter((s) => s.amountMinor !== 0)
		.map((s) => ({
			name: s.subcategoryName,
			value: Math.abs(toKr(s.amountMinor)),
			itemStyle: { color: s.color || subcategoryColor(colors, s.subcategoryId) }
		}));
	return {
		tooltip: { trigger: 'item', formatter: '{b}: {c} kr ({d}%)' },
		legend: { orient: 'vertical', left: 'left' },
		series: [
			{
				type: 'pie',
				radius: ['40%', '70%'],
				avoidLabelOverlap: true,
				itemStyle: { borderRadius: 6, borderColor: '#fff', borderWidth: 2 },
				label: { show: true, formatter: '{b}' },
				data
			}
		]
	};
}

/** VIZ-1e: rolling X-month average per category. */
export function rollingAverageOptions(
	avgBuckets: AvgBucket[],
	colors: ColorMap,
	categoryKeys: string[]
): EChartsOption {
	const labels = avgBuckets.map((b) => b.label);
	return {
		tooltip: { trigger: 'axis' },
		legend: { data: categoryKeys.map((k) => CATEGORY_LABELS[k] ?? k) },
		grid: GRID_SLANTED,
		xAxis: categoryXAxis(labels),
		yAxis: { type: 'value', name: 'kr' },
		series: categoryKeys.map((key) => ({
			name: CATEGORY_LABELS[key] ?? key,
			type: 'line' as const,
			smooth: true,
			itemStyle: { color: categoryColor(colors, key) },
			data: avgBuckets.map((b) => Math.round(toKr(b.values[key] ?? 0) * 100) / 100)
		}))
	};
}

/** VIZ-1f: per-account average monthly spending over the last X months. */
export function accountAverageOptions(
	buckets: Bucket[],
	accountNames: Record<number, string>,
	window: number
): EChartsOption {
	if (buckets.length === 0) {
		return { title: { text: 'No data' } };
	}
	// Take the last `window` buckets
	const slice = buckets.slice(-window);
	const accountIds = new Set<number>();
	for (const b of slice) {
		for (const id of Object.keys(b.byAccount)) accountIds.add(Number(id));
	}
	const labels = [...accountIds].map((id) => accountNames[id] ?? `Account ${id}`);
	const series = labels.map((name, i) => {
		const id = [...accountIds][i];
		const avg = slice.reduce((sum, b) => sum + (b.byAccount[String(id)] ?? 0), 0) / slice.length;
		return { name, value: Math.abs(Math.round(toKr(avg) * 100) / 100) };
	});
	return {
		tooltip: { trigger: 'item' },
		grid: { left: 60, right: 20, top: 20, bottom: 60 },
		xAxis: categoryXAxis(labels),
		yAxis: { type: 'value', name: 'kr/month avg' },
		series: [
			{
				type: 'bar',
				itemStyle: { color: '#3b82f6' },
				data: series.map((s) => s.value)
			}
		]
	};
}

/** Granularity label helper. */
export function granularityLabel(g: Granularity): string {
	return g === 'month' ? 'Month' : g === 'quarter' ? 'Quarter' : 'Year';
}
