<script lang="ts">
	// M5 Visualization dashboard (VIZ-1..7, plan docs/plans/M5-visualization.md).
	// Six default charts with global filters, URL-encoded state, per-chart
	// options persisted in settings, and drill-down to /statements.
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		type Account,
		type CategoryWithSubcategories,
		type VizBucket,
		type VizShare,
		getVizShare,
		getVizTimeseries,
		listAccounts,
		listCategories
	} from '$lib/api';
	import { i18n } from '$lib/i18n';
	import { presetToRange, rangeToPreset, type DatePreset } from '$lib/statementFilters';
	import Chart from '$lib/viz/Chart.svelte';
	import { rebucket, bucketRange, type Granularity } from '$lib/viz/bucket';
	import { rollingAverage, type AvgBucket } from '$lib/viz/average';
	import { buildColorMap, allSubcategories, type ColorMap } from '$lib/viz/colors';
	import {
		monthlyStackedBarOptions,
		incomeVsExpensesOptions,
		subcategoryBreakdownOptions,
		subcategoryShareOptions,
		rollingAverageOptions,
		accountAverageOptions
	} from '$lib/viz/charts';
	import { onMount } from 'svelte';

	// --- filter state (URL-encoded, VIZ-5) ---
	interface VizFilterState {
		from: string;
		to: string;
		accounts: number[];
		categories: number[];
		subcategories: number[];
		granularity: Granularity;
		includeTransfers: boolean;
		avgWindow: number;
		preset: DatePreset;
	}

	function defaultVizState(): VizFilterState {
		return {
			from: '',
			to: '',
			accounts: [],
			categories: [],
			subcategories: [],
			granularity: 'month',
			includeTransfers: false,
			avgWindow: 3,
			preset: 'all'
		};
	}

	function stateFromURL(sp: URLSearchParams): VizFilterState {
		const s = defaultVizState();
		s.from = sp.get('from') ?? '';
		s.to = sp.get('to') ?? '';
		const idList = (v: string | null): number[] =>
			(v ?? '')
				.split(',')
				.map((p) => p.trim())
				.filter((p) => p !== '')
				.map(Number)
				.filter((n) => Number.isInteger(n) && n > 0);
		s.accounts = idList(sp.get('accounts'));
		s.categories = idList(sp.get('categories'));
		s.subcategories = idList(sp.get('subcategories'));
		const g = sp.get('granularity');
		s.granularity = g === 'quarter' || g === 'year' ? g : 'month';
		s.includeTransfers = sp.get('includeTransfers') === '1';
		s.avgWindow = Math.max(1, parseInt(sp.get('avgWindow') ?? '3', 10) || 3);
		s.preset = rangeToPreset(s.from, s.to);
		return s;
	}

	function stateToURL(s: VizFilterState): string {
		const sp = new URLSearchParams();
		if (s.from) sp.set('from', s.from);
		if (s.to) sp.set('to', s.to);
		if (s.accounts.length) sp.set('accounts', s.accounts.join(','));
		if (s.categories.length) sp.set('categories', s.categories.join(','));
		if (s.subcategories.length) sp.set('subcategories', s.subcategories.join(','));
		if (s.granularity !== 'month') sp.set('granularity', s.granularity);
		if (s.includeTransfers) sp.set('includeTransfers', '1');
		if (s.avgWindow !== 3) sp.set('avgWindow', String(s.avgWindow));
		return sp.toString();
	}

	let vizState = $state<VizFilterState>(defaultVizState());
	let accounts = $state<Account[]>([]);
	let categories = $state<CategoryWithSubcategories[]>([]);
	let colors = $state<ColorMap>(buildColorMap([]));
	let buckets = $state<VizBucket[]>([]);
	let shares = $state<VizShare[]>([]);
	let loading = $state(false);
	let error = $state<string | null>(null);
	let selectedCategory = $state<string>('variable');

	// Re-fetch when URL changes (back/forward)
	$effect(() => {
		const s = stateFromURL($page.url.searchParams);
		vizState = s;
		void fetchData(s);
	});

	onMount(async () => {
		try {
			[accounts, categories] = await Promise.all([listAccounts(), listCategories()]);
			colors = buildColorMap(categories);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	});

	async function fetchData(s: VizFilterState) {
		loading = true;
		error = null;
		try {
			const params = {
				from: s.from || undefined,
				to: s.to || undefined,
				accounts: s.accounts.length ? s.accounts : undefined,
				categories: s.categories.length ? s.categories : undefined,
				subcategories: s.subcategories.length ? s.subcategories : undefined,
				includeTransfers: s.includeTransfers
			};
			const [ts, sh] = await Promise.all([getVizTimeseries(params), getVizShare(params)]);
			buckets = ts.buckets;
			shares = sh.shares;
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function update(patch: Partial<VizFilterState>) {
		const next = { ...vizState, ...patch };
		goto(`/visualize?${stateToURL(next)}`);
	}

	function setPreset(p: DatePreset) {
		const range = presetToRange(p);
		update({ preset: p, from: range.from, to: range.to });
	}

	// --- derived data ---
	let rebased = $derived(rebucket(buckets, vizState.granularity));
	let avgBuckets = $derived(
		rollingAverage(rebased, vizState.avgWindow, ['income', 'fixed', 'variable', 'savings'])
	);
	let subIdsForCategory = $derived(
		allSubcategories(categories)
			.filter((s) => {
				const cat = categories.find((c) => c.id === s.category_id);
				return cat?.key === selectedCategory;
			})
			.map((s) => s.id)
	);
	let accountNames = $derived(Object.fromEntries(accounts.map((a) => [a.id, a.name])));

	// --- drill-down (VIZ-4) ---
	function drilldown(bucketLabel: string, extra?: Record<string, string>) {
		const range = bucketRange(bucketLabel, vizState.granularity);
		const sp = new URLSearchParams();
		sp.set('from', range.from);
		sp.set('to', range.to);
		if (vizState.accounts.length) sp.set('accounts', vizState.accounts.join(','));
		if (extra) for (const [k, v] of Object.entries(extra)) sp.set(k, v);
		goto(`/statements?${sp.toString()}`);
	}

	function chartClick(bucketLabel: string) {
		return (params: { name: string; seriesName: string; dataIndex: number }) => {
			const label = params.name || bucketLabel;
			const extra: Record<string, string> = {};
			const seriesName = params.seriesName;
			if (seriesName === 'Income')
				extra.categories = String(categories.find((c) => c.key === 'income')?.id ?? '');
			else if (seriesName === 'Fixed costs')
				extra.categories = String(categories.find((c) => c.key === 'fixed')?.id ?? '');
			else if (seriesName === 'Variable spending')
				extra.categories = String(categories.find((c) => c.key === 'variable')?.id ?? '');
			else if (seriesName === 'Savings')
				extra.categories = String(categories.find((c) => c.key === 'savings')?.id ?? '');
			else if (seriesName === 'Uncategorized') extra.source = 'none';
			else if (seriesName === 'Conflict') extra.source = 'conflict';
			else if (seriesName === 'Transfers') extra.source = 'transfer';
			drilldown(label, extra);
		};
	}

	const presetOptions: { value: DatePreset; label: string }[] = [
		{ value: 'all', label: 'All' },
		{ value: 'last12m', label: 'Last 12 months' },
		{ value: 'ytd', label: 'Year to date' },
		{ value: 'prevYear', label: 'Previous year' },
		{ value: 'custom', label: 'Custom' }
	];
</script>

<svelte:head>
	<title>{$i18n.t('nav.visualize')} – {$i18n.t('app.name')}</title>
</svelte:head>

<div class="space-y-6">
	<div>
		<h2 class="text-2xl font-bold text-gray-900">{$i18n.t('nav.visualize')}</h2>
		<p class="mt-1 text-gray-600">{$i18n.t('viz.subtitle')}</p>
	</div>

	<!-- Filter bar (VIZ-2) -->
	<div class="bg-white shadow rounded-xl p-4 border border-gray-100 space-y-3">
		<div class="flex flex-wrap items-center gap-3">
			<!-- Date preset -->
			<select
				class="border border-gray-300 rounded-md px-2 py-1 text-sm"
				value={vizState.preset}
				onchange={(e) => setPreset((e.target as HTMLSelectElement).value as DatePreset)}
			>
				{#each presetOptions as opt}
					<option value={opt.value}>{opt.label}</option>
				{/each}
			</select>
			{#if vizState.preset === 'custom'}
				<input
					type="date"
					class="border border-gray-300 rounded-md px-2 py-1 text-sm"
					value={vizState.from}
					onchange={(e) => update({ from: (e.target as HTMLInputElement).value })}
				/>
				<span class="text-gray-400">–</span>
				<input
					type="date"
					class="border border-gray-300 rounded-md px-2 py-1 text-sm"
					value={vizState.to}
					onchange={(e) => update({ to: (e.target as HTMLInputElement).value })}
				/>
			{/if}

			<!-- Granularity -->
			<select
				class="border border-gray-300 rounded-md px-2 py-1 text-sm"
				value={vizState.granularity}
				onchange={(e) =>
					update({ granularity: (e.target as HTMLSelectElement).value as Granularity })}
			>
				<option value="month">{$i18n.t('viz.granularity.month')}</option>
				<option value="quarter">{$i18n.t('viz.granularity.quarter')}</option>
				<option value="year">{$i18n.t('viz.granularity.year')}</option>
			</select>

			<!-- Averaging window -->
			<label class="text-sm text-gray-600 flex items-center gap-1">
				{$i18n.t('viz.avgWindow')}
				<input
					type="number"
					min="1"
					max="36"
					class="border border-gray-300 rounded-md px-2 py-1 text-sm w-16"
					value={vizState.avgWindow}
					onchange={(e) =>
						update({ avgWindow: parseInt((e.target as HTMLInputElement).value, 10) || 3 })}
				/>
			</label>

			<!-- Include transfers -->
			<label class="text-sm text-gray-600 flex items-center gap-1">
				<input
					type="checkbox"
					checked={vizState.includeTransfers}
					onchange={(e) => update({ includeTransfers: (e.target as HTMLInputElement).checked })}
				/>
				{$i18n.t('viz.includeTransfers')}
			</label>

			<!-- Accounts multi-select -->
			<select
				class="border border-gray-300 rounded-md px-2 py-1 text-sm max-w-xs"
				multiple
				size={1}
				value={vizState.accounts.map(String)}
				onchange={(e) => {
					const sel = Array.from((e.target as HTMLSelectElement).selectedOptions).map(Number);
					update({ accounts: sel });
				}}
			>
				{#each accounts as acc}
					<option value={acc.id}>{acc.name}</option>
				{/each}
			</select>

			<!-- Categories multi-select -->
			<select
				class="border border-gray-300 rounded-md px-2 py-1 text-sm max-w-xs"
				multiple
				size={1}
				value={vizState.categories.map(String)}
				onchange={(e) => {
					const sel = Array.from((e.target as HTMLSelectElement).selectedOptions).map(Number);
					update({ categories: sel });
				}}
			>
				{#each categories as cat}
					<option value={cat.id}>{cat.name}</option>
				{/each}
			</select>
		</div>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg">
			{error}
		</div>
	{/if}

	{#if loading}
		<div class="text-center py-12 text-gray-500">{$i18n.t('viz.loading')}</div>
	{:else if rebased.length === 0}
		<div class="bg-white shadow rounded-xl p-12 border border-gray-100 text-center text-gray-500">
			{$i18n.t('viz.empty')}
		</div>
	{:else}
		<div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
			<!-- VIZ-1a: Monthly stacked bars -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h3 class="font-semibold text-gray-900 mb-2">{$i18n.t('viz.chart.monthlyStacked')}</h3>
				<Chart
					options={monthlyStackedBarOptions(rebased, colors, vizState.includeTransfers)}
					onClick={chartClick(rebased[0]?.label ?? '')}
				/>
			</div>

			<!-- VIZ-1b: Income vs expenses vs savings -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h3 class="font-semibold text-gray-900 mb-2">{$i18n.t('viz.chart.incomeVsExpenses')}</h3>
				<Chart
					options={incomeVsExpensesOptions(rebased, colors)}
					onClick={chartClick(rebased[0]?.label ?? '')}
				/>
			</div>

			<!-- VIZ-1c: Subcategory breakdown -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<div class="flex items-center justify-between mb-2">
					<h3 class="font-semibold text-gray-900">{$i18n.t('viz.chart.subcategoryBreakdown')}</h3>
					<select
						class="border border-gray-300 rounded-md px-2 py-1 text-sm"
						value={selectedCategory}
						onchange={(e) => (selectedCategory = (e.target as HTMLSelectElement).value)}
					>
						{#each categories as cat}
							<option value={cat.key}>{cat.name}</option>
						{/each}
					</select>
				</div>
				<Chart
					options={subcategoryBreakdownOptions(
						rebased,
						colors,
						selectedCategory,
						subIdsForCategory
					)}
					onClick={chartClick(rebased[0]?.label ?? '')}
				/>
			</div>

			<!-- VIZ-1d: Subcategory share donut -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h3 class="font-semibold text-gray-900 mb-2">{$i18n.t('viz.chart.subcategoryShare')}</h3>
				<Chart options={subcategoryShareOptions(shares, colors)} height="320px" />
			</div>

			<!-- VIZ-1e: Rolling average -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h3 class="font-semibold text-gray-900 mb-2">
					{$i18n.t('viz.chart.rollingAverage')} ({vizState.avgWindow}{$i18n.t('viz.monthsSuffix')})
				</h3>
				<Chart
					options={rollingAverageOptions(avgBuckets as AvgBucket[], colors, [
						'income',
						'fixed',
						'variable',
						'savings'
					])}
					onClick={chartClick(rebased[0]?.label ?? '')}
				/>
			</div>

			<!-- VIZ-1f: Per-account average -->
			<div class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h3 class="font-semibold text-gray-900 mb-2">
					{$i18n.t('viz.chart.accountAverage')} ({vizState.avgWindow}{$i18n.t('viz.monthsSuffix')})
				</h3>
				<Chart options={accountAverageOptions(rebased, accountNames, vizState.avgWindow)} />
			</div>
		</div>
	{/if}
</div>
