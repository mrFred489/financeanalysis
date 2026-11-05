<script lang="ts">
	import {
		APIError,
		type Account,
		type BackupInfo,
		type CategoryWithSubcategories,
		type Subcategory,
		createAccount,
		createSubcategory,
		deleteAccount,
		deleteSubcategory,
		downloadDatabase,
		listAccounts,
		listBackups,
		listCategories,
		replaceDatabase,
		restoreBackup,
		updateAccount,
		updateSubcategory,
		getSettings,
		updateSettings
	} from '$lib/api';
	import { i18n } from '$lib/i18n';
	import { locale } from '$lib/locale';
	import { onMount } from 'svelte';

	let accounts = $state<Account[]>([]);
	let categories = $state<CategoryWithSubcategories[]>([]);
	let backups = $state<BackupInfo[]>([]);
	let error: string | null = $state(null);
	let info: string | null = $state(null);

	// account form
	let newAccountName = $state('');
	let newAccountNumber = $state('');
	// subcategory form
	let newSubName = $state('');
	let newSubCategoryId = $state(3);
	// locale
	let selectedLocale = $state('da-DK');

	async function reload() {
		try {
			[accounts, categories, backups] = await Promise.all([
				listAccounts(),
				listCategories(),
				listBackups()
			]);
			const settings = await getSettings();
			if (settings.locale) {
				selectedLocale = settings.locale;
				locale.set(settings.locale);
			}
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	onMount(reload);

	function flash(msg: string) {
		info = msg;
		setTimeout(() => (info = null), 4000);
	}

	async function addAccount() {
		if (!newAccountName.trim()) return;
		try {
			await createAccount(newAccountName.trim(), newAccountNumber.trim() || undefined);
			newAccountName = '';
			newAccountNumber = '';
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function saveAccount(
		acc: Account,
		patch: {
			name?: string;
			account_number?: string | null;
			sort_order?: number;
			archived?: boolean;
		}
	) {
		try {
			await updateAccount(acc.id, {
				name: patch.name ?? acc.name,
				account_number:
					patch.account_number !== undefined ? patch.account_number : acc.account_number,
				sort_order: patch.sort_order ?? acc.sort_order,
				archived: patch.archived ?? acc.archived === 1
			});
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function removeAccount(acc: Account) {
		try {
			await deleteAccount(acc.id);
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function addSubcategory() {
		if (!newSubName.trim()) return;
		try {
			await createSubcategory({ category_id: newSubCategoryId, name: newSubName.trim() });
			newSubName = '';
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function saveSubcategory(sub: Subcategory, patch: Partial<Subcategory>) {
		try {
			await updateSubcategory(sub.id, {
				name: patch.name ?? sub.name,
				color: patch.color ?? sub.color,
				hotkey: patch.hotkey !== undefined ? patch.hotkey : sub.hotkey,
				sort_order: patch.sort_order ?? sub.sort_order,
				category_id: patch.category_id ?? sub.category_id
			});
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function removeSubcategory(sub: Subcategory) {
		const others = categories.flatMap((c) => c.subcategories).filter((s) => s.id !== sub.id);
		if (others.length === 0) return;
		// Merge into the first other subcategory (CAT-4 requires a merge target)
		const target = others[0];
		if (!confirm($i18n.t('settings.mergeConfirm', { name: sub.name, target: target.name }))) return;
		try {
			await deleteSubcategory(sub.id, target.id);
			await reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function changeLocale() {
		try {
			await updateSettings({ locale: selectedLocale });
			locale.set(selectedLocale);
			flash($i18n.t('settings.saved'));
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function download() {
		try {
			const blob = await downloadDatabase();
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = `finance-${new Date().toISOString().slice(0, 10)}.sqlite`;
			a.click();
			URL.revokeObjectURL(url);
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	let replaceFile: File | null = $state(null);
	async function doReplace() {
		if (!replaceFile) return;
		if (!confirm($i18n.t('settings.replaceConfirm'))) return;
		try {
			await replaceDatabase(replaceFile);
			flash($i18n.t('settings.replaced'));
			location.reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}

	async function restore(name: string) {
		if (!confirm($i18n.t('settings.restoreConfirm', { name }))) return;
		try {
			await restoreBackup(name);
			location.reload();
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
		}
	}
</script>

<svelte:head>
	<title>{$i18n.t('settings.title')}</title>
</svelte:head>

<div class="space-y-8">
	<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('settings.title')}</h1>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}
	{#if info}
		<div
			class="bg-green-50 border-l-4 border-green-500 text-green-700 px-4 py-3 rounded-r-lg shadow-sm"
		>
			<p class="text-sm">{info}</p>
		</div>
	{/if}

	<!-- Accounts (SETUP-6) -->
	<section class="bg-white shadow rounded-xl p-6 border border-gray-100">
		<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('settings.accounts')}</h2>
		<table class="w-full text-sm">
			<thead>
				<tr class="text-left text-gray-500 border-b">
					<th class="py-2 pr-2">{$i18n.t('settings.name')}</th>
					<th class="py-2 pr-2">{$i18n.t('settings.accountNumber')}</th>
					<th class="py-2 pr-2">{$i18n.t('settings.sortOrder')}</th>
					<th class="py-2 pr-2">{$i18n.t('settings.archived')}</th>
					<th class="py-2"></th>
				</tr>
			</thead>
			<tbody>
				{#each accounts as acc (acc.id)}
					<tr class="border-b last:border-0">
						<td class="py-2 pr-2">
							<input
								type="text"
								value={acc.name}
								onchange={(e) => saveAccount(acc, { name: e.currentTarget.value })}
								class="w-full rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
							/>
						</td>
						<td class="py-2 pr-2">
							<input
								type="text"
								value={acc.account_number ?? ''}
								onchange={(e) =>
									saveAccount(acc, { account_number: e.currentTarget.value || null })}
								class="w-full rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
							/>
						</td>
						<td class="py-2 pr-2">
							<input
								type="number"
								value={acc.sort_order}
								onchange={(e) => saveAccount(acc, { sort_order: Number(e.currentTarget.value) })}
								class="w-20 rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
							/>
						</td>
						<td class="py-2 pr-2">
							<input
								type="checkbox"
								checked={acc.archived === 1}
								onchange={(e) => saveAccount(acc, { archived: e.currentTarget.checked })}
							/>
						</td>
						<td class="py-2 text-right">
							<button
								type="button"
								onclick={() => removeAccount(acc)}
								class="text-red-600 hover:bg-red-50 px-2 py-1 rounded"
								>{$i18n.t('settings.delete')}</button
							>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
		<div class="mt-4 flex gap-2">
			<input
				type="text"
				placeholder={$i18n.t('settings.newAccountName')}
				bind:value={newAccountName}
				class="flex-1 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			/>
			<input
				type="text"
				placeholder={$i18n.t('settings.accountNumberOptional')}
				bind:value={newAccountNumber}
				class="w-44 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			/>
			<button
				type="button"
				onclick={addAccount}
				class="px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium"
				>{$i18n.t('settings.addAccount')}</button
			>
		</div>
	</section>

	<!-- Subcategories (CAT-3/CAT-4/CAT-5) -->
	<section class="bg-white shadow rounded-xl p-6 border border-gray-100">
		<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('settings.subcategories')}</h2>
		{#each categories as cat (cat.id)}
			<h3 class="font-medium text-gray-700 mt-4 mb-2 flex items-center gap-2">
				<span class="inline-block w-3 h-3 rounded-full" style="background-color: {cat.color}"
				></span>
				{cat.name}
			</h3>
			<table class="w-full text-sm">
				<thead>
					<tr class="text-left text-gray-500 border-b">
						<th class="py-1 pr-2">{$i18n.t('settings.name')}</th>
						<th class="py-1 pr-2">{$i18n.t('settings.color')}</th>
						<th class="py-1 pr-2">{$i18n.t('settings.hotkey')}</th>
						<th class="py-1 pr-2">{$i18n.t('settings.category')}</th>
						<th class="py-1"></th>
					</tr>
				</thead>
				<tbody>
					{#each cat.subcategories as sub (sub.id)}
						<tr class="border-b last:border-0">
							<td class="py-1 pr-2">
								<input
									type="text"
									value={sub.name}
									onchange={(e) => saveSubcategory(sub, { name: e.currentTarget.value })}
									class="w-full rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
								/>
							</td>
							<td class="py-1 pr-2">
								<input
									type="color"
									value={sub.color}
									onchange={(e) => saveSubcategory(sub, { color: e.currentTarget.value })}
									class="w-10 h-8 rounded border-gray-300"
								/>
							</td>
							<td class="py-1 pr-2">
								<input
									type="text"
									value={sub.hotkey ?? ''}
									maxlength={1}
									onchange={(e) => saveSubcategory(sub, { hotkey: e.currentTarget.value || null })}
									class="w-12 rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
								/>
							</td>
							<td class="py-1 pr-2">
								<select
									value={sub.category_id}
									onchange={(e) =>
										saveSubcategory(sub, { category_id: Number(e.currentTarget.value) })}
									class="rounded border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
								>
									{#each categories as c}
										<option value={c.id}>{c.name}</option>
									{/each}
								</select>
							</td>
							<td class="py-1 text-right">
								<button
									type="button"
									onclick={() => removeSubcategory(sub)}
									class="text-red-600 hover:bg-red-50 px-2 py-1 rounded"
									>{$i18n.t('settings.delete')}</button
								>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/each}
		<div class="mt-4 flex gap-2">
			<input
				type="text"
				placeholder={$i18n.t('settings.newSubcategoryName')}
				bind:value={newSubName}
				class="flex-1 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			/>
			<select
				bind:value={newSubCategoryId}
				class="rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			>
				{#each categories as c}
					<option value={c.id}>{c.name}</option>
				{/each}
			</select>
			<button
				type="button"
				onclick={addSubcategory}
				class="px-3 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-sm font-medium"
				>{$i18n.t('settings.addSubcategory')}</button
			>
		</div>
	</section>

	<!-- Display -->
	<section class="bg-white shadow rounded-xl p-6 border border-gray-100">
		<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('settings.display')}</h2>
		<label class="flex items-center gap-3 text-sm text-gray-700">
			{$i18n.t('settings.locale')}
			<select
				bind:value={selectedLocale}
				onchange={changeLocale}
				class="rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
			>
				<option value="da-DK">da-DK</option>
				<option value="en-US">en-US</option>
			</select>
		</label>
	</section>

	<!-- Database (SETUP-5) -->
	<section class="bg-white shadow rounded-xl p-6 border border-gray-100">
		<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('settings.database')}</h2>
		<div class="flex flex-wrap gap-2">
			<button
				type="button"
				onclick={download}
				class="px-3 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg text-sm font-medium text-gray-700"
				>{$i18n.t('settings.download')}</button
			>
			<label
				class="px-3 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg text-sm font-medium text-gray-700 cursor-pointer"
			>
				{$i18n.t('settings.replace')}
				<input
					type="file"
					accept=".sqlite,.db"
					class="hidden"
					onchange={(e) => (replaceFile = e.currentTarget.files?.[0] ?? null)}
				/>
			</label>
			{#if replaceFile}
				<button
					type="button"
					onclick={doReplace}
					class="px-3 py-2 bg-red-600 hover:bg-red-700 text-white rounded-lg text-sm font-medium"
					>{$i18n.t('settings.replaceConfirmButton', { file: replaceFile.name })}</button
				>
			{/if}
		</div>

		<h3 class="font-medium text-gray-700 mt-6 mb-2">{$i18n.t('settings.backups')}</h3>
		{#if backups.length === 0}
			<p class="text-sm text-gray-500">{$i18n.t('settings.noBackups')}</p>
		{:else}
			<table class="w-full text-sm">
				<thead>
					<tr class="text-left text-gray-500 border-b">
						<th class="py-1 pr-2">{$i18n.t('settings.backupName')}</th>
						<th class="py-1 pr-2">{$i18n.t('settings.backupDate')}</th>
						<th class="py-1 pr-2">{$i18n.t('settings.backupSize')}</th>
						<th class="py-1"></th>
					</tr>
				</thead>
				<tbody>
					{#each backups as b (b.name)}
						<tr class="border-b last:border-0">
							<td class="py-1 pr-2 font-mono text-xs">{b.name}</td>
							<td class="py-1 pr-2">{b.modTime}</td>
							<td class="py-1 pr-2">{(b.size / 1024).toFixed(0)} KB</td>
							<td class="py-1 text-right">
								<button
									type="button"
									onclick={() => restore(b.name)}
									class="text-blue-600 hover:bg-blue-50 px-2 py-1 rounded"
									>{$i18n.t('settings.restore')}</button
								>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</section>
</div>
