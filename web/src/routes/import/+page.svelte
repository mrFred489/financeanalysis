<script lang="ts">
	import { goto } from '$app/navigation';
	import {
		APIError,
		type Account,
		type CommitResult,
		type ImportBatch,
		type PreviewResult,
		commitImport,
		listAccounts,
		listImports,
		previewImport,
		undoImport
	} from '$lib/api';
	import { formatDate, formatDateTime } from '$lib/format';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import { onMount } from 'svelte';

	let accounts = $state<Account[]>([]);
	let batches = $state<ImportBatch[]>([]);
	let accountId = $state<number | null>(null);
	let file: File | null = $state(null);

	let preview: PreviewResult | null = $state(null);
	let result: CommitResult | null = $state(null);
	let error: string | null = $state(null);
	let busy = $state(false);

	async function reload() {
		try {
			[accounts, batches] = await Promise.all([listAccounts(), listImports()]);
			if (accountId === null) {
				const first = accounts.find((a) => a.archived !== 1) ?? accounts[0];
				accountId = first ? first.id : null;
			}
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	onMount(reload);

	async function doPreview() {
		if (!file || !accountId) return;
		busy = true;
		error = null;
		preview = null;
		result = null;
		try {
			preview = await previewImport(file, accountId);
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		} finally {
			busy = false;
		}
	}

	async function doCommit() {
		if (!file || !accountId || !preview) return;
		busy = true;
		error = null;
		try {
			result = await commitImport(file, accountId, preview.previewToken);
			preview = null;
			file = null;
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		} finally {
			busy = false;
		}
	}

	function cancelPreview() {
		preview = null;
	}

	function resetResult() {
		result = null;
	}

	async function undo(id: number) {
		if (!confirm($i18n.t('import.undoConfirm'))) return;
		try {
			await undoImport(id);
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	let latestActive = $derived(batches.find((b) => !b.undone_at));
</script>

<svelte:head>
	<title>{$i18n.t('import.title')}</title>
</svelte:head>

<div class="space-y-8">
	<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('import.title')}</h1>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<!-- Upload + preview (IMP-1..5) -->
	{#if !result}
		<section class="bg-white shadow rounded-xl p-6 border border-gray-100 space-y-4">
			<div class="flex flex-wrap gap-4 items-end">
				<label class="block">
					<span class="text-sm font-medium text-gray-700">{$i18n.t('import.account')}</span>
					<select
						bind:value={accountId}
						class="mt-1 block rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
					>
						{#each accounts as acc}
							<option value={acc.id}
								>{acc.name}{acc.archived === 1 ? ` (${$i18n.t('settings.archived')})` : ''}</option
							>
						{/each}
					</select>
				</label>
				<label class="block">
					<span class="text-sm font-medium text-gray-700">{$i18n.t('import.file')}</span>
					<input
						type="file"
						accept=".csv"
						onchange={(e) => {
							file = e.currentTarget.files?.[0] ?? null;
							preview = null;
							result = null;
						}}
						class="mt-1 block text-sm text-gray-600 file:mr-4 file:py-2 file:px-4 file:rounded-lg file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100"
					/>
				</label>
				<button
					type="button"
					onclick={doPreview}
					disabled={!file || !accountId || busy}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg shadow"
				>
					{busy ? $i18n.t('import.working') : $i18n.t('import.preview')}
				</button>
			</div>

			{#if preview}
				<div class="border-t border-gray-200 pt-4 space-y-4">
					<h2 class="text-lg font-semibold text-gray-900">
						{$i18n.t('import.previewTitle', { file: preview.fileName })}
					</h2>
					<dl class="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
						<div>
							<dt class="text-gray-500">{$i18n.t('import.dateRange')}</dt>
							<dd class="font-medium">
								{preview.dateFrom ? formatDate(preview.dateFrom, $locale) : '–'} – {preview.dateTo
									? formatDate(preview.dateTo, $locale)
									: '–'}
							</dd>
						</div>
						<div>
							<dt class="text-gray-500">{$i18n.t('import.fileAccounts')}</dt>
							<dd class="font-medium font-mono text-xs">
								{preview.fileAccounts.length > 0 ? preview.fileAccounts.join(', ') : '–'}
							</dd>
						</div>
						<div>
							<dt class="text-gray-500">{$i18n.t('import.newLines')}</dt>
							<dd class="font-medium text-green-700">{preview.newCount}</dd>
						</div>
						<div>
							<dt class="text-gray-500">{$i18n.t('import.duplicates')}</dt>
							<dd class="font-medium text-yellow-700">{preview.duplicateCount}</dd>
						</div>
						<div>
							<dt class="text-gray-500">{$i18n.t('import.parseErrors')}</dt>
							<dd class="font-medium text-red-700">{preview.parseErrors.length}</dd>
						</div>
					</dl>

					{#if preview.warnings.length > 0}
						<div class="bg-yellow-50 border-l-4 border-yellow-400 p-3 rounded-r-lg">
							<h3 class="font-medium text-yellow-800 text-sm">{$i18n.t('import.warnings')}</h3>
							<ul class="mt-1 list-disc list-inside text-sm text-yellow-800">
								{#each preview.warnings as w}
									<li>{w.message}</li>
								{/each}
							</ul>
						</div>
					{/if}

					{#if preview.parseErrors.length > 0}
						<div class="bg-red-50 border-l-4 border-red-400 p-3 rounded-r-lg">
							<h3 class="font-medium text-red-800 text-sm">{$i18n.t('import.parseErrorsTitle')}</h3>
							<table class="mt-1 text-sm text-red-800">
								<tbody>
									{#each preview.parseErrors as e}
										<tr>
											<td class="pr-3 font-mono">{$i18n.t('import.row', { row: e.row })}</td>
											<td>{e.reason}</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}

					<div class="flex gap-2">
						<button
							type="button"
							onclick={doCommit}
							disabled={busy || preview.newCount === 0}
							class="px-4 py-2 bg-green-600 hover:bg-green-700 disabled:opacity-50 text-white font-medium rounded-lg shadow"
							>{$i18n.t('import.confirm')}</button
						>
						<button
							type="button"
							onclick={cancelPreview}
							class="px-4 py-2 bg-gray-100 hover:bg-gray-200 text-gray-700 font-medium rounded-lg"
							>{$i18n.t('import.cancel')}</button
						>
					</div>
				</div>
			{/if}
		</section>
	{:else}
		<!-- Result screen (IMP-12) -->
		<section class="bg-white shadow rounded-xl p-6 border border-gray-100 space-y-4">
			<h2 class="text-lg font-semibold text-gray-900">
				{$i18n.t('import.resultTitle', { file: result.fileName })}
			</h2>
			<dl class="grid grid-cols-2 sm:grid-cols-3 gap-4 text-sm">
				<div>
					<dt class="text-gray-500">{$i18n.t('import.linesImported')}</dt>
					<dd class="font-medium text-green-700">{result.linesImported}</dd>
				</div>
				<div>
					<dt class="text-gray-500">{$i18n.t('import.duplicatesSkipped')}</dt>
					<dd class="font-medium text-yellow-700">{result.duplicatesSkipped}</dd>
				</div>
				<div>
					<dt class="text-gray-500">{$i18n.t('import.uncategorized')}</dt>
					<dd class="font-medium">{result.uncategorized}</dd>
				</div>
				<div>
					<dt class="text-gray-500">{$i18n.t('import.ruleCategorized')}</dt>
					<dd class="font-medium text-purple-700" data-rule-categorized>
						{result.ruleCategorized}
					</dd>
				</div>
				<div>
					<dt class="text-gray-500">{$i18n.t('import.ruleConflicts')}</dt>
					<dd class="font-medium text-red-700" data-rule-conflicts>{result.ruleConflicts}</dd>
				</div>
			</dl>
			<div class="flex gap-2">
				<button
					type="button"
					onclick={resetResult}
					class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg shadow"
					>{$i18n.t('import.importAnother')}</button
				>
				<a
					href="/statements"
					class="px-4 py-2 bg-gray-100 hover:bg-gray-200 text-gray-700 font-medium rounded-lg"
					>{$i18n.t('import.viewStatements')}</a
				>
				<button
					type="button"
					onclick={() => goto('/categorize?source=none')}
					class="px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white font-medium rounded-lg shadow"
					>{$i18n.t('import.categorizeNow')}</button
				>
			</div>
		</section>
	{/if}

	<!-- Import history (IMP-14) -->
	<section class="bg-white shadow rounded-xl p-6 border border-gray-100">
		<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('import.history')}</h2>
		{#if batches.length === 0}
			<p class="text-sm text-gray-500">{$i18n.t('import.noImports')}</p>
		{:else}
			<table class="w-full text-sm">
				<thead>
					<tr class="text-left text-gray-500 border-b">
						<th class="py-2 pr-2">{$i18n.t('import.account')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.file')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.dateRange')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.lines')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.duplicates')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.importedAt')}</th>
						<th class="py-2 pr-2">{$i18n.t('import.status')}</th>
						<th class="py-2"></th>
					</tr>
				</thead>
				<tbody>
					{#each batches as batch (batch.id)}
						<tr class="border-b last:border-0">
							<td class="py-2 pr-2">{batch.account_name}</td>
							<td class="py-2 pr-2 font-mono text-xs">{batch.file_name}</td>
							<td class="py-2 pr-2">
								{batch.date_from ? formatDate(batch.date_from, $locale) : '–'} – {batch.date_to
									? formatDate(batch.date_to, $locale)
									: '–'}
							</td>
							<td class="py-2 pr-2">{batch.actual_line_count}</td>
							<td class="py-2 pr-2">{batch.duplicate_count}</td>
							<td class="py-2 pr-2">{formatDateTime(batch.imported_at, $locale)}</td>
							<td class="py-2 pr-2">
								{#if batch.undone_at}
									<span class="text-gray-400">{$i18n.t('import.undone')}</span>
								{:else}
									<span class="text-green-600">{$i18n.t('import.active')}</span>
								{/if}
							</td>
							<td class="py-2 text-right">
								{#if latestActive && batch.id === latestActive.id}
									<button
										type="button"
										onclick={() => undo(batch.id)}
										class="text-red-600 hover:bg-red-50 px-2 py-1 rounded"
										>{$i18n.t('import.undo')}</button
									>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
</div>
