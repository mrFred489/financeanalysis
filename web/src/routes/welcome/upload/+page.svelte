<script lang="ts">
	import { goto } from '$app/navigation';
	import { APIError, uploadDatabase } from '$lib/api';
	import { i18n } from '$lib/i18n';

	let file: File | null = $state(null);
	let error: string | null = $state(null);
	let busy = $state(false);

	async function upload() {
		if (!file) return;
		busy = true;
		error = null;
		try {
			await uploadDatabase(file);
			await goto('/');
		} catch (err) {
			error = err instanceof APIError ? err.message : String(err);
			busy = false;
		}
	}
</script>

<svelte:head>
	<title>{$i18n.t('welcome.uploadTitle')}</title>
</svelte:head>

<div class="max-w-lg mx-auto space-y-6">
	<div>
		<h1 class="text-2xl font-bold text-gray-900">{$i18n.t('welcome.uploadTitle')}</h1>
		<p class="mt-1 text-gray-600">{$i18n.t('welcome.uploadHint')}</p>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="text-sm">{error}</p>
		</div>
	{/if}

	<div class="bg-white shadow rounded-xl p-6 border border-gray-100 space-y-4">
		<input
			type="file"
			accept=".sqlite,.db,application/x-sqlite3,application/vnd.sqlite3"
			onchange={(e) => (file = e.currentTarget.files?.[0] ?? null)}
			class="block w-full text-sm text-gray-600 file:mr-4 file:py-2 file:px-4 file:rounded-lg file:border-0 file:text-sm file:font-medium file:bg-blue-50 file:text-blue-700 hover:file:bg-blue-100"
		/>
		<div class="flex justify-between">
			<a
				href="/welcome"
				class="px-4 py-2 bg-gray-100 hover:bg-gray-200 rounded-lg text-gray-700 font-medium"
				>{$i18n.t('welcome.back')}</a
			>
			<button
				type="button"
				onclick={upload}
				disabled={!file || busy}
				class="px-4 py-2 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white font-medium rounded-lg shadow"
				>{busy ? $i18n.t('welcome.uploading') : $i18n.t('welcome.upload')}</button
			>
		</div>
	</div>
</div>
