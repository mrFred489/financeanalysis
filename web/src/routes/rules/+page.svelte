<script lang="ts">
	import {
		type Account,
		type ConflictEntry,
		type Rule,
		type Subcategory,
		APIError,
		deleteRule,
		listAccounts,
		listConflicts,
		listRules,
		listSubcategories,
		resolveConflict,
		toggleRule
	} from '$lib/api';
	import { formatDate, formatMinor } from '$lib/format';
	import { page } from '$app/stores';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import {
		accountLabel,
		buildRulesQuery,
		defaultRulesListFilter,
		type RulesListFilter,
		type RuleSortKey
	} from '$lib/rules';
	import { onMount } from 'svelte';

	let tab = $state<'rules' | 'conflicts'>(
		$page.url.searchParams.get('tab') === 'conflicts' ? 'conflicts' : 'rules'
	);
	let rules = $state<Rule[]>([]);
	let conflicts = $state<ConflictEntry[]>([]);
	let accounts = $state<Account[]>([]);
	let subcategories = $state<Subcategory[]>([]);
	let error = $state<string | null>(null);
	let toast = $state<string | null>(null);
	let toastTimer: ReturnType<typeof setTimeout> | undefined;
	let confirmDelete = $state<number | null>(null);

	let filter = $state<RulesListFilter>({ ...defaultRulesListFilter });

	function showToast(msg: string) {
		toast = msg;
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toast = null), 3500);
	}

	async function fetchRules() {
		try {
			rules = await listRules(buildRulesQuery(filter));
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	async function fetchConflicts() {
		try {
			conflicts = await listConflicts();
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
	}

	onMount(async () => {
		try {
			[accounts, subcategories] = await Promise.all([listAccounts(), listSubcategories()]);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		}
		await fetchRules();
		await fetchConflicts();
	});

	// Refetch the (server-side filtered/sorted) list when the filter changes (RULE-8)
	$effect(() => {
		void filter;
		void fetchRules();
	});

	const accountName = (id: number) => accounts.find((a) => a.id === id)?.name ?? `#${id}`;

	async function onToggle(rule: Rule) {
		try {
			await toggleRule(rule.id, !rule.enabled);
			showToast($i18n.t('rules.toasts.toggled', { name: rule.name }));
			await Promise.all([fetchRules(), fetchConflicts()]);
		} catch (err) {
			showToast(err instanceof Error ? err.message : String(err));
		}
	}

	async function onDelete(rule: Rule) {
		try {
			await deleteRule(rule.id);
			confirmDelete = null;
			showToast($i18n.t('rules.toasts.deleted', { name: rule.name }));
			await Promise.all([fetchRules(), fetchConflicts()]);
		} catch (err) {
			showToast(err instanceof Error ? err.message : String(err));
		}
	}

	async function onResolve(entry: ConflictEntry, ruleId: number) {
		try {
			await resolveConflict(entry.line.id, ruleId);
			showToast($i18n.t('conflicts.toasts.resolved', { id: entry.line.id }));
			await Promise.all([fetchRules(), fetchConflicts()]);
		} catch (err) {
			showToast(err instanceof APIError ? err.message : String(err));
		}
	}

	function setSort(key: RuleSortKey) {
		if (filter.sort === key) {
			filter = { ...filter, order: filter.order === 'asc' ? 'desc' : 'asc' };
		} else {
			filter = { ...filter, sort: key, order: 'asc' };
		}
	}

	const sortKeys: [RuleSortKey, string][] = [
		['name', 'rules.list.colName'],
		['pattern', 'rules.list.colPattern'],
		['subcategory', 'rules.list.colSubcategory'],
		['matchCount', 'rules.list.colMatchCount'],
		['createdAt', 'rules.list.colCreated']
	];
</script>

<svelte:head>
	<title>{$i18n.t('rules.title')}</title>
</svelte:head>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('rules.title')}</h1>
			<p class="text-sm text-gray-500">{$i18n.t('rules.subtitle')}</p>
		</div>
		<a
			href="/rules/new"
			class="px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white font-medium rounded-lg shadow"
			data-new-rule>{$i18n.t('rules.newRule')}</a
		>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<!-- Tabs -->
	<div class="flex gap-2 border-b border-gray-200">
		<button
			type="button"
			class="px-3 py-2 text-sm font-medium border-b-2 {tab === 'rules'
				? 'border-purple-600 text-purple-700'
				: 'border-transparent text-gray-500 hover:text-gray-800'}"
			onclick={() => (tab = 'rules')}
			data-tab-rules>{$i18n.t('rules.tabs.rules')} ({rules.length})</button
		>
		<button
			type="button"
			class="px-3 py-2 text-sm font-medium border-b-2 {tab === 'conflicts'
				? 'border-purple-600 text-purple-700'
				: 'border-transparent text-gray-500 hover:text-gray-800'}"
			onclick={() => (tab = 'conflicts')}
			data-tab-conflicts
			>{$i18n.t('rules.tabs.conflicts')}
			{#if conflicts.length > 0}
				<span class="ml-1 px-1.5 py-0.5 rounded-full bg-red-100 text-red-700 text-xs"
					>{conflicts.length}</span
				>
			{/if}</button
		>
	</div>

	{#if tab === 'rules'}
		<!-- Filter bar (RULE-8) -->
		<section
			class="bg-white shadow rounded-xl p-4 border border-gray-100 flex flex-wrap gap-3 items-end text-sm"
		>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('rules.list.search')}</span>
				<input
					type="text"
					value={filter.search}
					oninput={(e) => (filter = { ...filter, search: e.currentTarget.value })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('rules.list.enabled')}</span>
				<select
					value={filter.enabled}
					onchange={(e) =>
						(filter = { ...filter, enabled: e.currentTarget.value as '' | '1' | '0' })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('rules.list.all')}</option>
					<option value="1">{$i18n.t('rules.list.enabledOnly')}</option>
					<option value="0">{$i18n.t('rules.list.disabledOnly')}</option>
				</select>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('rules.list.subcategory')}</span>
				<select
					value={filter.subcategoryId === null ? '' : String(filter.subcategoryId)}
					onchange={(e) =>
						(filter = {
							...filter,
							subcategoryId:
								e.currentTarget.value === '' ? null : parseInt(e.currentTarget.value, 10)
						})}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('rules.list.all')}</option>
					{#each subcategories as sub (sub.id)}
						<option value={sub.id}>{sub.name}</option>
					{/each}
				</select>
			</label>
		</section>

		<!-- Rules table (RULE-8) -->
		<section class="bg-white shadow rounded-xl border border-gray-100 overflow-x-auto">
			<table class="w-full text-sm whitespace-nowrap">
				<thead>
					<tr class="text-left text-gray-500 border-b bg-gray-50">
						{#each sortKeys as [key, labelKey] (key)}
							<th
								class="py-2 px-3 cursor-pointer select-none"
								onclick={() => setSort(key)}
								data-sort={key}
							>
								{$i18n.t(labelKey)}{filter.sort === key
									? filter.order === 'asc'
										? ' ↑'
										: ' ↓'
									: ''}
							</th>
						{/each}
						<th class="py-2 px-3">{$i18n.t('rules.list.colDirection')}</th>
						<th class="py-2 px-3">{$i18n.t('rules.list.colAccounts')}</th>
						<th class="py-2 px-3">{$i18n.t('rules.list.colEnabled')}</th>
						<th class="py-2 px-3"></th>
					</tr>
				</thead>
				<tbody>
					{#each rules as rule (rule.id)}
						<tr class="border-b last:border-0 hover:bg-gray-50" data-rule-id={rule.id}>
							<td class="py-1.5 px-3 font-medium">{rule.name}</td>
							<td class="py-1.5 px-3 font-mono text-xs max-w-xs truncate" title={rule.pattern}
								>{rule.pattern}</td
							>
							<td class="py-1.5 px-3">
								{#if rule.subcategoryName}
									<span
										class="inline-block w-2 h-2 rounded-full mr-1"
										style="background-color: {rule.subcategoryColor ?? '#999'}"
									></span>{rule.subcategoryName}
								{:else}
									–
								{/if}
							</td>
							<td class="py-1.5 px-3 text-gray-500" data-match-count>{rule.matchCount}</td>
							<td class="py-1.5 px-3 text-gray-500 text-xs"
								>{formatDate(rule.createdAt.slice(0, 10), $locale)}</td
							>
							<td class="py-1.5 px-3 text-gray-500">{rule.direction}</td>
							<td class="py-1.5 px-3 text-gray-500">{accountLabel(rule, accountName)}</td>
							<td class="py-1.5 px-3">
								<button
									type="button"
									class="px-2 py-1 rounded-lg border text-xs {rule.enabled
										? 'border-green-300 text-green-700 hover:bg-green-50'
										: 'border-gray-300 text-gray-500 hover:bg-gray-50'}"
									onclick={() => void onToggle(rule)}
									data-toggle-rule
									>{rule.enabled
										? $i18n.t('rules.list.enabled')
										: $i18n.t('rules.list.disabled')}</button
								>
							</td>
							<td class="py-1.5 px-3 text-right">
								{#if confirmDelete === rule.id}
									<button
										type="button"
										class="px-2 py-1 rounded-lg bg-red-600 text-white text-xs mr-1"
										onclick={() => void onDelete(rule)}
										data-confirm-delete>{$i18n.t('rules.list.confirmDelete')}</button
									>
									<button
										type="button"
										class="px-2 py-1 rounded-lg border border-gray-200 text-xs"
										onclick={() => (confirmDelete = null)}>{$i18n.t('rules.list.cancel')}</button
									>
								{:else}
									<a
										href="/rules/{rule.id}/edit"
										class="px-2 py-1 rounded-lg border border-gray-200 text-xs hover:bg-gray-50 mr-1"
										data-edit-rule>{$i18n.t('rules.list.edit')}</a
									>
									<button
										type="button"
										class="px-2 py-1 rounded-lg border border-red-200 text-red-700 text-xs hover:bg-red-50"
										onclick={() => (confirmDelete = rule.id)}
										data-delete-rule>{$i18n.t('rules.list.delete')}</button
									>
								{/if}
							</td>
						</tr>
					{:else}
						<tr
							><td colspan="9" class="py-6 text-center text-gray-500"
								>{$i18n.t('rules.list.empty')}</td
							></tr
						>
					{/each}
				</tbody>
			</table>
		</section>
	{:else}
		<!-- Conflicts (RULE-7) -->
		<section class="bg-white shadow rounded-xl border border-gray-100 overflow-x-auto">
			{#if conflicts.length === 0}
				<p class="p-6 text-sm text-gray-500">{$i18n.t('conflicts.empty')}</p>
			{:else}
				<table class="w-full text-sm">
					<thead>
						<tr class="text-left text-gray-500 border-b bg-gray-50">
							<th class="py-2 px-3">{$i18n.t('conflicts.colDate')}</th>
							<th class="py-2 px-3">{$i18n.t('conflicts.colAccount')}</th>
							<th class="py-2 px-3">{$i18n.t('conflicts.colMessage')}</th>
							<th class="py-2 px-3 text-right">{$i18n.t('conflicts.colAmount')}</th>
							<th class="py-2 px-3">{$i18n.t('conflicts.colRules')}</th>
						</tr>
					</thead>
					<tbody>
						{#each conflicts as entry (entry.line.id)}
							<tr class="border-b last:border-0" data-conflict-line={entry.line.id}>
								<td class="py-1.5 px-3">{formatDate(entry.line.bookingDate, $locale)}</td>
								<td class="py-1.5 px-3">{entry.line.accountName}</td>
								<td class="py-1.5 px-3 max-w-md truncate" title={entry.line.message}
									>{entry.line.message}</td
								>
								<td class="py-1.5 px-3 text-right font-variant-numeric-tabular"
									>{formatMinor(entry.line.amountMinor, $locale)}</td
								>
								<td class="py-1.5 px-3">
									<div class="flex flex-wrap gap-1">
										{#each entry.rules as rule (rule.id)}
											<span class="inline-flex items-center gap-1">
												<button
													type="button"
													class="px-2 py-1 rounded-lg border text-xs hover:bg-purple-50 {entry.resolutionRuleId ===
													rule.id
														? 'border-purple-500 bg-purple-50 text-purple-800'
														: 'border-gray-300'}"
													title={rule.pattern}
													onclick={() => void onResolve(entry, rule.id)}
													data-resolve-rule={rule.id}>{rule.name}</button
												>
												<a
													href="/rules/{rule.id}/edit"
													class="text-xs text-purple-600 hover:underline"
													data-edit-conflict-rule={rule.id}>{$i18n.t('conflicts.editRule')}</a
												>
											</span>
										{/each}
										<a
											href="/categorize?focus={entry.line.id}&source=conflict"
											class="text-xs text-gray-500 hover:underline"
											data-categorize-manually>{$i18n.t('conflicts.categorizeManually')}</a
										>
									</div>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</section>
	{/if}
</div>

{#if toast}
	<div
		class="fixed bottom-4 right-4 bg-gray-900 text-white text-sm px-4 py-2 rounded-lg shadow-lg z-50"
		role="status"
	>
		{toast}
	</div>
{/if}
