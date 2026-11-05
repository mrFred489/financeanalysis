<script lang="ts">
	import { i18n } from '$lib/i18n';
	import { getStatus, type StatusResponse } from '$lib/api';
	import { onMount } from 'svelte';

	let status: StatusResponse | null = null;
	let error: string | null = null;

	onMount(async () => {
		try {
			status = await getStatus();
		} catch (err) {
			error = err instanceof Error ? err.message : 'Unknown error';
		}
	});
</script>

<svelte:head>
	<title>{$i18n.t('app.name')}</title>
</svelte:head>

<div class="space-y-6">
	<div>
		<h2 class="text-2xl font-bold text-gray-900">{$i18n.t('home.title')}</h2>
		<p class="mt-1 text-gray-600">{$i18n.t('home.subtitle')}</p>
	</div>

	{#if error}
		<div class="bg-red-50 border-l-4 border-red-500 text-red-700 px-4 py-3 rounded-r-lg shadow-sm">
			<p class="font-medium">{$i18n.t('home.error')}</p>
			<p class="text-sm mt-1">{error}</p>
		</div>
	{/if}

	{#if status}
		<div class="bg-white shadow rounded-xl p-6 border border-gray-100">
			<p class="text-sm text-gray-600">
				<span class="font-medium text-gray-900">{$i18n.t('home.status')}: </span>
				{#if status.database === 'ready'}
					<span class="text-green-600 font-medium">{$i18n.t('home.ready')}</span>
					{#if status.schemaVersion}
						<span class="text-gray-500 ml-2"
							>({$i18n.t('home.schemaVersion', { version: status.schemaVersion })})</span
						>
					{/if}
				{:else}
					<span class="text-yellow-600 font-medium">{$i18n.t('home.missing')}</span>
				{/if}
			</p>
		</div>

		{#if status.database === 'ready'}
			<div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
				<a
					href="/statements"
					class="block bg-white border border-gray-200 rounded-xl p-5 shadow-sm hover:shadow-md hover:border-blue-300 transition-all"
				>
					<h3 class="font-semibold text-gray-900">{$i18n.t('nav.statements')}</h3>
					<p class="text-sm text-gray-500 mt-1">{$i18n.t('home.statementsHint')}</p>
				</a>
				<a
					href="/import"
					class="block bg-white border border-gray-200 rounded-xl p-5 shadow-sm hover:shadow-md hover:border-cyan-300 transition-all"
				>
					<h3 class="font-semibold text-gray-900">{$i18n.t('nav.import')}</h3>
					<p class="text-sm text-gray-500 mt-1">{$i18n.t('home.importHint')}</p>
				</a>
				<a
					href="/settings"
					class="block bg-white border border-gray-200 rounded-xl p-5 shadow-sm hover:shadow-md hover:border-gray-300 transition-all"
				>
					<h3 class="font-semibold text-gray-900">{$i18n.t('nav.settings')}</h3>
					<p class="text-sm text-gray-500 mt-1">{$i18n.t('home.settingsHint')}</p>
				</a>
			</div>
		{/if}
	{/if}
</div>
