<script lang="ts">
	import { goto } from '$app/navigation';
	import { APIError, createDatabase } from '$lib/api';
	import { CATEGORY_LABELS, DEFAULT_SUBCATEGORIES, type DefaultSubcategory } from '$lib/defaults';
	import { i18n } from '$lib/i18n';

	let step = $state(1);

	// Step 1: accounts
	let accounts = $state<{ name: string; account_number: string }[]>([
		{ name: '', account_number: '' }
	]);

	// Step 2: seeded subcategories, editable
	let subcategories = $state<DefaultSubcategory[]>(DEFAULT_SUBCATEGORIES.map((s) => ({ ...s })));

	let error: string | null = $state(null);
	let busy = $state(false);

	function addAccount() {
		accounts = [...accounts, { name: '', account_number: '' }];
	}
	function removeAccount(i: number) {
		accounts = accounts.filter((_, idx) => idx !== i);
	}
	function addSubcategory(categoryKey: string) {
		const maxOrder = Math.max(
			-1,
			...subcategories.filter((s) => s.category_key === categoryKey).map((s) => s.sort_order)
		);
		subcategories = [
			...subcategories,
			{ category_key: categoryKey, name: '', sort_order: maxOrder + 1 }
		];
	}
	function removeSubcategory(i: number) {
		subcategories = subcategories.filter((_, idx) => idx !== i);
	}

	function next() {
		error = null;
		if (step === 1 && accounts.some((a) => a.name.trim() === '')) {
			error = $i18n.t('welcome.accountNameRequired');
			return;
		}
		if (step === 2 && subcategories.some((s) => s.name.trim() === '')) {
			error = $i18n.t('welcome.subcategoryNameRequired');
			return;
		}
		step++;
	}
	function back() {
		error = null;
		step--;
	}

	async function finish() {
		busy = true;
		error = null;
		try {
			await createDatabase({
				accounts: accounts.map((a) => ({
					name: a.name.trim(),
					account_number: a.account_number.trim() || null
				})),
				subcategories: subcategories.map((s) => ({
					category_key: s.category_key,
					name: s.name.trim(),
					sort_order: s.sort_order
				}))
			});
			await goto('/import');
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
			busy = false;
		}
	}

	const categories = Object.entries(CATEGORY_LABELS);
</script>

<svelte:head>
	<title>{$i18n.t('welcome.newTitle')}</title>
</svelte:head>

<div class="max-w-2xl mx-auto space-y-6">
	<div>
		<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('welcome.newTitle')}</h1>
		<p class="mt-1 text-gray-600">
			{$i18n.t('welcome.step', { step, total: 3 })}: {step === 1
				? $i18n.t('welcome.stepAccounts')
				: step === 2
					? $i18n.t('welcome.stepSubcategories')
					: $i18n.t('welcome.stepFinish')}
		</p>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<div class="bg-white shadow rounded-xl p-6 border border-gray-100">
		{#if step === 1}
			<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('welcome.accounts')}</h2>
			<div class="space-y-3">
				{#each accounts as acc, i}
					<div class="flex gap-2 items-center">
						<input
							type="text"
							placeholder={$i18n.t('welcome.accountName')}
							bind:value={acc.name}
							class="flex-1 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
						/>
						<input
							type="text"
							placeholder={$i18n.t('welcome.accountNumber')}
							bind:value={acc.account_number}
							class="w-40 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
						/>
						<button
							type="button"
							onclick={() => removeAccount(i)}
							class="px-2 py-1 text-red-600 hover:bg-red-50 rounded"
							disabled={accounts.length === 1}>{$i18n.t('welcome.remove')}</button
						>
					</div>
				{/each}
			</div>
			<button
				type="button"
				onclick={addAccount}
				class="mt-3 px-3 py-1.5 text-sm bg-gray-100 hover:bg-gray-200 rounded-lg text-gray-700"
				>+ {$i18n.t('welcome.addAccount')}</button
			>
		{:else if step === 2}
			<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('welcome.subcategories')}</h2>
			{#each categories as [key, label]}
				<div class="mb-4">
					<h3 class="font-medium text-gray-700 mb-2">{label}</h3>
					<div class="space-y-2">
						{#each subcategories
							.map((s, i) => ({ s, i }))
							.filter(({ s }) => s.category_key === key) as { s, i }}
							<div class="flex gap-2 items-center">
								<input
									type="text"
									bind:value={s.name}
									class="flex-1 rounded-lg border-gray-300 shadow-sm focus:border-blue-500 focus:ring-blue-500"
								/>
								<button
									type="button"
									onclick={() => removeSubcategory(i)}
									class="px-2 py-1 text-red-600 hover:bg-red-50 rounded"
									>{$i18n.t('welcome.remove')}</button
								>
							</div>
						{/each}
					</div>
					<button
						type="button"
						onclick={() => addSubcategory(key)}
						class="mt-1 px-2 py-1 text-xs bg-gray-100 hover:bg-gray-200 rounded text-gray-700"
						>+ {$i18n.t('welcome.addSubcategory')}</button
					>
				</div>
			{/each}
		{:else}
			<h2 class="text-lg font-semibold text-gray-900 mb-4">{$i18n.t('welcome.finishTitle')}</h2>
			<p class="text-sm text-gray-600">
				{$i18n.t('welcome.finishSummary', {
					accounts: accounts.length,
					subcategories: subcategories.length
				})}
			</p>
		{/if}
	</div>

	<div class="flex justify-between">
		{#if step > 1}
			<button
				type="button"
				onclick={back}
				class="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg text-gray-700 font-medium"
				>{$i18n.t('welcome.back')}</button
			>
		{:else}
			<span></span>
		{/if}
		{#if step < 3}
			<button
				type="button"
				onclick={next}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white font-medium rounded-lg shadow"
				>{$i18n.t('welcome.next')}</button
			>
		{:else}
			<button
				type="button"
				onclick={finish}
				disabled={busy}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg shadow"
				>{busy ? $i18n.t('welcome.creating') : $i18n.t('welcome.finish')}</button
			>
		{/if}
	</div>
</div>
