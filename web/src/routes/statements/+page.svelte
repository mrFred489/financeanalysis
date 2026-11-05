<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		type Account,
		type CategoryWithSubcategories,
		type LinesResponse,
		listAccounts,
		listCategories,
		listLines
	} from '$lib/api';
	import { formatDate, formatMinor } from '$lib/format';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import {
		type DatePreset,
		type StatementFilters,
		defaultFilters,
		filtersFromParams,
		filtersToQueryString,
		paramsFromFilters,
		presetToRange,
		rangeToPreset
	} from '$lib/statementFilters';
	import { onMount } from 'svelte';

	let accounts = $state<Account[]>([]);
	let categories = $state<CategoryWithSubcategories[]>([]);
	let data = $state<LinesResponse | null>(null);
	let error: string | null = $state(null);
	let loading = $state(false);

	// Filter state is derived from the URL (STMT-5); the effect below applies it on load
	let filters = $state<StatementFilters>(defaultFilters());
	let preset = $state<DatePreset>('all');

	// Re-fetch whenever the URL query changes (back/forward navigation)
	$effect(() => {
		const f = filtersFromParams($page.url.searchParams);
		filters = f;
		preset = rangeToPreset(f.from, f.to);
		void fetchLines(f);
	});

	onMount(async () => {
		try {
			[accounts, categories] = await Promise.all([listAccounts(), listCategories()]);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	});

	async function fetchLines(f: StatementFilters) {
		loading = true;
		error = null;
		try {
			data = await listLines(filtersToQueryString(f));
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	// Push a new filter state into the URL, which triggers the $effect above
	function applyFilters(next: StatementFilters) {
		const sp = paramsFromFilters(next);
		goto(`/statements?${sp.toString()}`);
	}

	function update(patch: Partial<StatementFilters>) {
		applyFilters({ ...filters, ...patch, page: 1 });
	}

	function setPreset(p: DatePreset) {
		preset = p;
		const range = presetToRange(p);
		applyFilters({ ...filters, from: range.from, to: range.to, page: 1 });
	}

	function toggleId(list: number[], id: number): number[] {
		return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
	}

	function sortBy(col: string) {
		if (filters.sort === col) {
			update({ sort: col, order: filters.order === 'asc' ? 'desc' : 'asc', page: 1 });
		} else {
			update({ sort: col, order: col === 'date' ? 'desc' : 'asc', page: 1 });
		}
	}

	const sortArrow = (col: string) =>
		filters.sort === col ? (filters.order === 'asc' ? ' ▲' : ' ▼') : '';

	function setPage(p: number) {
		applyFilters({ ...filters, page: p });
	}

	let totalPages = $derived(data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1);
	let allSubcategories = $derived(categories.flatMap((c) => c.subcategories));
</script>

<svelte:head>
	<title>{$i18n.t('statements.title')}</title>
</svelte:head>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('statements.title')}</h1>
		<a
			href="/categorize?{$page.url.searchParams.toString()}"
			class="px-3 py-2 bg-purple-600 hover:bg-purple-700 text-white text-sm font-medium rounded-lg shadow"
			>{$i18n.t('statements.categorizeThese')}</a
		>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<!-- Filter bar (STMT-2) -->
	<section class="bg-white shadow rounded-xl p-4 border border-gray-100 space-y-3 text-sm">
		<div class="flex flex-wrap gap-3 items-end">
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.preset')}</span>
				<select
					value={preset}
					onchange={(e) => setPreset(e.currentTarget.value as DatePreset)}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="all">{$i18n.t('statements.presetAll')}</option>
					<option value="last12m">{$i18n.t('statements.presetLast12m')}</option>
					<option value="ytd">{$i18n.t('statements.presetYtd')}</option>
					<option value="prevYear">{$i18n.t('statements.presetPrevYear')}</option>
					<option value="custom">{$i18n.t('statements.presetCustom')}</option>
				</select>
			</label>
			{#if preset === 'custom' || filters.from || filters.to}
				<label class="block">
					<span class="text-xs text-gray-500">{$i18n.t('statements.from')}</span>
					<input
						type="date"
						value={filters.from}
						onchange={(e) => update({ from: e.currentTarget.value, page: 1 })}
						class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					/>
				</label>
				<label class="block">
					<span class="text-xs text-gray-500">{$i18n.t('statements.to')}</span>
					<input
						type="date"
						value={filters.to}
						onchange={(e) => update({ to: e.currentTarget.value, page: 1 })}
						class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					/>
				</label>
			{/if}
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.direction')}</span>
				<select
					value={filters.direction}
					onchange={(e) => update({ direction: e.currentTarget.value as '' | 'in' | 'out' })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('statements.both')}</option>
					<option value="in">{$i18n.t('statements.in')}</option>
					<option value="out">{$i18n.t('statements.out')}</option>
				</select>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.message')}</span>
				<input
					type="text"
					value={filters.message}
					onchange={(e) => update({ message: e.currentTarget.value })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.amountMin')}</span>
				<input
					type="number"
					value={filters.amountMin}
					onchange={(e) => update({ amountMin: e.currentTarget.value })}
					class="block w-28 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.amountMax')}</span>
				<input
					type="number"
					value={filters.amountMax}
					onchange={(e) => update({ amountMax: e.currentTarget.value })}
					class="block w-28 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('statements.source')}</span>
				<select
					value={filters.source}
					onchange={(e) => update({ source: e.currentTarget.value })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('statements.all')}</option>
					{#each ['none', 'rule', 'manual', 'transfer', 'conflict'] as s}
						<option value={s}>{s}</option>
					{/each}
				</select>
			</label>
		</div>
		<details class="text-sm">
			<summary class="cursor-pointer text-gray-600 hover:text-gray-900"
				>{$i18n.t('statements.moreFilters')}</summary
			>
			<div class="flex flex-wrap gap-4 mt-2">
				<fieldset>
					<legend class="text-xs text-gray-500">{$i18n.t('statements.accounts')}</legend>
					{#each accounts as acc}
						<label class="mr-3 inline-flex items-center gap-1">
							<input
								type="checkbox"
								checked={filters.accounts.includes(acc.id)}
								onchange={() => update({ accounts: toggleId(filters.accounts, acc.id) })}
							/>
							{acc.name}
						</label>
					{/each}
				</fieldset>
				<fieldset>
					<legend class="text-xs text-gray-500">{$i18n.t('statements.categories')}</legend>
					{#each categories as c}
						<label class="mr-3 inline-flex items-center gap-1">
							<input
								type="checkbox"
								checked={filters.categories.includes(c.id)}
								onchange={() => update({ categories: toggleId(filters.categories, c.id) })}
							/>
							<span class="inline-block w-2 h-2 rounded-full" style="background-color: {c.color}"
							></span>
							{c.name}
						</label>
					{/each}
				</fieldset>
				<fieldset>
					<legend class="text-xs text-gray-500">{$i18n.t('statements.subcategories')}</legend>
					{#each allSubcategories as sub}
						<label class="mr-3 inline-flex items-center gap-1">
							<input
								type="checkbox"
								checked={filters.subcategories.includes(sub.id)}
								onchange={() => update({ subcategories: toggleId(filters.subcategories, sub.id) })}
							/>
							<span class="inline-block w-2 h-2 rounded-full" style="background-color: {sub.color}"
							></span>
							{sub.name}
						</label>
					{/each}
				</fieldset>
				<label class="block">
					<span class="text-xs text-gray-500">{$i18n.t('statements.spreadGt')}</span>
					<input
						type="number"
						min="1"
						value={filters.spreadGt}
						onchange={(e) => update({ spreadGt: e.currentTarget.value })}
						class="block w-20 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					/>
				</label>
			</div>
		</details>
	</section>

	<!-- Totals bar (STMT-4) -->
	{#if data}
		<div
			class="bg-white shadow rounded-xl px-4 py-3 border border-gray-100 flex flex-wrap gap-x-6 gap-y-1 text-sm"
		>
			<span
				><span class="text-gray-500">{$i18n.t('statements.count')}:</span>
				<strong>{data.totals.count}</strong></span
			>
			<span
				><span class="text-gray-500">{$i18n.t('statements.sumIn')}:</span>
				<strong class="text-green-700">{formatMinor(data.totals.sumIn, $locale)}</strong></span
			>
			<span
				><span class="text-gray-500">{$i18n.t('statements.sumOut')}:</span>
				<strong class="text-red-700">{formatMinor(data.totals.sumOut, $locale)}</strong></span
			>
			<span
				><span class="text-gray-500">{$i18n.t('statements.net')}:</span>
				<strong>{formatMinor(data.totals.net, $locale)}</strong></span
			>
		</div>
	{/if}

	<!-- Table (STMT-1/STMT-3) -->
	<section class="bg-white shadow rounded-xl border border-gray-100 overflow-x-auto">
		<table class="w-full text-sm whitespace-nowrap">
			<thead>
				<tr class="text-left text-gray-500 border-b bg-gray-50">
					<th class="py-2 px-3 cursor-pointer" onclick={() => sortBy('date')}
						>{$i18n.t('statements.date')}{sortArrow('date')}</th
					>
					<th class="py-2 px-3 cursor-pointer" onclick={() => sortBy('account')}
						>{$i18n.t('statements.account')}{sortArrow('account')}</th
					>
					<th class="py-2 px-3 cursor-pointer" onclick={() => sortBy('message')}
						>{$i18n.t('statements.message')}{sortArrow('message')}</th
					>
					<th class="py-2 px-3 cursor-pointer text-right" onclick={() => sortBy('amount')}
						>{$i18n.t('statements.amount')}{sortArrow('amount')}</th
					>
					<th class="py-2 px-3 cursor-pointer text-right" onclick={() => sortBy('balance')}
						>{$i18n.t('statements.balance')}{sortArrow('balance')}</th
					>
					<th class="py-2 px-3">{$i18n.t('statements.category')}</th>
					<th class="py-2 px-3">{$i18n.t('statements.subcategory')}</th>
					<th class="py-2 px-3">{$i18n.t('statements.source')}</th>
					<th class="py-2 px-3">{$i18n.t('statements.spread')}</th>
				</tr>
			</thead>
			<tbody>
				{#if data}
					{#each data.lines as line (line.id)}
						<tr class="border-b last:border-0 hover:bg-gray-50">
							<td class="py-1.5 px-3">{formatDate(line.bookingDate, $locale)}</td>
							<td class="py-1.5 px-3">{line.accountName}</td>
							<td class="py-1.5 px-3 max-w-md truncate" title={line.message}>{line.message}</td>
							<td
								class="py-1.5 px-3 text-right font-variant-numeric-tabular {line.amountMinor < 0
									? 'text-red-700'
									: 'text-green-700'}">{formatMinor(line.amountMinor, $locale)}</td
							>
							<td class="py-1.5 px-3 text-right font-variant-numeric-tabular"
								>{formatMinor(line.balanceMinor, $locale)}</td
							>
							<td class="py-1.5 px-3">{line.categoryName ?? '–'}</td>
							<td class="py-1.5 px-3">
								{#if line.subcategoryName}
									<span
										class="inline-block w-2 h-2 rounded-full mr-1"
										style="background-color: {line.subcategoryColor}"
									></span>{line.subcategoryName}
								{:else}
									–
								{/if}
							</td>
							<td class="py-1.5 px-3 text-gray-500">{line.source}</td>
							<td class="py-1.5 px-3 text-gray-500"
								>{line.spreadMonths > 1 ? line.spreadMonths : ''}</td
							>
						</tr>
					{/each}
				{:else if !loading}
					<tr
						><td colspan="9" class="py-6 text-center text-gray-500"
							>{$i18n.t('statements.noLines')}</td
						></tr
					>
				{/if}
			</tbody>
		</table>
		{#if loading}
			<p class="p-3 text-sm text-gray-500">{$i18n.t('statements.loading')}</p>
		{/if}
	</section>

	<!-- Pagination -->
	{#if data && data.total > 0}
		<div class="flex items-center justify-between text-sm">
			<span class="text-gray-500">
				{data.total}
				{$i18n.t('statements.rows')} · {$i18n.t('statements.page')}
				{data.page} / {totalPages}
			</span>
			<div class="flex gap-2">
				<button
					type="button"
					onclick={() => setPage(data.page - 1)}
					disabled={data.page <= 1}
					class="px-3 py-1.5 bg-white border border-gray-200 rounded-lg disabled:opacity-50 hover:bg-gray-50"
					>{$i18n.t('statements.prev')}</button
				>
				<button
					type="button"
					onclick={() => setPage(data.page + 1)}
					disabled={data.page >= totalPages}
					class="px-3 py-1.5 bg-white border border-gray-200 rounded-lg disabled:opacity-50 hover:bg-gray-50"
					>{$i18n.t('statements.next')}</button
				>
			</div>
		</div>
	{/if}
</div>
