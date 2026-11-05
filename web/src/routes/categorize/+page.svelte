<script lang="ts">
	import { goto, replaceState } from '$app/navigation';
	import { page } from '$app/stores';
	import {
		type Account,
		type CategoryWithSubcategories,
		type LinesResponse,
		APIError,
		bulkCategorize,
		categorizeLine,
		listAccounts,
		listCategories,
		listLines,
		redoAction,
		resetLine,
		setLineSpread,
		undoAction
	} from '$lib/api';
	import {
		type CatzAction,
		clampFocus,
		filterPickerItems,
		isReservedKey,
		mapKeyToAction,
		nextFocusId,
		normalizeKey,
		selectionRange
	} from '$lib/categorizeKeys';
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
	let error = $state<string | null>(null);
	let loading = $state(false);

	let filters = $state<StatementFilters>(defaultFilters());
	let preset = $state<DatePreset>('all');
	let focusId = $state<number | null>(null);
	let anchorId = $state<number | null>(null);
	let selection = $state<number[]>([]);

	let pickerOpen = $state(false);
	let pickerQuery = $state('');
	let pickerIndex = $state(0);
	let pickerInput = $state<HTMLInputElement | null>(null);
	let helpOpen = $state(false);
	let spreadOpen = $state(false);
	let spreadValue = $state('1');
	let spreadError = $state<string | null>(null);
	let toast = $state<string | null>(null);
	let toastTimer: ReturnType<typeof setTimeout> | undefined;
	let messageInput = $state<HTMLInputElement | null>(null);

	let fetchKey: string | null = null;

	const allSubcategories = $derived(categories.flatMap((c) => c.subcategories));
	const categoryName = (sub: { category_id: number }) =>
		categories.find((c) => c.id === sub.category_id)?.name ?? '';
	// hotkey -> subcategoryId; reserved keys can never be hotkeys (D7)
	const hotkeyMap = $derived(
		new Map(
			allSubcategories
				.filter((s) => s.hotkey && !isReservedKey(s.hotkey))
				.map((s) => [normalizeKey(s.hotkey as string), s.id])
		)
	);
	const lineIds = $derived(data?.lines.map((l) => l.id) ?? []);
	const focused = $derived(data?.lines.find((l) => l.id === focusId) ?? null);
	const pickerItems = $derived(
		allSubcategories.map((s) => ({
			id: s.id,
			label: s.name,
			haystack: `${categoryName(s)} ${s.name}`,
			sub: s
		}))
	);
	const pickerResults = $derived(filterPickerItems(pickerQuery, pickerItems));
	const pickerHighlight = $derived(pickerResults[Math.min(pickerIndex, pickerResults.length - 1)]);
	const overlayOpen = $derived(pickerOpen || helpOpen || spreadOpen);
	const totalPages = $derived(data ? Math.max(1, Math.ceil(data.total / data.pageSize)) : 1);

	// Filter state lives in the URL (D5); refetch only when the filter part changes.
	$effect(() => {
		const sp = $page.url.searchParams;
		const f = filtersFromParams(sp);
		if (sp.toString() === '') {
			// CATZ-1 default: uncategorized + conflicts, newest first
			f.source = 'none,conflict';
			f.sort = 'date';
			f.order = 'desc';
		}
		const key = filtersToQueryString({ ...f, focus: '' });
		filters = f;
		preset = rangeToPreset(f.from, f.to);
		if (key !== fetchKey) {
			fetchKey = key;
			void fetchLines(f);
		}
		const fid = f.focus ? parseInt(f.focus, 10) : null;
		if (fid !== null && Number.isInteger(fid) && fid !== focusId) {
			focusId = fid;
			anchorId = fid;
			selection = [];
		}
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
			focusId = clampFocus(
				data.lines.map((l) => l.id),
				focusId
			);
		} catch (err) {
			error = err instanceof Error ? err.message : String(err);
		} finally {
			loading = false;
		}
	}

	function showToast(msg: string) {
		toast = msg;
		clearTimeout(toastTimer);
		toastTimer = setTimeout(() => (toast = null), 3500);
	}

	// --- URL helpers ---

	function applyFilters(next: StatementFilters) {
		const sp = paramsFromFilters(next);
		goto(`/categorize?${sp.toString()}`);
	}

	function update(patch: Partial<StatementFilters>) {
		applyFilters({ ...filters, ...patch, page: 1, focus: '' });
	}

	function setPreset(p: DatePreset) {
		preset = p;
		const range = presetToRange(p);
		applyFilters({ ...filters, from: range.from, to: range.to, page: 1, focus: '' });
	}

	function setFocus(id: number | null) {
		focusId = id;
		const sp = new URLSearchParams($page.url.searchParams);
		if (id !== null) sp.set('focus', String(id));
		else sp.delete('focus');
		replaceState(`/categorize?${sp.toString()}`, {});
	}

	function setPage(p: number) {
		applyFilters({ ...filters, page: p });
	}

	// --- actions ---

	async function assign(subcategoryId: number) {
		if (focusId === null) return;
		const targets = selection.length > 1 ? selection : [focusId];
		try {
			const updated =
				targets.length > 1
					? (await bulkCategorize(targets, subcategoryId)).lines
					: [await categorizeLine(focusId, subcategoryId)];
			const name =
				allSubcategories.find((s) => s.id === subcategoryId)?.name ?? `#${subcategoryId}`;
			showToast($i18n.t('catz.toasts.assigned', { subcategory: name, count: targets.length }));
			selection = [];
			// CATZ-4: categorized lines stay visible (dimmed) until the next list
			// refresh, so patch the local list instead of refetching.
			if (data) {
				const byId = new Map(updated.map((l) => [l.id, l]));
				data = { ...data, lines: data.lines.map((l) => byId.get(l.id) ?? l) };
			}
			// CATZ-4: move focus to the next line
			setFocus(nextFocusId(lineIds, focusId, 1));
		} catch (err) {
			showToast(
				$i18n.t('catz.toasts.error', { message: err instanceof Error ? err.message : String(err) })
			);
		}
	}

	async function doUndo() {
		try {
			const action = await undoAction();
			showToast($i18n.t('catz.toasts.undone', { description: action.description }));
			await focusAffected(action.changes?.map((c) => c.entityId) ?? []);
		} catch (err) {
			if (err instanceof APIError && err.status === 409)
				showToast($i18n.t('catz.toasts.nothingToUndo'));
			else
				showToast(
					$i18n.t('catz.toasts.error', {
						message: err instanceof Error ? err.message : String(err)
					})
				);
		}
	}

	async function doRedo() {
		try {
			const action = await redoAction();
			showToast($i18n.t('catz.toasts.redone', { description: action.description }));
			await focusAffected(action.changes?.map((c) => c.entityId) ?? []);
		} catch (err) {
			if (err instanceof APIError && err.status === 409)
				showToast($i18n.t('catz.toasts.nothingToRedo'));
			else
				showToast(
					$i18n.t('catz.toasts.error', {
						message: err instanceof Error ? err.message : String(err)
					})
				);
		}
	}

	// CATZ-6: after undo/redo, focus returns to the first affected line
	async function focusAffected(ids: number[]) {
		await fetchLines(filters);
		const first = ids.find((id) => lineIds.includes(id));
		setFocus(first !== undefined ? first : focusId);
	}

	async function doReset() {
		if (focusId === null) return;
		try {
			await resetLine(focusId);
			await fetchLines(filters);
		} catch (err) {
			showToast(
				$i18n.t('catz.toasts.error', { message: err instanceof Error ? err.message : String(err) })
			);
		}
	}

	async function saveSpread() {
		if (focusId === null) return;
		const n = parseInt(spreadValue, 10);
		if (!Number.isInteger(n) || n < 1 || n > 36) {
			spreadError = $i18n.t('catz.spreadDialog.invalid');
			return;
		}
		spreadError = null;
		try {
			await setLineSpread(focusId, n);
			spreadOpen = false;
			await fetchLines(filters);
		} catch (err) {
			spreadError = err instanceof Error ? err.message : String(err);
		}
	}

	function openSpread() {
		spreadValue = String(focused?.spreadMonths ?? 1);
		spreadError = null;
		spreadOpen = true;
	}

	function openPicker() {
		pickerQuery = '';
		pickerIndex = 0;
		pickerOpen = true;
		queueMicrotask(() => pickerInput?.focus());
	}

	function closeOverlays() {
		pickerOpen = false;
		helpOpen = false;
		spreadOpen = false;
	}

	function handleAction(action: CatzAction) {
		switch (action.type) {
			case 'move': {
				const next = nextFocusId(lineIds, focusId, action.delta);
				if (next === null) return;
				if (action.extendSelection) {
					if (anchorId === null) anchorId = focusId;
					selection = selectionRange(lineIds, anchorId, next);
				} else {
					selection = [];
					anchorId = next;
				}
				setFocus(next);
				return;
			}
			case 'assign':
				void assign(action.subcategoryId);
				return;
			case 'openPicker':
				openPicker();
				return;
			case 'spread':
				openSpread();
				return;
			case 'reset':
				void doReset();
				return;
			case 'undo':
				void doUndo();
				return;
			case 'redo':
				void doRedo();
				return;
			case 'focusFilter':
				messageInput?.focus();
				return;
			case 'help':
				helpOpen = true;
				return;
			case 'closeOverlay':
				closeOverlays();
				return;
			case 'none':
				return;
		}
	}

	function onKeydown(e: KeyboardEvent) {
		const target = e.target as HTMLElement | null;
		const inField =
			target instanceof HTMLInputElement ||
			target instanceof HTMLTextAreaElement ||
			target instanceof HTMLSelectElement;
		if (inField && !overlayOpen) return; // typing in a filter field
		const action = mapKeyToAction({
			key: e.key,
			ctrlKey: e.ctrlKey,
			metaKey: e.metaKey,
			shiftKey: e.shiftKey,
			hotkeys: hotkeyMap,
			overlayOpen
		});
		if (action.type === 'none') return;
		e.preventDefault();
		handleAction(action);
	}

	function onPickerKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			pickerIndex = Math.min(pickerResults.length - 1, pickerIndex + 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			pickerIndex = Math.max(0, pickerIndex - 1);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			if (pickerHighlight) {
				pickerOpen = false;
				void assign(pickerHighlight.id);
			}
		}
	}

	function onSpreadKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			void saveSpread();
		}
	}

	function toggleId(list: number[], id: number): number[] {
		return list.includes(id) ? list.filter((x) => x !== id) : [...list, id];
	}

	const shortcutRows: [string, string][] = [
		['↑ / ↓ or k / j', 'catz.shortcuts.move'],
		['Shift+↑ / Shift+↓', 'catz.shortcuts.multiselect'],
		['1–9, a–z (hotkey)', 'catz.shortcuts.assign'],
		['Space', 'catz.shortcuts.picker'],
		['s', 'catz.shortcuts.spread'],
		['Backspace', 'catz.shortcuts.reset'],
		['Ctrl+Z', 'catz.shortcuts.undo'],
		['Ctrl+Shift+Z', 'catz.shortcuts.redo'],
		['/', 'catz.shortcuts.filter'],
		['?', 'catz.shortcuts.help'],
		['Esc', 'catz.shortcuts.close']
	];
</script>

<svelte:window onkeydown={onKeydown} />

<svelte:head>
	<title>{$i18n.t('catz.title')}</title>
</svelte:head>

<div class="space-y-4">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('catz.title')}</h1>
			<p class="text-sm text-gray-500">{$i18n.t('catz.subtitle')}</p>
		</div>
		{#if selection.length > 1}
			<span class="text-sm text-purple-700 font-medium"
				>{$i18n.t('catz.selected', { count: selection.length })}</span
			>
		{/if}
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<!-- Filter bar (CATZ-1: same filters as statements, state in the URL) -->
	<section class="bg-white shadow rounded-xl p-4 border border-gray-100 space-y-3 text-sm">
		<div class="flex flex-wrap gap-3 items-end">
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('catz.filters.preset')}</span>
				<select
					value={preset}
					onchange={(e) => setPreset(e.currentTarget.value as DatePreset)}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="all">{$i18n.t('catz.filters.presetAll')}</option>
					<option value="last12m">{$i18n.t('catz.filters.presetLast12m')}</option>
					<option value="ytd">{$i18n.t('catz.filters.presetYtd')}</option>
					<option value="prevYear">{$i18n.t('catz.filters.presetPrevYear')}</option>
					<option value="custom">{$i18n.t('catz.filters.presetCustom')}</option>
				</select>
			</label>
			{#if preset === 'custom' || filters.from || filters.to}
				<label class="block">
					<span class="text-xs text-gray-500">{$i18n.t('catz.filters.from')}</span>
					<input
						type="date"
						value={filters.from}
						onchange={(e) => update({ from: e.currentTarget.value, page: 1 })}
						class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					/>
				</label>
				<label class="block">
					<span class="text-xs text-gray-500">{$i18n.t('catz.filters.to')}</span>
					<input
						type="date"
						value={filters.to}
						onchange={(e) => update({ to: e.currentTarget.value, page: 1 })}
						class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					/>
				</label>
			{/if}
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('catz.filters.direction')}</span>
				<select
					value={filters.direction}
					onchange={(e) => update({ direction: e.currentTarget.value as '' | 'in' | 'out' })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('catz.filters.both')}</option>
					<option value="in">{$i18n.t('catz.filters.in')}</option>
					<option value="out">{$i18n.t('catz.filters.out')}</option>
				</select>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('catz.filters.message')}</span>
				<input
					bind:this={messageInput}
					type="text"
					value={filters.message}
					onchange={(e) => update({ message: e.currentTarget.value })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			<label class="block">
				<span class="text-xs text-gray-500">{$i18n.t('catz.filters.source')}</span>
				<select
					value={filters.source}
					onchange={(e) => update({ source: e.currentTarget.value })}
					class="block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				>
					<option value="">{$i18n.t('catz.filters.all')}</option>
					{#each ['none', 'conflict', 'rule', 'manual', 'transfer'] as s}
						<option value={s}>{s}</option>
					{/each}
				</select>
			</label>
		</div>
		<details class="text-sm">
			<summary class="cursor-pointer text-gray-600 hover:text-gray-900"
				>{$i18n.t('catz.filters.moreFilters')}</summary
			>
			<div class="flex flex-wrap gap-4 mt-2">
				<fieldset>
					<legend class="text-xs text-gray-500">{$i18n.t('catz.filters.accounts')}</legend>
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
					<legend class="text-xs text-gray-500">{$i18n.t('catz.filters.categories')}</legend>
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
					<legend class="text-xs text-gray-500">{$i18n.t('catz.filters.subcategories')}</legend>
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
					<span class="text-xs text-gray-500">{$i18n.t('catz.filters.spreadGt')}</span>
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

	<div class="grid grid-cols-1 lg:grid-cols-3 gap-4">
		<!-- Line list (CATZ-1/CATZ-2) -->
		<section
			class="lg:col-span-2 bg-white shadow rounded-xl border border-gray-100 overflow-x-auto"
		>
			<table class="w-full text-sm whitespace-nowrap">
				<thead>
					<tr class="text-left text-gray-500 border-b bg-gray-50">
						<th class="py-2 px-3">{$i18n.t('catz.list.date')}</th>
						<th class="py-2 px-3">{$i18n.t('catz.list.account')}</th>
						<th class="py-2 px-3">{$i18n.t('catz.list.message')}</th>
						<th class="py-2 px-3 text-right">{$i18n.t('catz.list.amount')}</th>
						<th class="py-2 px-3">{$i18n.t('catz.list.subcategory')}</th>
						<th class="py-2 px-3">{$i18n.t('catz.list.source')}</th>
						<th class="py-2 px-3">{$i18n.t('catz.list.spread')}</th>
					</tr>
				</thead>
				<tbody>
					{#if data}
						{#each data.lines as line (line.id)}
							{@const isFocused = line.id === focusId}
							{@const isSelected = selection.includes(line.id)}
							{@const dimmed = line.subcategoryId !== null}
							<tr
								class="border-b last:border-0 cursor-pointer {isFocused
									? 'bg-purple-50'
									: 'hover:bg-gray-50'} {dimmed ? 'opacity-50' : ''} {isSelected
									? 'ring-2 ring-inset ring-purple-400'
									: ''}"
								onclick={() => {
									selection = [];
									anchorId = line.id;
									setFocus(line.id);
								}}
								data-line-id={line.id}
							>
								<td class="py-1.5 px-3">{formatDate(line.bookingDate, $locale)}</td>
								<td class="py-1.5 px-3">{line.accountName}</td>
								<td class="py-1.5 px-3 max-w-md truncate" title={line.message}>{line.message}</td>
								<td
									class="py-1.5 px-3 text-right font-variant-numeric-tabular {line.amountMinor < 0
										? 'text-red-700'
										: 'text-green-700'}">{formatMinor(line.amountMinor, $locale)}</td
								>
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
							><td colspan="7" class="py-6 text-center text-gray-500">{$i18n.t('catz.empty')}</td
							></tr
						>
					{/if}
				</tbody>
			</table>
			{#if loading}
				<p class="p-3 text-sm text-gray-500">{$i18n.t('catz.loading')}</p>
			{/if}
			{#if data && data.total > 0}
				<div class="flex items-center justify-between text-sm p-3 border-t">
					<span class="text-gray-500"
						>{data.total}
						{$i18n.t('catz.pager.rows')} · {$i18n.t('catz.pager.page')}
						{data.page} /
						{totalPages}</span
					>
					<div class="flex gap-2">
						<button
							type="button"
							onclick={() => setPage(data.page - 1)}
							disabled={data.page <= 1}
							class="px-3 py-1.5 bg-white border border-gray-200 rounded-lg disabled:opacity-50 hover:bg-gray-50"
							>{$i18n.t('catz.pager.prev')}</button
						>
						<button
							type="button"
							onclick={() => setPage(data.page + 1)}
							disabled={data.page >= totalPages}
							class="px-3 py-1.5 bg-white border border-gray-200 rounded-lg disabled:opacity-50 hover:bg-gray-50"
							>{$i18n.t('catz.pager.next')}</button
						>
					</div>
				</div>
			{/if}
		</section>

		<!-- Detail pane + palette + actions (CATZ-2/3/7) -->
		<div class="space-y-4">
			{#if focused}
				{@const line = focused}
				<section class="bg-white shadow rounded-xl p-4 border border-gray-100 text-sm space-y-1">
					<h2 class="font-semibold text-gray-900 mb-2">{$i18n.t('catz.detail.title')}</h2>
					<dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
						<dt class="text-gray-500">{$i18n.t('catz.detail.account')}</dt>
						<dd>{line.accountName}</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.message')}</dt>
						<dd class="break-words">{line.message}</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.amount')}</dt>
						<dd class={line.amountMinor < 0 ? 'text-red-700' : 'text-green-700'}>
							{formatMinor(line.amountMinor, $locale)}
						</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.balance')}</dt>
						<dd>{line.balanceMinor != null ? formatMinor(line.balanceMinor, $locale) : '–'}</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.bookingDate')}</dt>
						<dd>{formatDate(line.bookingDate, $locale)}</dd>
						{#if line.transactionDate}
							<dt class="text-gray-500">{$i18n.t('catz.detail.transactionDate')}</dt>
							<dd>{formatDate(line.transactionDate, $locale)}</dd>
						{/if}
						{#if line.valueDate}
							<dt class="text-gray-500">{$i18n.t('catz.detail.valueDate')}</dt>
							<dd>{formatDate(line.valueDate, $locale)}</dd>
						{/if}
						{#if line.bankRef}
							<dt class="text-gray-500">{$i18n.t('catz.detail.bankRef')}</dt>
							<dd class="font-mono text-xs">{line.bankRef}</dd>
						{/if}
						{#if line.counterpartyAccount}
							<dt class="text-gray-500">{$i18n.t('catz.detail.counterparty')}</dt>
							<dd class="font-mono text-xs">{line.counterpartyAccount}</dd>
						{/if}
						<dt class="text-gray-500">{$i18n.t('catz.detail.source')}</dt>
						<dd>{line.source}</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.subcategory')}</dt>
						<dd>
							{#if line.subcategoryName}
								<span
									class="inline-block w-2 h-2 rounded-full mr-1"
									style="background-color: {line.subcategoryColor}"
								></span>{line.subcategoryName}
							{:else}
								–
							{/if}
						</dd>
						<dt class="text-gray-500">{$i18n.t('catz.detail.spread')}</dt>
						<dd>{line.spreadMonths}</dd>
					</dl>
				</section>
			{/if}

			<!-- Subcategory palette (CATZ-3) -->
			<section class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h2 class="font-semibold text-gray-900 text-sm">{$i18n.t('catz.palette.title')}</h2>
				<p class="text-xs text-gray-500 mb-2">{$i18n.t('catz.palette.hint')}</p>
				{#each categories as cat}
					{#if cat.subcategories.length}
						<div class="mb-2">
							<div class="text-xs font-medium text-gray-500 mb-1 flex items-center gap-1">
								<span
									class="inline-block w-2 h-2 rounded-full"
									style="background-color: {cat.color}"
								></span>{cat.name}
							</div>
							<div class="flex flex-wrap gap-1">
								{#each cat.subcategories as sub}
									<button
										type="button"
										onclick={() => void assign(sub.id)}
										class="px-2 py-1 rounded-lg border border-gray-200 text-xs flex items-center gap-1 hover:bg-gray-50"
										title={sub.name}
										data-subcategory-id={sub.id}
									>
										<span
											class="inline-block w-2 h-2 rounded-full"
											style="background-color: {sub.color}"
										></span>
										<span>{sub.name}</span>
										{#if sub.hotkey && !isReservedKey(sub.hotkey)}
											<kbd
												class="ml-1 px-1 rounded bg-gray-100 border border-gray-200 font-mono text-[10px]"
												>{sub.hotkey}</kbd
											>
										{/if}
									</button>
								{/each}
							</div>
						</div>
					{/if}
				{/each}
			</section>

			<!-- Actions on the focused line (CATZ-7) -->
			<section class="bg-white shadow rounded-xl p-4 border border-gray-100">
				<h2 class="font-semibold text-gray-900 text-sm mb-2">{$i18n.t('catz.actions.title')}</h2>
				<div class="flex flex-wrap gap-2 text-sm">
					<button
						type="button"
						disabled
						title={$i18n.t('catz.actions.laterMilestone')}
						class="px-2 py-1.5 rounded-lg border border-gray-200 text-gray-400 cursor-not-allowed"
						>{$i18n.t('catz.actions.createRule')}</button
					>
					<button
						type="button"
						disabled
						title={$i18n.t('catz.actions.laterMilestone')}
						class="px-2 py-1.5 rounded-lg border border-gray-200 text-gray-400 cursor-not-allowed"
						>{$i18n.t('catz.actions.resolveConflict')}</button
					>
					<button
						type="button"
						disabled
						title={$i18n.t('catz.actions.laterMilestone')}
						class="px-2 py-1.5 rounded-lg border border-gray-200 text-gray-400 cursor-not-allowed"
						>{$i18n.t('catz.actions.transfer')}</button
					>
					<button
						type="button"
						onclick={openSpread}
						disabled={focusId === null}
						class="px-2 py-1.5 rounded-lg border border-gray-200 hover:bg-gray-50 disabled:opacity-50"
						>{$i18n.t('catz.actions.spread')}</button
					>
					<button
						type="button"
						onclick={() => void doReset()}
						disabled={focusId === null}
						class="px-2 py-1.5 rounded-lg border border-gray-200 hover:bg-gray-50 disabled:opacity-50"
						>{$i18n.t('catz.actions.reset')}</button
					>
				</div>
			</section>
		</div>
	</div>

	<!-- Persistent hint bar (CATZ-8) -->
	<footer
		class="text-xs text-gray-500 bg-white border border-gray-100 rounded-xl px-4 py-2 shadow flex items-center justify-between"
	>
		<span>{$i18n.t('catz.hintBar')}</span>
		<button type="button" class="text-purple-600 hover:underline" onclick={() => (helpOpen = true)}
			>? {$i18n.t('catz.shortcuts.help')}</button
		>
	</footer>
</div>

<!-- Type-ahead picker overlay (CATZ-5) -->
{#if pickerOpen}
	<div
		class="fixed inset-0 bg-black/40 flex items-start justify-center pt-24 z-40"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) pickerOpen = false;
		}}
	>
		<div class="bg-white rounded-xl shadow-xl border border-gray-200 w-full max-w-md p-4">
			<h2 class="font-semibold text-gray-900 mb-2">{$i18n.t('catz.picker.title')}</h2>
			<input
				bind:this={pickerInput}
				value={pickerQuery}
				oninput={(e) => {
					pickerQuery = e.currentTarget.value;
					pickerIndex = 0;
				}}
				onkeydown={onPickerKeydown}
				type="text"
				placeholder={$i18n.t('catz.picker.placeholder')}
				class="w-full rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500 mb-2"
			/>
			<ul class="max-h-80 overflow-y-auto">
				{#each pickerResults as item, i (item.id)}
					<li>
						<button
							type="button"
							class="w-full text-left px-2 py-1.5 rounded flex items-center gap-2 {i === pickerIndex
								? 'bg-purple-50'
								: 'hover:bg-gray-50'}"
							onmouseenter={() => (pickerIndex = i)}
							onclick={() => {
								pickerOpen = false;
								void assign(item.id);
							}}
						>
							<span
								class="inline-block w-2 h-2 rounded-full"
								style="background-color: {item.sub.color}"
							></span>
							<span class="text-sm">{item.sub.name}</span>
							<span class="text-xs text-gray-400">{categoryName(item.sub)}</span>
							{#if item.sub.hotkey}
								<kbd
									class="ml-auto px-1 rounded bg-gray-100 border border-gray-200 font-mono text-[10px]"
									>{item.sub.hotkey}</kbd
								>
							{/if}
						</button>
					</li>
				{:else}
					<li class="px-2 py-3 text-sm text-gray-500">{$i18n.t('catz.picker.empty')}</li>
				{/each}
			</ul>
			<p class="text-xs text-gray-400 mt-2">{$i18n.t('catz.picker.hint')}</p>
		</div>
	</div>
{/if}

<!-- Spread dialog (CATZ-7 / VIZ-7) -->
{#if spreadOpen}
	<div
		class="fixed inset-0 bg-black/40 flex items-center justify-center z-40"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) spreadOpen = false;
		}}
	>
		<div class="bg-white rounded-xl shadow-xl border border-gray-200 w-full max-w-sm p-4">
			<h2 class="font-semibold text-gray-900 mb-2">{$i18n.t('catz.spreadDialog.title')}</h2>
			<label class="block text-sm">
				<span class="text-xs text-gray-500">{$i18n.t('catz.spreadDialog.label')}</span>
				<input
					type="number"
					min="1"
					max="36"
					value={spreadValue}
					oninput={(e) => (spreadValue = e.currentTarget.value)}
					onkeydown={onSpreadKeydown}
					class="block w-full rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
				/>
			</label>
			{#if spreadError}
				<p class="text-xs text-red-600 mt-1">{spreadError}</p>
			{/if}
			<div class="flex justify-end gap-2 mt-3">
				<button
					type="button"
					onclick={() => (spreadOpen = false)}
					class="px-3 py-1.5 rounded-lg border border-gray-200 hover:bg-gray-50"
					>{$i18n.t('catz.spreadDialog.cancel')}</button
				>
				<button
					type="button"
					onclick={() => void saveSpread()}
					class="px-3 py-1.5 rounded-lg bg-purple-600 text-white hover:bg-purple-700"
					>{$i18n.t('catz.spreadDialog.save')}</button
				>
			</div>
		</div>
	</div>
{/if}

<!-- Shortcut help overlay (CATZ-8) -->
{#if helpOpen}
	<div
		class="fixed inset-0 bg-black/40 flex items-center justify-center z-40"
		role="presentation"
		onclick={(e) => {
			if (e.target === e.currentTarget) helpOpen = false;
		}}
	>
		<div class="bg-white rounded-xl shadow-xl border border-gray-200 w-full max-w-md p-4">
			<h2 class="font-semibold text-gray-900 mb-3">{$i18n.t('catz.shortcuts.title')}</h2>
			<table class="w-full text-sm">
				<tbody>
					{#each shortcutRows as [keys, labelKey]}
						<tr class="border-b last:border-0">
							<td class="py-1.5 pr-3 text-gray-600">{$i18n.t(labelKey)}</td>
							<td class="py-1.5 text-right"
								><kbd
									class="px-1.5 py-0.5 rounded bg-gray-100 border border-gray-200 font-mono text-xs"
									>{keys}</kbd
								></td
							>
						</tr>
					{/each}
				</tbody>
			</table>
			<div class="flex justify-end mt-3">
				<button
					type="button"
					onclick={() => (helpOpen = false)}
					class="px-3 py-1.5 rounded-lg bg-purple-600 text-white hover:bg-purple-700"
					>{$i18n.t('catz.shortcuts.close')}</button
				>
			</div>
		</div>
	</div>
{/if}

<!-- Toast -->
{#if toast}
	<div
		class="fixed bottom-4 right-4 bg-gray-900 text-white text-sm px-4 py-2 rounded-lg shadow-lg z-50"
		role="status"
	>
		{toast}
	</div>
{/if}
